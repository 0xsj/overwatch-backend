package outbox_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/outbox"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

func open(t *testing.T) (*postgres.Pool, *outbox.Postgres) {
	t.Helper()
	dsn := os.Getenv("OVERWATCH_TEST_DSN")
	if dsn == "" {
		t.Skip("OVERWATCH_TEST_DSN is unset — run `make test-db`")
	}
	schema := fmt.Sprintf("t_%d", time.Now().UnixNano())
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	p, err := postgres.Open(context.Background(), postgres.Config{DSN: dsn + sep + "search_path=" + schema})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := p.DB(ctx).Exec(ctx, "create schema "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = p.DB(context.Background()).Exec(context.Background(), "drop schema "+schema+" cascade")
		p.Close()
	})
	if _, err := postgres.Migrate(ctx, p, outbox.Migrations); err != nil {
		t.Fatal(err)
	}
	return p, outbox.NewPostgres(p)
}

func TestTheEventCommitsWithTheFactOrNeitherDoes(t *testing.T) {
	pool, store := open(t)
	ctx := context.Background()
	f := newFixture(t)
	if _, err := pool.DB(ctx).Exec(ctx, `create table fact (id int primary key)`); err != nil {
		t.Fatal(err)
	}
	pub := outbox.NewPublisher(store)

	// The whole property decisions/0007 turns on: roll the transaction back and
	// the event must be gone with the fact.
	rollback := f.event(t, "identity.account.created")
	_ = pool.InTx(ctx, func(ctx context.Context) error {
		if _, err := pool.DB(ctx).Exec(ctx, `insert into fact values (1)`); err != nil {
			return err
		}
		if err := pub.Publish(ctx, rollback); err != nil {
			return err
		}
		return fmt.Errorf("the command failed after publishing")
	})

	commit := f.event(t, "identity.account.created")
	if err := pool.InTx(ctx, func(ctx context.Context) error {
		if _, err := pool.DB(ctx).Exec(ctx, `insert into fact values (2)`); err != nil {
			return err
		}
		return pub.Publish(ctx, commit)
	}); err != nil {
		t.Fatal(err)
	}

	var facts int
	if err := pool.DB(ctx).QueryRow(ctx, `select count(*) from fact`).Scan(&facts); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.Claim(ctx, 10, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if facts != 1 || len(claimed) != 1 {
		t.Fatalf("%d facts and %d events survived; they commit together or not at all", facts, len(claimed))
	}
	if claimed[0].Event.ID != commit.ID {
		t.Error("the surviving event is the rolled-back one")
	}
}

// AMENDED: the first version claimed twice in sequence, which exercised the
// due_at lease and never SKIP LOCKED — removing SKIP LOCKED survived it. Two
// CONCURRENT transactions are the only way to see the difference: without it the
// second claim blocks on the first's row locks until it commits.
func TestASecondDispatcherSkipsLockedRowsRatherThanBlocking(t *testing.T) {
	pool, store := open(t)
	ctx := context.Background()
	f := newFixture(t)
	if err := outbox.NewPublisher(store).Publish(ctx, f.event(t, "a.b")); err != nil {
		t.Fatal(err)
	}

	held := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = pool.InTx(ctx, func(ctx context.Context) error {
			if _, err := store.Claim(ctx, 10, time.Now()); err != nil {
				return err
			}
			close(held)
			<-release // hold the row locks open
			return nil
		})
	}()
	<-held

	// A second dispatcher, on its own connection, while those locks are held.
	probe, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := store.Claim(probe, 10, time.Now())
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("the second claim failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("the second claim blocked on rows another dispatcher holds; SKIP LOCKED is what makes a second process add throughput instead of contention")
	}
	close(release)
}

func TestTheSameEventPublishedTwiceIsOneRowInPostgres(t *testing.T) {
	pool, store := open(t)
	ctx := context.Background()
	f := newFixture(t)
	e := f.event(t, "identity.account.created")
	pub := outbox.NewPublisher(store)

	// A retried command re-publishing the same event id. The memory adapter has
	// its own version of this test; without one here, `on conflict do nothing`
	// could be deleted and nothing would notice.
	if err := pub.Publish(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := pub.Publish(ctx, e); err != nil {
		t.Fatalf("the second publish errored: %v — a retry must be absorbed, not refused", err)
	}
	var n int
	if err := pool.DB(ctx).QueryRow(ctx, `select count(*) from outbox where id = $1`, e.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("%d rows for one event id; the id is the dedup key all the way down, not only at the handler", n)
	}
}

func TestProvenanceSurvivesTheQueueIntact(t *testing.T) {
	_, store := open(t)
	ctx := context.Background()
	f := newFixture(t)

	child, err := f.prov.Derive(f.ids)
	if err != nil {
		t.Fatal(err)
	}
	e, err := events.New(f.ids, f.clk, "identity.account.created", child, map[string]string{"email": "a@b.c"})
	if err != nil {
		t.Fatal(err)
	}
	if err := outbox.NewPublisher(store).Publish(ctx, e); err != nil {
		t.Fatal(err)
	}

	got, err := store.Claim(ctx, 1, time.Now())
	if err != nil || len(got) != 1 {
		t.Fatalf("claim: %v %d", err, len(got))
	}
	back := got[0].Event
	if back.Provenance != child {
		t.Fatalf("provenance did not round-trip:\n got %v\nwant %v", back.Provenance, child)
	}
	if back.Provenance.Causation() != f.prov.Request() {
		t.Error("causation was lost crossing the queue — the one thing a subscriber cannot reconstruct")
	}
	var payload map[string]string
	if err := back.Into(&payload); err != nil || payload["email"] != "a@b.c" {
		t.Fatalf("payload: %v %v", payload, err)
	}
	if back.OccurredAt.UTC() != e.OccurredAt.UTC() {
		t.Errorf("occurred_at moved: %v vs %v", back.OccurredAt, e.OccurredAt)
	}
	_ = provenance.Provenance{}
}

func TestBuryingLeavesTheRowAndItsReason(t *testing.T) {
	pool, store := open(t)
	ctx := context.Background()
	f := newFixture(t)
	e := f.event(t, "a.b")
	if err := outbox.NewPublisher(store).Publish(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := store.Failed(ctx, []id.ID{e.ID}, "the subscriber is broken", time.Time{}); err != nil {
		t.Fatal(err)
	}

	if got, _ := store.Claim(ctx, 10, time.Now().Add(72*time.Hour)); len(got) != 0 {
		t.Error("a buried row was claimed; burial is what stops a poison event blocking the queue")
	}
	var reason string
	var attempts int
	if err := pool.DB(ctx).QueryRow(ctx,
		`select last_error, attempts from outbox where id = $1`, e.ID).Scan(&reason, &attempts); err != nil {
		t.Fatalf("the buried row is gone: %v — the reason it could not be delivered is the only place the bug is visible", err)
	}
	if reason == "" || attempts != 1 {
		t.Errorf("reason=%q attempts=%d", reason, attempts)
	}
	l, err := store.Depth(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if l.Pending != 0 || l.Buried != 1 {
		t.Errorf("depth = %+v; a buried row is not owed and is not gone", l)
	}
}

func TestDepthReportsTheAgeOfTheOldestThingOwed(t *testing.T) {
	_, store := open(t)
	ctx := context.Background()
	f := newFixture(t)
	if err := outbox.NewPublisher(store).Publish(ctx, f.event(t, "a.b")); err != nil {
		t.Fatal(err)
	}
	// AMENDED 2026-09-06, custody 0011 M39: this used ONE pending row, where
	// min and max are the same value — so `min(occurred_at)` could be `max` and
	// nothing noticed. Two rows, and the OLDER one must win.
	newer := f.event(t, "a.c")
	newer.OccurredAt = f.clk.Now().Add(60 * time.Second)
	if err := outbox.NewPublisher(store).Publish(ctx, newer); err != nil {
		t.Fatal(err)
	}

	l, err := store.Depth(ctx, f.clk.Now().Add(90*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if l.Pending != 2 {
		t.Fatalf("pending = %d", l.Pending)
	}
	if l.Oldest < 89*time.Second || l.Oldest > 91*time.Second {
		t.Errorf("oldest = %v, want ~90s from the OLDER of two rows; taking the newest reports a queue that is never behind", l.Oldest)
	}
}
