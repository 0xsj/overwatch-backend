// Tests requiring no database: dispatcher policy against the memory adapter,
// the Publisher, Depth/Drain/Run/Stats, and a Store-conformance suite shared
// with the Postgres adapter in outbox_spec_db_test.AS-WRITTEN.go.
package outbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/logger"
	"github.com/0xsj/overwatch-backend/pkg/outbox"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// ---- shared helpers ---------------------------------------------------

func specCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func specNewMinter() *id.Sequence {
	return id.NewSequence(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
}

func specEventClock() *clock.Fake {
	return clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
}

// specSyncedFake seeds a dispatcher's clock with the real wall clock. Memory
// and Postgres both set due_at from real time at Add (neither adapter takes a
// clock), so a dispatcher-side clock must start in sync with it or freshly
// added rows would never appear due. This is a testing assumption made to get
// deterministic control over backoff, not a claim about behavior.
func specSyncedFake() *clock.Fake {
	return clock.NewFake(time.Now())
}

func specMustEvent(t *testing.T, m *id.Sequence, name string, payload any) events.Event {
	t.Helper()
	origin, ok := provenance.ParseOrigin("request")
	if !ok {
		t.Fatalf(`provenance.ParseOrigin("request") failed — the spec lists "request" as a valid origin name`)
	}
	prov := provenance.New(origin, m)
	ev, err := events.New(m, specEventClock(), name, prov, payload)
	if err != nil {
		t.Fatalf("events.New(%q): %v — this fixture must be valid for the outbox tests to mean anything", name, err)
	}
	return ev
}

// specConformStore is the port's contract, run against whichever Store is
// handed to it. It is intentionally blind to which adapter it is testing:
// the whole point is that Memory and Postgres are two implementations of one
// promise.
func specConformStore(t *testing.T, newStore func(t *testing.T) outbox.Store) {
	t.Helper()

	t.Run("an added event can be claimed back with the same identity", func(t *testing.T) {
		store := newStore(t)
		ctx := specCtx(t)
		seq := specNewMinter()
		ev := specMustEvent(t, seq, "outbox.spec_roundtrip", map[string]int{"n": 42})
		if err := store.Add(ctx, ev); err != nil {
			t.Fatalf("Add: %v", err)
		}
		got, err := store.Claim(ctx, 10, time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("an event just added must be claimable exactly once; got %d results", len(got))
		}
		if got[0].Event.ID != ev.ID {
			t.Errorf("Claim returned a different event than was added: got %s, want %s", got[0].Event.ID, ev.ID)
		}
		if got[0].Event.Name != ev.Name {
			t.Errorf("the event's name must survive storage: got %q, want %q", got[0].Event.Name, ev.Name)
		}
		if got[0].Attempts != 0 {
			t.Errorf("a freshly added event has never been attempted; got Attempts=%d, want 0", got[0].Attempts)
		}
		var payload struct {
			N int `json:"n"`
		}
		if err := json.Unmarshal(got[0].Event.Payload, &payload); err != nil {
			t.Fatalf("payload did not survive storage as valid JSON: %v", err)
		}
		if payload.N != 42 {
			t.Errorf("payload value did not survive storage: got %d, want 42", payload.N)
		}
		if got[0].Event.Provenance.Correlation() != ev.Provenance.Correlation() {
			t.Errorf("provenance correlation id must survive storage — it is the lineage this package exists to carry")
		}
		if got[0].Event.Provenance.Origin() != ev.Provenance.Origin() {
			t.Errorf("provenance origin must survive storage")
		}
	})

	t.Run("the queue is drained, not kept: a delivered row is gone", func(t *testing.T) {
		store := newStore(t)
		ctx := specCtx(t)
		seq := specNewMinter()
		ev := specMustEvent(t, seq, "outbox.spec_drain", nil)
		if err := store.Add(ctx, ev); err != nil {
			t.Fatalf("Add: %v", err)
		}
		now := time.Now().Add(time.Hour)
		claimed, err := store.Claim(ctx, 10, now)
		if err != nil || len(claimed) != 1 {
			t.Fatalf("setup: Claim: err=%v n=%d", err, len(claimed))
		}
		if err := store.Delivered(ctx, []id.ID{claimed[0].Event.ID}); err != nil {
			t.Fatalf("Delivered: %v", err)
		}
		again, err := store.Claim(ctx, 10, now.Add(365*24*time.Hour))
		if err != nil {
			t.Fatalf("Claim after delivery: %v", err)
		}
		if len(again) != 0 {
			t.Errorf("a delivered row must be gone, not merely marked — this is a queue, not a log; it reappeared in Claim")
		}
		lvl, err := store.Depth(ctx, now)
		if err != nil {
			t.Fatalf("Depth: %v", err)
		}
		if lvl.Pending != 0 || lvl.Buried != 0 {
			t.Errorf("a delivered row must not be counted anywhere in Depth; got %+v", lvl)
		}
	})

	t.Run("claiming marks a row so a concurrent claim does not take it again", func(t *testing.T) {
		store := newStore(t)
		ctx := specCtx(t)
		seq := specNewMinter()
		evs := []events.Event{
			specMustEvent(t, seq, "outbox.spec_claim_a", nil),
			specMustEvent(t, seq, "outbox.spec_claim_b", nil),
			specMustEvent(t, seq, "outbox.spec_claim_c", nil),
		}
		if err := store.Add(ctx, evs...); err != nil {
			t.Fatalf("Add: %v", err)
		}
		now := time.Now().Add(time.Hour)
		first, err := store.Claim(ctx, 2, now)
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		if len(first) != 2 {
			t.Fatalf("requested 2 of 3 pending events, got %d", len(first))
		}
		second, err := store.Claim(ctx, 2, now)
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		if len(second) != 1 {
			t.Fatalf("Claim must mark rows it returns so a second claim before Delivered/Failed takes different ones; got %d, want the 1 remaining row", len(second))
		}
		seen := map[id.ID]bool{}
		for _, p := range append(first, second...) {
			if seen[p.Event.ID] {
				t.Errorf("the same event was claimed twice before it was delivered or failed")
			}
			seen[p.Event.ID] = true
		}
	})

	t.Run("Claim never returns more than requested", func(t *testing.T) {
		store := newStore(t)
		ctx := specCtx(t)
		seq := specNewMinter()
		evs := make([]events.Event, 5)
		for i := range evs {
			evs[i] = specMustEvent(t, seq, "outbox.spec_bound", nil)
		}
		if err := store.Add(ctx, evs...); err != nil {
			t.Fatalf("Add: %v", err)
		}
		got, err := store.Claim(ctx, 3, time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		if len(got) > 3 {
			t.Errorf("Claim(3, ...) is a promise of 'up to' 3; got %d", len(got))
		}
	})

	t.Run("Claim(0, ...) returns nothing", func(t *testing.T) {
		store := newStore(t)
		ctx := specCtx(t)
		seq := specNewMinter()
		if err := store.Add(ctx, specMustEvent(t, seq, "outbox.spec_zero", nil)); err != nil {
			t.Fatalf("Add: %v", err)
		}
		got, err := store.Claim(ctx, 0, time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("Claim(0, ...) must return nothing; got %d", len(got))
		}
	})

	t.Run("Failed with a future retryAt backs off and records the attempt", func(t *testing.T) {
		store := newStore(t)
		ctx := specCtx(t)
		seq := specNewMinter()
		ev := specMustEvent(t, seq, "outbox.spec_backoff", nil)
		if err := store.Add(ctx, ev); err != nil {
			t.Fatalf("Add: %v", err)
		}
		now := time.Now().Add(time.Hour)
		claimed, err := store.Claim(ctx, 10, now)
		if err != nil || len(claimed) != 1 {
			t.Fatalf("setup: err=%v n=%d", err, len(claimed))
		}
		retryAt := now.Add(24 * time.Hour)
		if err := store.Failed(ctx, []id.ID{claimed[0].Event.ID}, "spec: forced failure", retryAt); err != nil {
			t.Fatalf("Failed: %v", err)
		}
		tooSoon, err := store.Claim(ctx, 10, now)
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		if len(tooSoon) != 0 {
			t.Errorf("a failed delivery must back off before it is retried; it was claimable before its retryAt")
		}
		lvl, err := store.Depth(ctx, now)
		if err != nil {
			t.Fatalf("Depth: %v", err)
		}
		if lvl.Buried != 0 {
			t.Errorf("a non-empty retryAt must not bury the row; Buried=%d", lvl.Buried)
		}
		due, err := store.Claim(ctx, 10, retryAt.Add(time.Millisecond))
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		if len(due) != 1 {
			t.Errorf("the row must become claimable once its retryAt has passed")
		} else if due[0].Attempts != 1 {
			t.Errorf("Failed must record that an attempt was made; got Attempts=%d, want 1", due[0].Attempts)
		}
	})

	t.Run("INFERENCE: Depth counts a row still waiting out its backoff as pending", func(t *testing.T) {
		// The spec states Depth reports what is "owed" but does not say
		// whether a row backed off into the future counts. Asserted here on
		// the reading that the health-metric framing ("a number that keeps
		// rising means the dispatcher is behind") is about total backlog,
		// not just currently-claimable rows.
		store := newStore(t)
		ctx := specCtx(t)
		seq := specNewMinter()
		ev := specMustEvent(t, seq, "outbox.spec_owed", nil)
		if err := store.Add(ctx, ev); err != nil {
			t.Fatalf("Add: %v", err)
		}
		now := time.Now().Add(time.Hour)
		claimed, _ := store.Claim(ctx, 10, now)
		if len(claimed) != 1 {
			t.Fatalf("setup: expected 1 claimed")
		}
		if err := store.Failed(ctx, []id.ID{claimed[0].Event.ID}, "spec: backoff", now.Add(24*time.Hour)); err != nil {
			t.Fatalf("Failed: %v", err)
		}
		lvl, err := store.Depth(ctx, now)
		if err != nil {
			t.Fatalf("Depth: %v", err)
		}
		if lvl.Pending != 1 {
			t.Errorf("INFERENCE not stated by the spec text: got Pending=%d, want 1 (a row waiting out backoff still owed)", lvl.Pending)
		}
	})

	t.Run("Failed with a zero retryAt buries the row", func(t *testing.T) {
		store := newStore(t)
		ctx := specCtx(t)
		seq := specNewMinter()
		ev := specMustEvent(t, seq, "outbox.spec_bury", nil)
		if err := store.Add(ctx, ev); err != nil {
			t.Fatalf("Add: %v", err)
		}
		now := time.Now().Add(time.Hour)
		claimed, _ := store.Claim(ctx, 10, now)
		if len(claimed) != 1 {
			t.Fatalf("setup: expected 1 claimed")
		}
		if err := store.Failed(ctx, []id.ID{claimed[0].Event.ID}, "spec: poison", time.Time{}); err != nil {
			t.Fatalf("Failed: %v", err)
		}
		lvl, err := store.Depth(ctx, now)
		if err != nil {
			t.Fatalf("Depth: %v", err)
		}
		if lvl.Buried != 1 {
			t.Errorf("an empty retryAt must bury the row per the Store contract; Buried=%d, want 1", lvl.Buried)
		}
		if lvl.Pending != 0 {
			t.Errorf("a buried row must not also count as pending; Pending=%d, want 0", lvl.Pending)
		}
	})

	t.Run("a buried row is skipped by Claim forever, not deleted", func(t *testing.T) {
		store := newStore(t)
		ctx := specCtx(t)
		seq := specNewMinter()
		ev := specMustEvent(t, seq, "outbox.spec_forever", nil)
		if err := store.Add(ctx, ev); err != nil {
			t.Fatalf("Add: %v", err)
		}
		now := time.Now().Add(time.Hour)
		claimed, _ := store.Claim(ctx, 10, now)
		if len(claimed) != 1 {
			t.Fatalf("setup: expected 1 claimed")
		}
		if err := store.Failed(ctx, []id.ID{claimed[0].Event.ID}, "spec: poison", time.Time{}); err != nil {
			t.Fatalf("Failed: %v", err)
		}
		farFuture := now.Add(365 * 24 * time.Hour)
		again, err := store.Claim(ctx, 10, farFuture)
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		if len(again) != 0 {
			t.Errorf("a buried row must be skipped forever; it was returned by a later Claim")
		}
		lvl, err := store.Depth(ctx, farFuture)
		if err != nil {
			t.Fatalf("Depth: %v", err)
		}
		if lvl.Buried != 1 {
			t.Errorf("burial marks the row dead and leaves it in place, not deleted; Buried=%d, want 1", lvl.Buried)
		}
	})

	t.Run("Depth.Oldest advances exactly with the caller's clock", func(t *testing.T) {
		store := newStore(t)
		ctx := specCtx(t)
		seq := specNewMinter()
		if err := store.Add(ctx, specMustEvent(t, seq, "outbox.spec_oldest", nil)); err != nil {
			t.Fatalf("Add: %v", err)
		}
		anchor := time.Now().Add(time.Hour)
		lvl1, err := store.Depth(ctx, anchor)
		if err != nil {
			t.Fatalf("Depth: %v", err)
		}
		later := anchor.Add(30 * time.Minute)
		lvl2, err := store.Depth(ctx, later)
		if err != nil {
			t.Fatalf("Depth: %v", err)
		}
		diff := lvl2.Oldest - lvl1.Oldest
		if diff != 30*time.Minute {
			t.Errorf("Oldest is the age of the oldest owed row relative to 'now'; asking 30m later with no store mutation in between must add exactly 30m, got a difference of %v", diff)
		}
	})

	t.Run("an empty store reports zero pending and zero buried", func(t *testing.T) {
		store := newStore(t)
		ctx := specCtx(t)
		lvl, err := store.Depth(ctx, time.Now())
		if err != nil {
			t.Fatalf("Depth: %v", err)
		}
		if lvl.Pending != 0 || lvl.Buried != 0 {
			t.Errorf("nothing has ever been added; got %+v, want zero pending and buried", lvl)
		}
	})
}

func TestSpecStoreConformanceMemory(t *testing.T) {
	specConformStore(t, func(t *testing.T) outbox.Store {
		return outbox.NewMemory()
	})
}

func TestSpecPublisherPublishesThroughStore(t *testing.T) {
	m := outbox.NewMemory()
	pub := outbox.NewPublisher(m)
	ctx := specCtx(t)
	seq := specNewMinter()
	ev := specMustEvent(t, seq, "outbox.spec_publish", nil)
	if err := pub.Publish(ctx, ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	got, err := m.Claim(ctx, 10, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if len(got) != 1 || got[0].Event.ID != ev.ID {
		t.Fatalf("Publisher.Publish must hand its events to the Store it wraps; got %d results", len(got))
	}
}

func TestSpecDispatcherSharedDeliveryAttempt(t *testing.T) {
	m := outbox.NewMemory()
	ctx := specCtx(t)
	seq := specNewMinter()
	ev := specMustEvent(t, seq, "outbox.spec_shared", nil)
	if err := m.Add(ctx, ev); err != nil {
		t.Fatalf("Add: %v", err)
	}

	var aCalls, bCalls, bFailed int32
	handlerA := func(_ context.Context, _ events.Event) error {
		atomic.AddInt32(&aCalls, 1)
		return nil
	}
	handlerB := func(_ context.Context, _ events.Event) error {
		atomic.AddInt32(&bCalls, 1)
		if atomic.CompareAndSwapInt32(&bFailed, 0, 1) {
			return errors.New("spec: forced failure on first delivery")
		}
		return nil
	}
	fake := specSyncedFake()
	d := outbox.New(outbox.Config{
		Store: m, Clock: fake, Log: logger.Nop(),
		Handlers: []events.Handler{handlerA, handlerB}, Batch: 10, Interval: time.Millisecond,
		MaxAttempts: 5, Backoff: func(int) time.Duration { return 0 },
	})
	if _, err := d.Dispatch(ctx); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	fake.Advance(time.Millisecond)
	if _, err := d.Dispatch(ctx); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if atomic.LoadInt32(&aCalls) != 2 {
		t.Errorf("all handlers share one delivery attempt: handler A must see the event again after B's first failure; A was called %d times, want 2", aCalls)
	}
	if atomic.LoadInt32(&bCalls) != 2 {
		t.Errorf("handler B should have been retried once after its own failure; called %d times, want 2", bCalls)
	}
}

func TestSpecDispatcherPoisonIsolation(t *testing.T) {
	m := outbox.NewMemory()
	ctx := specCtx(t)
	seq := specNewMinter()
	poison := specMustEvent(t, seq, "outbox.spec_poison", nil)
	healthy := specMustEvent(t, seq, "outbox.spec_healthy", nil)
	if err := m.Add(ctx, poison, healthy); err != nil {
		t.Fatalf("Add: %v", err)
	}

	handler := func(_ context.Context, e events.Event) error {
		if e.ID == poison.ID {
			return errors.New("spec: this event always fails")
		}
		return nil
	}
	d := outbox.New(outbox.Config{
		Store: m, Clock: specSyncedFake(), Log: logger.Nop(),
		Handlers: []events.Handler{handler}, Batch: 10, Interval: time.Millisecond,
		MaxAttempts: 100, Backoff: func(int) time.Duration { return 0 },
	})
	if _, err := d.Dispatch(ctx); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	remaining, err := m.Claim(ctx, 10, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if len(remaining) != 1 || remaining[0].Event.ID != poison.ID {
		t.Fatalf("one poisoned event must not stop the rest of its batch: the healthy event must be delivered and gone, leaving only the poisoned one pending")
	}
}

func TestSpecDispatcherBackoffBeforeRetry(t *testing.T) {
	m := outbox.NewMemory()
	ctx := specCtx(t)
	seq := specNewMinter()
	ev := specMustEvent(t, seq, "outbox.spec_backoff_retry", nil)
	if err := m.Add(ctx, ev); err != nil {
		t.Fatalf("Add: %v", err)
	}

	var calls int32
	handler := func(_ context.Context, _ events.Event) error {
		if atomic.AddInt32(&calls, 1) == 1 {
			return errors.New("spec: forced first failure")
		}
		return nil
	}
	fake := specSyncedFake()
	d := outbox.New(outbox.Config{
		Store: m, Clock: fake, Log: logger.Nop(),
		Handlers: []events.Handler{handler}, Batch: 10, Interval: time.Millisecond,
		MaxAttempts: 10, Backoff: func(int) time.Duration { return time.Minute },
	})
	if _, err := d.Dispatch(ctx); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("setup: expected exactly one delivery attempt before backoff, got %d", calls)
	}
	if _, err := d.Dispatch(ctx); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("a failed delivery must back off before retrying; the handler was called again before its backoff elapsed")
	}
	fake.Advance(time.Minute)
	if _, err := d.Dispatch(ctx); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Errorf("once the backoff has elapsed the event must be retried; handler was called %d times, want 2", calls)
	}
}

func TestSpecDispatcherBurialPastMaxAttempts(t *testing.T) {
	m := outbox.NewMemory()
	ctx := specCtx(t)
	seq := specNewMinter()
	ev := specMustEvent(t, seq, "outbox.spec_bury_dispatch", nil)
	if err := m.Add(ctx, ev); err != nil {
		t.Fatalf("Add: %v", err)
	}

	var calls int32
	handler := func(_ context.Context, _ events.Event) error {
		atomic.AddInt32(&calls, 1)
		return errors.New("spec: this handler never succeeds")
	}
	const maxAttempts = 3
	fake := specSyncedFake()
	d := outbox.New(outbox.Config{
		Store: m, Clock: fake, Log: logger.Nop(),
		Handlers: []events.Handler{handler}, Batch: 10, Interval: time.Millisecond,
		MaxAttempts: maxAttempts, Backoff: func(int) time.Duration { return 0 },
	})
	for i := 0; i < maxAttempts+5; i++ {
		if _, err := d.Dispatch(ctx); err != nil {
			t.Fatalf("Dispatch: %v", err)
		}
		fake.Advance(time.Millisecond)
	}

	buried := m.Buried()
	if len(buried) != 1 || buried[0].ID != ev.ID {
		t.Fatalf("a handler that always fails must eventually bury the event past MaxAttempts; buried=%d", len(buried))
	}
	callsAtBurial := atomic.LoadInt32(&calls)
	if _, err := d.Dispatch(ctx); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if atomic.LoadInt32(&calls) != callsAtBurial {
		t.Errorf("a buried row must be skipped forever; the handler was invoked again after burial")
	}
	lvl, err := d.Depth(ctx)
	if err != nil {
		t.Fatalf("Depth: %v", err)
	}
	if lvl.Buried != 1 || lvl.Pending != 0 {
		t.Errorf("Depth must count the buried row separately from pending; got %+v", lvl)
	}
}

func TestSpecDispatcherDispatchReturnsDrainCount(t *testing.T) {
	m := outbox.NewMemory()
	ctx := specCtx(t)
	seq := specNewMinter()
	const n = 4
	evs := make([]events.Event, n)
	for i := range evs {
		evs[i] = specMustEvent(t, seq, "outbox.spec_count", nil)
	}
	if err := m.Add(ctx, evs...); err != nil {
		t.Fatalf("Add: %v", err)
	}

	d := outbox.New(outbox.Config{
		Store: m, Clock: specSyncedFake(), Log: logger.Nop(),
		Handlers: []events.Handler{func(context.Context, events.Event) error { return nil }},
		Batch:    10, Interval: time.Millisecond,
		MaxAttempts: 5, Backoff: func(int) time.Duration { return time.Minute },
	})
	before, err := d.Depth(ctx)
	if err != nil {
		t.Fatalf("Depth: %v", err)
	}
	if before.Pending != n || before.Buried != 0 {
		t.Fatalf("setup: expected %d pending and 0 buried before dispatch, got %+v", n, before)
	}
	got, err := d.Dispatch(ctx)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if got != n {
		t.Errorf("Dispatch must return how many were drained so a caller can loop until zero; got %d, want %d", got, n)
	}
	again, err := d.Dispatch(ctx)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if again != 0 {
		t.Errorf("nothing is left to deliver; Dispatch returned %d, want 0", again)
	}
}

func TestSpecDispatcherDrainUntilNothingOwed(t *testing.T) {
	m := outbox.NewMemory()
	ctx := specCtx(t)
	seq := specNewMinter()
	const n = 7
	evs := make([]events.Event, n)
	for i := range evs {
		evs[i] = specMustEvent(t, seq, "outbox.spec_drain_all", nil)
	}
	if err := m.Add(ctx, evs...); err != nil {
		t.Fatalf("Add: %v", err)
	}

	var delivered int32
	handler := func(_ context.Context, _ events.Event) error {
		atomic.AddInt32(&delivered, 1)
		return nil
	}
	d := outbox.New(outbox.Config{
		Store: m, Clock: specSyncedFake(), Log: logger.Nop(),
		Handlers: []events.Handler{handler}, Batch: 2, Interval: time.Millisecond,
		MaxAttempts: 5, Backoff: func(int) time.Duration { return time.Minute },
	})
	if err := d.Drain(ctx); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if atomic.LoadInt32(&delivered) != n {
		t.Errorf("Drain must dispatch batches until nothing is owed, even with a batch smaller than the queue; delivered %d of %d", delivered, n)
	}
	lvl, err := d.Depth(ctx)
	if err != nil {
		t.Fatalf("Depth: %v", err)
	}
	if lvl.Pending != 0 {
		t.Errorf("after Drain nothing should remain owed; Pending=%d", lvl.Pending)
	}
}

func TestSpecDispatcherRunReturnsOnCancellation(t *testing.T) {
	m := outbox.NewMemory()
	d := outbox.New(outbox.Config{
		Store: m, Clock: specSyncedFake(), Log: logger.Nop(),
		Handlers: []events.Handler{func(context.Context, events.Event) error { return nil }},
		Batch:    10, Interval: time.Millisecond, MaxAttempts: 5,
		Backoff: func(int) time.Duration { return time.Minute },
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("cancellation is not a failure; Run returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return promptly after its context was cancelled")
	}
}

func TestSpecDispatcherStatsMonotonic(t *testing.T) {
	m := outbox.NewMemory()
	ctx := specCtx(t)
	seq := specNewMinter()
	if err := m.Add(ctx, specMustEvent(t, seq, "outbox.spec_stats", nil)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	d := outbox.New(outbox.Config{
		Store: m, Clock: specSyncedFake(), Log: logger.Nop(),
		Handlers: []events.Handler{func(context.Context, events.Event) error { return nil }},
		Batch:    10, Interval: time.Millisecond, MaxAttempts: 5,
		Backoff: func(int) time.Duration { return time.Minute },
	})
	before := d.Stats()
	if _, err := d.Dispatch(ctx); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	after := d.Stats()
	if after.Delivered <= before.Delivered {
		t.Errorf("Stats().Delivered must increase after a successful delivery; before=%d after=%d", before.Delivered, after.Delivered)
	}
	if after.Runs <= before.Runs {
		t.Errorf("Stats().Runs must increase after Dispatch runs; before=%d after=%d", before.Runs, after.Runs)
	}
}
