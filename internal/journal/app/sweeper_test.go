package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/journal/app"
	"github.com/0xsj/overwatch-backend/pkg/errors"
)

type fixed struct{ at time.Time }

func (f fixed) Now() time.Time { return f.at }

// spy records the cutoffs it was asked for and hands back a scripted number of
// deletions per call, so a multi-batch sweep is observable without a database.
type spy struct {
	cutoffs []time.Time
	batches []int
	deleted []int
	fail    error
}

func (s *spy) ExpireBefore(_ context.Context, cutoff time.Time, batch int) (int, error) {
	s.cutoffs = append(s.cutoffs, cutoff)
	s.batches = append(s.batches, batch)
	if s.fail != nil {
		return 0, s.fail
	}
	if len(s.deleted) == 0 {
		return 0, nil
	}
	n := s.deleted[0]
	s.deleted = s.deleted[1:]
	return n, nil
}

func sweeper(t *testing.T, store app.Expirer, retention time.Duration, batch int) *app.Sweeper {
	t.Helper()
	s, err := app.NewSweeper(app.SweeperConfig{
		Store: store, Clock: fixed{at: time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)},
		Retention: retention, Batch: batch,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The floor is the whole reason this is constructed at boot rather than at the
// first tick — decisions/0022. A misconfigured deployment must die before it
// deletes anything.
func TestARetentionBelowTheFloorRefusesToConstruct(t *testing.T) {
	for _, d := range []time.Duration{time.Nanosecond, time.Hour, 6 * 24 * time.Hour} {
		_, err := app.NewSweeper(app.SweeperConfig{
			Store: &spy{}, Clock: fixed{}, Retention: d,
		})
		if !errors.Is(err, app.ErrRetentionTooShort) {
			t.Errorf("retention %s was accepted: %v", d, err)
		}
	}
	// Exactly the floor is allowed. A boundary that refuses its own value is
	// the kind of off-by-one nobody notices until a deployment will not start.
	if _, err := app.NewSweeper(app.SweeperConfig{
		Store: &spy{}, Clock: fixed{}, Retention: app.MinRetention,
	}); err != nil {
		t.Errorf("the floor itself was refused: %v", err)
	}
	// Zero means "unset" and takes the default, which is above the floor.
	s, err := app.NewSweeper(app.SweeperConfig{Store: &spy{}, Clock: fixed{}})
	if err != nil {
		t.Fatalf("an unset retention was refused: %v", err)
	}
	if s.Retention() != app.DefaultRetention {
		t.Errorf("unset gave %s, want the default %s", s.Retention(), app.DefaultRetention)
	}
}

func TestTheCutoffIsTheRetentionBehindNow(t *testing.T) {
	store := &spy{}
	s := sweeper(t, store, 30*24*time.Hour, 100)
	if _, err := s.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.cutoffs) != 1 {
		t.Fatalf("%d passes for an empty table, want 1", len(store.cutoffs))
	}
	want := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	if !store.cutoffs[0].Equal(want) {
		t.Errorf("cutoff %s, want %s", store.cutoffs[0], want)
	}
}

// It batches until a batch comes back short, and the cutoff is computed ONCE so
// a long sweep deletes a consistent window rather than a creeping one.
func TestASweepBatchesUntilShortWithOneCutoff(t *testing.T) {
	store := &spy{deleted: []int{100, 100, 42}}
	s := sweeper(t, store, 30*24*time.Hour, 100)

	n, err := s.Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 242 {
		t.Errorf("deleted %d, want 242", n)
	}
	if len(store.cutoffs) != 3 {
		t.Fatalf("%d passes, want 3", len(store.cutoffs))
	}
	for i, c := range store.cutoffs {
		if !c.Equal(store.cutoffs[0]) {
			t.Errorf("pass %d used a different cutoff: %s vs %s", i, c, store.cutoffs[0])
		}
	}
	for i, b := range store.batches {
		if b != 100 {
			t.Errorf("pass %d asked for %d, want the configured 100", i, b)
		}
	}
}

// A full final batch is NOT the end. Stopping there would leave exactly a batch
// of expired rows behind on every sweep, forever, and the table would shrink
// while never emptying — which looks like it is working.
func TestAFullFinalBatchIsNotTheEnd(t *testing.T) {
	store := &spy{deleted: []int{10, 10}}
	s := sweeper(t, store, 30*24*time.Hour, 10)
	n, err := s.Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// 10, 10, then 0 — three calls, because the second was full.
	if len(store.cutoffs) != 3 {
		t.Errorf("%d passes after two full batches, want 3", len(store.cutoffs))
	}
	if n != 20 {
		t.Errorf("deleted %d, want 20", n)
	}
}

// An interrupted sweep reports what it managed rather than failing. What was
// deleted stays deleted, and the next pass recomputes the cutoff and carries on.
func TestAnInterruptedSweepReportsProgressRatherThanFailure(t *testing.T) {
	store := &spy{deleted: []int{10, 10, 10, 10}}
	s := sweeper(t, store, 30*24*time.Hour, 10)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	n, err := s.Sweep(ctx)
	if err != nil {
		t.Errorf("a cancelled sweep reported an error: %v", err)
	}
	if n != 0 {
		t.Errorf("deleted %d before noticing the cancellation", n)
	}
	if len(store.cutoffs) != 0 {
		t.Errorf("%d passes ran after cancellation", len(store.cutoffs))
	}
}

func TestASweepThatFailsReportsWhatItDeletedFirst(t *testing.T) {
	store := &spy{deleted: []int{10}, fail: nil}
	s := sweeper(t, store, 30*24*time.Hour, 10)
	// First batch succeeds, then the store starts failing.
	if _, err := s.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.fail = errors.New(errors.Unavailable, "the database went away")
	store.deleted = []int{10}
	n, err := s.Sweep(context.Background())
	if err == nil {
		t.Fatal("a failing store swept without error")
	}
	if n != 0 {
		t.Errorf("reported %d deleted on a failure that deleted nothing", n)
	}
}
