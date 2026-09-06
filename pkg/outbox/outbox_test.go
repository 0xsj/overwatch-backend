package outbox_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/clock"
	pkgerrors "github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/logger"
	"github.com/0xsj/overwatch-backend/pkg/outbox"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type fixture struct {
	store *outbox.Memory
	clk   *clock.Fake
	ids   *id.Sequence
	prov  provenance.Provenance
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	at := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	ids := id.NewSequence(at)
	return &fixture{
		store: outbox.NewMemory(),
		clk:   clock.NewFake(at),
		ids:   ids,
		prov:  provenance.New(provenance.OriginRequest, ids),
	}
}

func (f *fixture) event(t *testing.T, name string) events.Event {
	t.Helper()
	e, err := events.New(f.ids, f.clk, name, f.prov, nil)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (f *fixture) dispatcher(hs ...events.Handler) *outbox.Dispatcher {
	return outbox.New(outbox.Config{
		Store: f.store, Clock: f.clk, Log: logger.Nop(), Handlers: hs,
		MaxAttempts: 3, Batch: 10,
	})
}

func TestDrainDeliversEverythingOwedExactlyOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	var mu sync.Mutex
	seen := map[id.ID]int{}
	d := f.dispatcher(func(_ context.Context, e events.Event) error {
		mu.Lock()
		defer mu.Unlock()
		seen[e.ID]++
		return nil
	})

	pub := outbox.NewPublisher(f.store)
	var want []events.Event
	for i := 0; i < 5; i++ {
		want = append(want, f.event(t, "identity.account.created"))
	}
	if err := pub.Publish(ctx, want...); err != nil {
		t.Fatal(err)
	}
	if err := d.Drain(ctx); err != nil {
		t.Fatal(err)
	}

	for _, e := range want {
		if seen[e.ID] != 1 {
			t.Errorf("%s delivered %d times, want 1", e.ID, seen[e.ID])
		}
	}
	l, _ := f.store.Depth(ctx, f.clk.Now())
	if l.Pending != 0 {
		t.Errorf("%d still owed after a drain; the queue is drained, not a log", l.Pending)
	}
}

func TestAFailedDeliveryBacksOffAndIsThenBuriedRatherThanDeleted(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	boom := errors.New("the subscriber is broken")
	d := f.dispatcher(func(context.Context, events.Event) error { return boom })

	e := f.event(t, "identity.account.created")
	if err := outbox.NewPublisher(f.store).Publish(ctx, e); err != nil {
		t.Fatal(err)
	}

	// Attempt 1 fails and is deferred, so an immediate drain finds nothing due.
	if _, err := d.Dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	// AMENDED: this asserted on Dispatch's return, which is the DELIVERED count
	// and is zero whether or not the event was deferred. Removing the backoff
	// survived it. Ask the store what is due instead.
	if due, _ := f.store.Claim(ctx, 10, f.clk.Now()); len(due) != 0 {
		t.Fatal("the event was due again immediately; the backoff did nothing")
	}

	// Walk the clock forward past each backoff until it buries.
	for i := 0; i < 3; i++ {
		f.clk.Advance(time.Hour)
		if _, err := d.Dispatch(ctx); err != nil {
			t.Fatal(err)
		}
	}

	buried := f.store.Buried()
	if len(buried) != 1 || buried[0].ID != e.ID {
		t.Fatalf("buried %d events, want 1 — a deleted poison event takes the only record of the bug with it", len(buried))
	}
	l, _ := f.store.Depth(ctx, f.clk.Now())
	if l.Pending != 0 || l.Buried != 1 {
		t.Errorf("depth = %+v; a buried row is not owed and is not gone", l)
	}
	f.clk.Advance(24 * time.Hour)
	if n, _ := d.Dispatch(ctx); n != 0 {
		t.Error("a buried event was retried; burial is forever, which is what stops it blocking the queue")
	}
}

func TestOnePoisonEventDoesNotStopItsBatch(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	bad := f.event(t, "identity.account.created")
	good := []events.Event{
		f.event(t, "identity.session.started"),
		f.event(t, "identity.session.ended"),
	}
	if err := outbox.NewPublisher(f.store).Publish(ctx, append([]events.Event{bad}, good...)...); err != nil {
		t.Fatal(err)
	}

	var delivered []id.ID
	d := f.dispatcher(func(_ context.Context, e events.Event) error {
		if e.ID == bad.ID {
			return errors.New("poison")
		}
		delivered = append(delivered, e.ID)
		return nil
	})
	n, err := d.Dispatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(good) {
		t.Fatalf("drained %d of %d good events; one failing subscriber must not stop the others", n, len(good))
	}
	if len(delivered) != 2 {
		t.Fatalf("delivered %d", len(delivered))
	}
}

func TestAPanickingHandlerIsContainedAndCountsAsAFailure(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d := f.dispatcher(func(context.Context, events.Event) error { panic("a subscriber's bug") })

	e := f.event(t, "identity.account.created")
	if err := outbox.NewPublisher(f.store).Publish(ctx, e); err != nil {
		t.Fatal(err)
	}
	n, err := d.Dispatch(ctx)
	if err != nil {
		t.Fatalf("the panic escaped: %v — one subscriber's bug would stop every other event being delivered", err)
	}
	if n != 0 {
		t.Fatal("a panicking delivery was counted as drained")
	}
	if got := d.Stats().Failed; got != 1 {
		t.Errorf("Failed = %d, want 1", got)
	}
}

func TestEveryHandlerRunsAndTheFirstFailureStopsTheRest(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	var ran []string
	d := f.dispatcher(
		func(context.Context, events.Event) error { ran = append(ran, "first"); return nil },
		func(context.Context, events.Event) error { ran = append(ran, "second"); return errors.New("no") },
		func(context.Context, events.Event) error { ran = append(ran, "third"); return nil },
	)
	if err := outbox.NewPublisher(f.store).Publish(ctx, f.event(t, "a.b")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 2 || ran[0] != "first" || ran[1] != "second" {
		t.Fatalf("ran %v; handlers share one delivery attempt, so the first failure ends it and the whole event retries", ran)
	}
}

func TestPublishRefusesAnEventNothingCouldDeduplicate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	pub := outbox.NewPublisher(f.store)

	if err := pub.Publish(ctx, events.Event{Name: "a.b"}); !pkgerrors.IsKind(err, pkgerrors.Internal) {
		t.Errorf("an event with no id was accepted: %v — delivery is at-least-once and the id is the handler's only defence", err)
	}
	e := f.event(t, "a.b")
	e.Name = "notanamespace"
	if err := pub.Publish(ctx, e); !pkgerrors.IsKind(err, pkgerrors.Internal) {
		t.Errorf("an event with a bad name was accepted: %v", err)
	}
	if err := pub.Publish(ctx); err != nil {
		t.Errorf("publishing nothing errored: %v", err)
	}
}

func TestRepublishingTheSameEventWritesOneRow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	pub := outbox.NewPublisher(f.store)
	e := f.event(t, "identity.account.created")

	// A retried command re-publishing the same event id.
	if err := pub.Publish(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := pub.Publish(ctx, e); err != nil {
		t.Fatal(err)
	}
	count := 0
	d := f.dispatcher(func(context.Context, events.Event) error { count++; return nil })
	if err := d.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("delivered %d times; the event id is the dedup key all the way down, not only at the handler", count)
	}
}

func TestNilDependenciesPanicWhereTheyAreWired(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct {
		name string
		want string
		call func()
	}{
		{"a nil Store", "nil Store", func() { outbox.New(outbox.Config{Clock: f.clk, Log: logger.Nop()}) }},
		{"a nil Clock", "nil Clock", func() { outbox.New(outbox.Config{Store: f.store, Log: logger.Nop()}) }},
		{"a nil Logger", "nil Logger", func() { outbox.New(outbox.Config{Store: f.store, Clock: f.clk}) }},
		{"a nil Store on the publisher", "nil Store", func() { outbox.NewPublisher(nil) }},
		// AMENDED, custody 0011 M40: the fourth constructor was missing.
		{"a nil Pool on the postgres store", "nil Pool", func() { outbox.NewPostgres(nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// AMENDED: a bare recover() cannot distinguish the guard from a nil
			// dereference two statements later — custody 0010 M27/M28.
			defer func() {
				v := recover()
				if v == nil {
					t.Error("constructed cleanly; a nil here fails inside a goroutine later, on a delivery nobody is watching")
					return
				}
				if str, ok := v.(string); !ok || !strings.Contains(str, tc.want) {
					t.Errorf("panicked with %v (%T); wanted the guard naming %q", v, v, tc.want)
				}
			}()
			tc.call()
		})
	}
}
