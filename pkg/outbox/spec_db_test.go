// Tests requiring Postgres. Skips whenever OVERWATCH_TEST_DSN is unset.
// Helpers here reuse specCtx, specNewMinter and specMustEvent defined in
// outbox_spec_test.AS-WRITTEN.go — same package, one compilation unit.
package outbox_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/outbox"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

// specOpenDB follows the isolation convention exactly: DSN from the
// environment, a unique schema appended as search_path, create it, migrate
// this package's own migrations into it, and clean up after.
func specOpenDB(t *testing.T) *postgres.Pool {
	t.Helper()
	dsn := os.Getenv("OVERWATCH_TEST_DSN")
	if dsn == "" {
		t.Skip("OVERWATCH_TEST_DSN is not set; skipping database-backed outbox tests")
	}
	schema := fmt.Sprintf("outbox_spec_%d", time.Now().UnixNano())
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := postgres.Open(ctx, postgres.Config{DSN: dsn + sep + "search_path=" + schema})
	if err != nil {
		t.Fatalf("postgres.Open: %v", err)
	}
	if _, err := pool.DB(ctx).Exec(ctx, "create schema "+schema); err != nil {
		pool.Close()
		t.Fatalf("create schema %s: %v", schema, err)
	}
	if _, err := postgres.Migrate(ctx, pool, outbox.Migrations); err != nil {
		pool.Close()
		t.Fatalf("postgres.Migrate: %v", err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer ccancel()
		_, _ = pool.DB(cctx).Exec(cctx, "drop schema "+schema+" cascade")
		pool.Close()
	})
	return pool
}

func TestSpecStoreConformancePostgres(t *testing.T) {
	pool := specOpenDB(t)
	specConformStore(t, func(t *testing.T) outbox.Store {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := pool.DB(ctx).Exec(ctx, "delete from outbox"); err != nil {
			t.Fatalf("reset outbox table between conformance subtests: %v", err)
		}
		return outbox.NewPostgres(pool)
	})
}

func TestSpecFactCommitsAtomicallyWithEvent(t *testing.T) {
	pool := specOpenDB(t)
	ctx := specCtx(t)
	if _, err := pool.DB(ctx).Exec(ctx, "create table spec_fact (id uuid primary key)"); err != nil {
		t.Fatalf("setup: create scratch fact table: %v", err)
	}
	store := outbox.NewPostgres(pool)
	pub := outbox.NewPublisher(store)
	seq := specNewMinter()

	t.Run("a fact and its event commit together", func(t *testing.T) {
		ev := specMustEvent(t, seq, "outbox.spec_fact_commit", nil)
		err := pool.InTx(ctx, func(ctx context.Context) error {
			if _, err := pool.DB(ctx).Exec(ctx, "insert into spec_fact(id) values ($1)", ev.ID.String()); err != nil {
				return err
			}
			return pub.Publish(ctx, ev)
		})
		if err != nil {
			t.Fatalf("InTx: %v", err)
		}
		var factCount int
		if err := pool.DB(ctx).QueryRow(ctx, "select count(*) from spec_fact where id = $1", ev.ID.String()).Scan(&factCount); err != nil {
			t.Fatalf("query spec_fact: %v", err)
		}
		if factCount != 1 {
			t.Fatalf("setup: fact row missing after a committed transaction")
		}
		claimed, err := store.Claim(ctx, 10, time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		found := false
		for _, p := range claimed {
			if p.Event.ID == ev.ID {
				found = true
			}
		}
		if !found {
			t.Errorf("the fact committed but its event did not: the fact and the event must commit together, or not at all")
		}
	})

	t.Run("a fact and its event roll back together", func(t *testing.T) {
		ev := specMustEvent(t, seq, "outbox.spec_fact_rollback", nil)
		wantErr := errors.New("spec: forced rollback after publish")
		err := pool.InTx(ctx, func(ctx context.Context) error {
			if _, err := pool.DB(ctx).Exec(ctx, "insert into spec_fact(id) values ($1)", ev.ID.String()); err != nil {
				return err
			}
			if err := pub.Publish(ctx, ev); err != nil {
				return err
			}
			return wantErr
		})
		if !errors.Is(err, wantErr) {
			t.Fatalf("InTx: got %v, want the forced error to propagate", err)
		}
		var factCount int
		if err := pool.DB(ctx).QueryRow(ctx, "select count(*) from spec_fact where id = $1", ev.ID.String()).Scan(&factCount); err != nil {
			t.Fatalf("query spec_fact: %v", err)
		}
		if factCount != 0 {
			t.Fatalf("setup invariant broken: the fact row survived a rolled-back transaction")
		}
		claimed, err := store.Claim(ctx, 100, time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		for _, p := range claimed {
			if p.Event.ID == ev.ID {
				t.Errorf("the fact rolled back but its event survived: the fact and the event must commit together, or not at all")
			}
		}
	})
}

func TestSpecPublishOutsideTransactionStillWrites(t *testing.T) {
	// Documents the risk the spec names directly: "Publishing outside a
	// transaction compiles, runs, and silently gives up the one property this
	// package exists for. Nothing can detect it from inside." This asserts
	// only that the write itself still silently succeeds — that is the
	// danger, not something this test can catch from the caller's side.
	pool := specOpenDB(t)
	ctx := specCtx(t)
	store := outbox.NewPostgres(pool)
	pub := outbox.NewPublisher(store)
	seq := specNewMinter()
	ev := specMustEvent(t, seq, "outbox.spec_no_tx", nil)

	if err := pub.Publish(ctx, ev); err != nil {
		t.Fatalf("Publish outside a transaction: %v", err)
	}
	claimed, err := store.Claim(ctx, 10, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	found := false
	for _, p := range claimed {
		if p.Event.ID == ev.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("Publish outside a transaction was expected to still write the row silently (that is the danger, not a safeguard); the row was not found")
	}
}

func TestSpecConcurrentClaimsAreExclusive(t *testing.T) {
	pool := specOpenDB(t)
	ctx := specCtx(t)
	store := outbox.NewPostgres(pool)
	seq := specNewMinter()
	const total = 20
	evs := make([]events.Event, total)
	for i := range evs {
		evs[i] = specMustEvent(t, seq, "outbox.spec_concurrent", nil)
	}
	if err := store.Add(ctx, evs...); err != nil {
		t.Fatalf("Add: %v", err)
	}

	now := time.Now().Add(time.Hour)
	results := make([][]outbox.Pending, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := store.Claim(ctx, total/2, now)
			if err != nil {
				t.Errorf("Claim from goroutine %d: %v", i, err)
				return
			}
			results[i] = got
		}(i)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("two concurrent Claim calls should not block on each other: FOR UPDATE SKIP LOCKED exists exactly so a second dispatcher takes different rows instead of waiting")
	}

	seen := map[id.ID]bool{}
	totalClaimed := 0
	for _, r := range results {
		for _, p := range r {
			if seen[p.Event.ID] {
				t.Errorf("the same event was claimed by two concurrent dispatchers; SKIP LOCKED must give each dispatcher different rows")
			}
			seen[p.Event.ID] = true
			totalClaimed++
		}
	}
	if totalClaimed != total {
		t.Errorf("two dispatchers claiming concurrently should together account for every pending event with none lost or double-claimed; got %d of %d", totalClaimed, total)
	}
}
