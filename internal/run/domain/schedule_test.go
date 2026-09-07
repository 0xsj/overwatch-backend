// Author-written, from decisions/0038's Verification block. `Due` is pure, so
// the entire scheduling rule is asserted without a database, a clock or a
// process — which is the part of a scheduler that is normally only observable by
// waiting for it.
package domain_test

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/run/domain"
)

var tick = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

func pair(target, check byte, every time.Duration) domain.Pair {
	return domain.Pair{
		WorkspaceID: nonZero(200), TargetID: nonZero(target), CheckID: nonZero(check),
		CheckName: "a check", Interval: every, Enabled: true, HasChain: true,
	}
}

func names(due []domain.Pair) []domain.PairKey {
	out := make([]domain.PairKey, 0, len(due))
	for _, p := range due {
		out = append(out, p.Key())
	}
	return out
}

// "a pair that has never run is due" — and it is what makes a newly created
// check take effect against targets that already existed.
func TestAPairThatHasNeverRunIsDue(t *testing.T) {
	got := domain.Due([]domain.Pair{pair(1, 10, time.Hour)}, nil, tick, 0)
	if len(got) != 1 {
		t.Fatalf("want 1 due, got %d", len(got))
	}
}

func TestTheIntervalDecides(t *testing.T) {
	p := pair(1, 10, time.Hour)
	for _, tc := range []struct {
		name string
		ago  time.Duration
		want int
	}{
		{"just ran", time.Minute, 0},
		{"within the interval", 59 * time.Minute, 0},
		{"exactly the interval", time.Hour, 1},
		{"long past", 6 * time.Hour, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			last := map[domain.PairKey]time.Time{p.Key(): tick.Add(-tc.ago)}
			if got := len(domain.Due([]domain.Pair{p}, last, tick, 0)); got != tc.want {
				t.Fatalf("ran %v ago: want %d due, got %d", tc.ago, tc.want, got)
			}
		})
	}
}

// Three reasons a pair is never due, and each is a different fact.
func TestThreeReasonsAPairIsNeverDue(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    domain.Pair
	}{
		{"disabled — the standing authorisation is withdrawn", func() domain.Pair {
			p := pair(1, 10, time.Hour)
			p.Enabled = false
			return p
		}()},
		{"human — nothing spawns; a person reading it is the whole act", func() domain.Pair {
			p := pair(1, 10, time.Hour)
			p.Human = true
			return p
		}()},
		{"no interval — it runs when somebody asks", pair(1, 10, 0)},
		{"no chain — unfinished, and retrying it starves the queue", func() domain.Pair {
			p := pair(1, 10, time.Hour)
			p.HasChain = false
			return p
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Never run, so the ONLY thing that can exclude it is the rule.
			if got := domain.Due([]domain.Pair{tc.p}, nil, tick, 0); len(got) != 0 {
				t.Fatalf("want none due, got %d", len(got))
			}
			if tc.p.Runnable() {
				t.Fatal("Runnable disagrees with Due")
			}
		})
	}
}

// "the clock is PER PAIR: one target being fresh does not make another fresh"
//
// A firm running one check over sixty clients is sixty independent clocks, and a
// shared one would mean fifty-nine engagements are covered because the sixtieth
// was scanned.
func TestTheClockIsPerPairAndNotPerCheck(t *testing.T) {
	fresh := pair(1, 10, time.Hour)
	stale := pair(2, 10, time.Hour) // SAME check, different target
	last := map[domain.PairKey]time.Time{fresh.Key(): tick}

	got := domain.Due([]domain.Pair{fresh, stale}, last, tick, 0)
	if len(got) != 1 {
		t.Fatalf("want 1 due, got %d", len(got))
	}
	if got[0].TargetID != stale.TargetID {
		t.Fatal("the wrong target was scheduled — the clock is shared")
	}
}

// And the mirror: one CHECK being fresh does not make another fresh on the same
// target.
func TestTheClockIsPerPairAndNotPerTarget(t *testing.T) {
	fresh := pair(1, 10, time.Hour)
	stale := pair(1, 11, time.Hour) // same target, different check
	last := map[domain.PairKey]time.Time{fresh.Key(): tick}

	got := domain.Due([]domain.Pair{fresh, stale}, last, tick, 0)
	if len(got) != 1 || got[0].CheckID != stale.CheckID {
		t.Fatalf("want the other check due, got %v", names(got))
	}
}

// "the cap is honoured, and the OLDEST are taken first" — so a large estate
// makes progress round-robin rather than starving whatever sorts last.
func TestTheCapTakesTheOldestFirst(t *testing.T) {
	recent := pair(1, 10, time.Hour)
	older := pair(2, 10, time.Hour)
	oldest := pair(3, 10, time.Hour)
	last := map[domain.PairKey]time.Time{
		recent.Key(): tick.Add(-2 * time.Hour),
		older.Key():  tick.Add(-5 * time.Hour),
		oldest.Key(): tick.Add(-9 * time.Hour),
	}
	got := domain.Due([]domain.Pair{recent, older, oldest}, last, tick, 2)
	if len(got) != 2 {
		t.Fatalf("the cap was not honoured: %d", len(got))
	}
	if got[0].TargetID != oldest.TargetID || got[1].TargetID != older.TargetID {
		t.Fatalf("want oldest then older, got %v", names(got))
	}
}

// Never-run beats long-ago: `never` is the deficit 0011 calls the answerable
// question, and it is the one worth closing first.
func TestNeverRunSortsAheadOfLongAgo(t *testing.T) {
	never := pair(1, 10, time.Hour)
	ancient := pair(2, 10, time.Hour)
	last := map[domain.PairKey]time.Time{ancient.Key(): tick.Add(-1000 * time.Hour)}

	got := domain.Due([]domain.Pair{ancient, never}, last, tick, 1)
	if len(got) != 1 || got[0].TargetID != never.TargetID {
		t.Fatalf("a pair that has never run comes first: %v", names(got))
	}
}

// A fixed input produces a fixed plan. A scheduler whose output reshuffles is
// one nobody can reason about from a log.
func TestTheOrderIsStableForPairsThatHaveNeverRun(t *testing.T) {
	pairs := []domain.Pair{pair(1, 10, time.Hour), pair(2, 10, time.Hour), pair(3, 10, time.Hour)}
	first := names(domain.Due(pairs, nil, tick, 0))
	for i := 0; i < 20; i++ {
		again := names(domain.Due(pairs, nil, tick, 0))
		for n := range first {
			if again[n] != first[n] {
				t.Fatalf("the plan reshuffled on run %d", i)
			}
		}
	}
}

// The starvation the first live tick found: a pair that can never start sorts
// FIRST (never-run beats long-ago) and never updates its last-started time, so
// it holds a slot every tick forever and everything behind it waits.
func TestAnUnrunnablePairDoesNotStarveTheQueue(t *testing.T) {
	broken := pair(1, 10, time.Hour)
	broken.HasChain = false
	ready := pair(2, 11, time.Hour)
	last := map[domain.PairKey]time.Time{ready.Key(): tick.Add(-2 * time.Hour)}

	// A batch of one. The broken pair has never run, so it would sort first.
	got := domain.Due([]domain.Pair{broken, ready}, last, tick, 1)
	if len(got) != 1 || got[0].CheckID != ready.CheckID {
		t.Fatalf("the unrunnable pair took the only slot: %v", names(got))
	}
}
