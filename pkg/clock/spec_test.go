// Tests for pkg/clock, written against go doc -all output only. No
// implementation was read. Where the doc is silent or ambiguous the
// inference is marked inline at the assertion that depends on it.
package clock_test

import (
	"context"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/0xsj/overwatch-backend/pkg/clock"
)

// Both concrete clocks must satisfy the port. This is a compile-time
// contract check: if either type stops implementing Clock, the test
// package fails to build rather than reporting a runtime failure.
var (
	_ clock.Clock = clock.System{}
	_ clock.Clock = (*clock.Fake)(nil)
)

var baseTime = time.Date(2024, time.March, 3, 9, 30, 0, 0, time.UTC)

// fired reports whether a timer channel already has a value ready,
// without blocking.
func fired(ch <-chan time.Time) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------
// Fake: construction
// ---------------------------------------------------------------------

func TestFakeConstructsExactlyAtTheGivenInstant(t *testing.T) {
	f := clock.NewFake(baseTime)
	if !f.Now().Equal(baseTime) {
		t.Errorf("NewFake(%v).Now() = %v; a fake must start reporting exactly the instant it was told to start at, or every test built on it starts from a lie", baseTime, f.Now())
	}
	if now := f.Now(); now.Round(0) != now {
		t.Errorf("Fake.Now() carries a monotonic reading; a fake built from ordinary time.Time arithmetic must never introduce one, or code that strips it (t.UTC(), a storage round-trip) would behave differently against the fake than against System")
	}
}

// ---------------------------------------------------------------------
// Fake: Advance moves BOTH readings, by exactly the requested amount
// ---------------------------------------------------------------------

func TestFakeAdvanceMovesWallAndElapsedByExactlyTheRequestedAmount(t *testing.T) {
	cases := []struct {
		name string
		d    time.Duration
	}{
		{"advancing by zero moves neither reading", 0},
		{"advancing by a single nanosecond", 1 * time.Nanosecond},
		{"advancing by a sub-second amount", 500 * time.Millisecond},
		{"advancing by a full day", 24 * time.Hour},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			f := clock.NewFake(baseTime)
			wallBefore := f.Now()
			elapsedBefore := f.Elapsed()

			f.Advance(tc.d)

			if wallDiff := f.Now().Sub(wallBefore); wallDiff != tc.d {
				t.Errorf("Advance(%v) moved the wall clock by %v; Advance means time passed, so the wall reading must move by exactly the requested amount, not an approximation of it", tc.d, wallDiff)
			}
			if elapsedDiff := f.Elapsed() - elapsedBefore; elapsedDiff != tc.d {
				t.Errorf("Advance(%v) moved Elapsed() by %v; Advance moves the wall clock AND the monotonic reading together by the same amount, because that is exactly what distinguishes it from Set", tc.d, elapsedDiff)
			}
		})
	}
}

// ---------------------------------------------------------------------
// Fake: Set moves wall ONLY — the central Advance/Set distinction
// ---------------------------------------------------------------------

func TestFakeSetMovesWallClockOnlyLeavingElapsedUntouched(t *testing.T) {
	cases := []struct {
		name      string
		correctTo time.Time
	}{
		{"an NTP correction forward leaves the monotonic reading untouched", baseTime.Add(6 * time.Hour)},
		{"an NTP correction backward leaves the monotonic reading untouched", baseTime.Add(-6 * time.Hour)},
		{"correcting to the same instant leaves the monotonic reading untouched", baseTime},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			f := clock.NewFake(baseTime)
			before := f.Elapsed()

			f.Set(tc.correctTo)

			if after := f.Elapsed(); after != before {
				t.Errorf("Elapsed() changed from %v to %v across a Set call; Set models a wall-clock correction, and a correction to wall time must never affect a duration meant to survive that correction — conflating the two is exactly the bug this package exists to prevent", before, after)
			}
			if !f.Now().Equal(tc.correctTo) {
				t.Errorf("Now() = %v after Set(%v); Set must land the wall clock exactly on the corrected instant", f.Now(), tc.correctTo)
			}
		})
	}
}

func TestFakeSetMovesWallClockBackwardWithoutPanicking(t *testing.T) {
	f := clock.NewFake(baseTime)
	f.Advance(1 * time.Hour)
	earlier := baseTime

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Set(%v) panicked with %v; the doc names Set as the way to move the wall clock back after a correction, so a backward correction through Set must be accepted", earlier, r)
			}
		}()
		f.Set(earlier)
	}()

	if !f.Now().Equal(earlier) {
		t.Errorf("after Set(%v), Now() = %v; Set must land the wall clock exactly on the corrected instant, forward or backward", earlier, f.Now())
	}
}

// ---------------------------------------------------------------------
// Fake: order-independence of sequential advances (commutativity)
// ---------------------------------------------------------------------

func TestFakeSequentialAdvancesCommute(t *testing.T) {
	d1 := 3 * time.Hour
	d2 := 47 * time.Minute

	a := clock.NewFake(baseTime)
	aBaseline := a.Elapsed()
	a.Advance(d1)
	a.Advance(d2)

	b := clock.NewFake(baseTime)
	bBaseline := b.Elapsed()
	b.Advance(d2)
	b.Advance(d1)

	if !a.Now().Equal(b.Now()) {
		t.Errorf("advancing by (%v then %v) produced wall clock %v, but advancing by (%v then %v) produced %v; two intervals applied in either order must land on the same instant, because time passing does not care which increment is counted first", d1, d2, a.Now(), d2, d1, b.Now())
	}

	aDelta := a.Elapsed() - aBaseline
	bDelta := b.Elapsed() - bBaseline
	if aDelta != bDelta {
		t.Errorf("elapsed displacement was %v for order (%v,%v) and %v for order (%v,%v); the total interval measured must not depend on the order the advances were applied", aDelta, d1, d2, bDelta, d2, d1)
	}
	if want := d1 + d2; aDelta != want {
		t.Errorf("elapsed displacement after two advances was %v, want exactly %v; Advance must account for the full requested duration", aDelta, want)
	}
}

// ---------------------------------------------------------------------
// Fake: negative Advance is refused, not clamped or silently accepted
// ---------------------------------------------------------------------

func TestFakeAdvancePanicsOnNegativeDuration(t *testing.T) {
	cases := []struct {
		name string
		d    time.Duration
	}{
		{"a single negative nanosecond", -1 * time.Nanosecond},
		{"a negative duration of ordinary size", -500 * time.Millisecond},
		{"a large negative duration", -24 * time.Hour},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			f := clock.NewFake(baseTime)
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("Advance(%v) did not panic; monotonic time never goes backwards, and a fake that accepted a negative advance would model something that cannot happen in the real Clock it stands in for", tc.d)
				}
			}()
			f.Advance(tc.d)
		})
	}
}

// ---------------------------------------------------------------------
// Fake: timer firing on Advance, boundary and multi-deadline behavior
// ---------------------------------------------------------------------

func TestFakeAfterFiresExactlyWhenTheDeadlineIsCrossed(t *testing.T) {
	cases := []struct {
		name      string
		deadline  time.Duration
		advanceBy time.Duration
		wantFired bool
	}{
		{"advancing less than the deadline leaves the timer unfired", 10 * time.Millisecond, 5 * time.Millisecond, false},
		// Inference: the doc says sleeping past a deadline does not skip it,
		// but does not say whether landing exactly ON the deadline counts as
		// crossing it. This assumes the boundary is inclusive, matching
		// time.Timer/time.After semantics.
		{"advancing exactly to the deadline fires the timer (inference: inclusive boundary not stated in the doc)", 10 * time.Millisecond, 10 * time.Millisecond, true},
		{"advancing well past the deadline fires the timer", 10 * time.Millisecond, 50 * time.Millisecond, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			f := clock.NewFake(baseTime)
			ch := f.After(tc.deadline)
			f.Advance(tc.advanceBy)
			if got := fired(ch); got != tc.wantFired {
				t.Errorf("After(%v) fired=%v after Advance(%v), want %v; a not-yet-reached deadline must not fire early, and a crossed one must not be skipped", tc.deadline, got, tc.advanceBy, tc.wantFired)
			}
		})
	}
}

func TestFakeAfterNeverFiresOnSet(t *testing.T) {
	cases := []struct {
		name      string
		correctTo time.Time
	}{
		{"a forward wall-clock correction does not fire a pending timer", baseTime.Add(1 * time.Hour)},
		{"a backward wall-clock correction does not fire a pending timer", baseTime.Add(-1 * time.Hour)},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			f := clock.NewFake(baseTime)
			ch := f.After(1 * time.Minute)
			f.Set(tc.correctTo)
			if fired(ch) {
				t.Errorf("a timer fired after Set(%v); a timer's deadline is monotonic, so a wall-clock correction — forward or backward — must neither trigger it nor postpone it", tc.correctTo)
			}
		})
	}
}

func TestFakeAfterFiresOnlyOnceNotRepeatedly(t *testing.T) {
	// Inference: the doc lists a Ticker as deliberately absent, implying
	// After behaves like a one-shot time.After rather than something that
	// refires. This exact contract is not spelled out.
	f := clock.NewFake(baseTime)
	ch := f.After(2 * time.Millisecond)

	f.Advance(10 * time.Millisecond)
	if !fired(ch) {
		t.Fatalf("a timer with a 2ms deadline did not fire after advancing 10ms")
	}

	f.Advance(10 * time.Millisecond)
	if fired(ch) {
		t.Errorf("a one-shot timer fired a second time on a later Advance (inference: After is not a Ticker, per the doc's explicit contrast, so it should not refire)")
	}
}

func TestFakeAdvanceFiresEveryDeadlineItCrosses(t *testing.T) {
	f := clock.NewFake(baseTime)
	timers := map[string]<-chan time.Time{
		"early (1ms)": f.After(1 * time.Millisecond),
		"mid (5ms)":   f.After(5 * time.Millisecond),
		"late (9ms)":  f.After(9 * time.Millisecond),
	}

	f.Advance(10 * time.Millisecond)

	for name, ch := range timers {
		if !fired(ch) {
			t.Errorf("the %s timer did not fire after a single Advance that crossed its deadline; one Advance must fire every deadline it crosses, not just the nearest or the last", name)
		}
	}
}

// ---------------------------------------------------------------------
// Fake: Sleep advances rather than waits
// ---------------------------------------------------------------------

func TestFakeSleepAdvancesTheClockRatherThanWaiting(t *testing.T) {
	f := clock.NewFake(baseTime)
	ch := f.After(3 * time.Millisecond)
	wallBefore := f.Now()
	elapsedBefore := f.Elapsed()

	realStart := time.Now()
	err := f.Sleep(context.Background(), 10*time.Millisecond)
	realElapsed := time.Since(realStart)

	if err != nil {
		t.Fatalf("Sleep returned error %v for an uncancelled context; a fake sleep with nothing to cancel must succeed", err)
	}
	if wallDiff := f.Now().Sub(wallBefore); wallDiff != 10*time.Millisecond {
		t.Errorf("Sleep(10ms) moved the wall clock by %v; Fake.Sleep advances the clock by the requested duration rather than waiting for it", wallDiff)
	}
	if elapsedDiff := f.Elapsed() - elapsedBefore; elapsedDiff != 10*time.Millisecond {
		t.Errorf("Sleep(10ms) moved Elapsed() by %v; a retry that waits 10ms must leave the clock 10ms further on so a later interval measurement sees production's numbers", elapsedDiff)
	}
	if !fired(ch) {
		t.Errorf("a timer due before the sleep's duration did not fire; sleeping is advancing, so it must fire every timer the interval crossed, exactly as Advance does")
	}
	if realElapsed > 100*time.Millisecond {
		t.Errorf("Fake.Sleep took %v of real wall-clock time to return; a fake must not actually wait — that is the entire point of using one in a test suite", realElapsed)
	}
}

// ---------------------------------------------------------------------
// Since / Until: wall arithmetic, not Elapsed
// ---------------------------------------------------------------------

func TestSinceAndUntil(t *testing.T) {
	f := clock.NewFake(baseTime)
	past := baseTime.Add(-2 * time.Hour)
	future := baseTime.Add(3 * time.Hour)

	t.Run("Since equals now minus t, the only computation every implementation would agree on", func(t *testing.T) {
		got := clock.Since(f, past)
		want := f.Now().Sub(past)
		if got != want {
			t.Errorf("Since(c, t) = %v, want c.Now().Sub(t) = %v; the doc states every implementation would compute Since identically, so it must match that formula exactly", got, want)
		}
	})

	t.Run("Until equals t minus now", func(t *testing.T) {
		got := clock.Until(f, future)
		want := future.Sub(f.Now())
		if got != want {
			t.Errorf("Until(c, t) = %v, want t.Sub(c.Now()) = %v", got, want)
		}
	})

	t.Run("Since and Until are additive inverses for the same instant", func(t *testing.T) {
		for _, tm := range []time.Time{past, future, baseTime} {
			s := clock.Since(f, tm)
			u := clock.Until(f, tm)
			if s != -u {
				t.Errorf("Since(c, %v) = %v and Until(c, %v) = %v are not additive inverses; both describe the same gap measured from opposite ends and must sum to zero", tm, s, tm, u)
			}
		}
	})

	t.Run("Since tracks a wall-clock correction made via Set, proving it is wall arithmetic and not an Elapsed-based interval", func(t *testing.T) {
		f2 := clock.NewFake(baseTime)
		before := clock.Since(f2, past)
		f2.Set(baseTime.Add(10 * time.Hour))
		after := clock.Since(f2, past)
		if diff := after - before; diff != 10*time.Hour {
			t.Errorf("Since(c, t) moved by %v after correcting the wall clock forward by 10h, want exactly 10h; Since answers 'how long ago was this persisted timestamp' using the wall clock, which is precisely what Set corrects — an Elapsed-based reading would not have moved at all", diff)
		}
	})
}

// ---------------------------------------------------------------------
// System: properties that hold regardless of wall-clock value
// ---------------------------------------------------------------------

func TestSystemNowIsAlwaysUTC(t *testing.T) {
	now := clock.System{}.Now()
	if now.Location() != time.UTC {
		t.Errorf("System.Now() returned location %v, want UTC; the doc states Now() is the wall clock, UTC — a database column and a second process can only agree on it if every caller stamps in the same zone", now.Location())
	}
}

func TestSystemNowCarriesNoMonotonicReading(t *testing.T) {
	now := clock.System{}.Now()
	if now.Round(0) != now {
		t.Errorf("System.Now() carries a monotonic reading (detected via t.Round(0) != t); the doc says System strips it up front so a stamped value behaves exactly like one read back from a database or from JSON")
	}
}

func TestSystemElapsedAdvancesAcrossRealTime(t *testing.T) {
	var s clock.System
	first := s.Elapsed()
	time.Sleep(5 * time.Millisecond)
	second := s.Elapsed()
	if second <= first {
		t.Errorf("Elapsed() went from %v to %v across 5ms of real sleep; the monotonic clock must never go backwards and must reflect that real time passed", first, second)
	}
}

func TestSystemAfterEventuallyFires(t *testing.T) {
	var s clock.System
	ch := s.After(10 * time.Millisecond)
	select {
	case <-ch:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("System.After(10ms) did not fire within 500ms of real time; After must eventually deliver, or nothing built on it (retry backoff, lease timeouts) can ever wake up")
	}
}

func TestSystemSleepWaitsAtLeastTheRequestedDuration(t *testing.T) {
	var s clock.System
	const want = 10 * time.Millisecond
	start := time.Now()
	err := s.Sleep(context.Background(), want)
	got := time.Since(start)
	if err != nil {
		t.Fatalf("Sleep(%v) with an uncancelled context returned error %v, want nil", want, err)
	}
	if got < want-2*time.Millisecond {
		t.Errorf("Sleep(%v) returned after only %v of real time; a sleep must wait at least the requested duration", want, got)
	}
}

func TestSystemSleepRespectsCancellation(t *testing.T) {
	// Inference: the doc's stated reason Sleep takes a context at all is
	// "waiting needs a context to be cancellable." It does not spell out
	// the exact contract (which error value, how promptly). This test
	// only asserts what that stated reason implies: cancellation must
	// have an observable effect — a non-nil error and a prompt return —
	// not that Sleep ignores the context and always completes the wait.
	var s clock.System
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := s.Sleep(ctx, 2*time.Second)
	got := time.Since(start)

	if err == nil {
		t.Errorf("Sleep returned a nil error for an already-canceled context; a wait that ignores cancellation gives no reason to take a context in the first place (inference: exact error unspecified)")
	}
	if got > 200*time.Millisecond {
		t.Errorf("Sleep with an already-canceled context took %v to return, close to the full 2s requested; a cancellable wait must return promptly once canceled (inference)", got)
	}
}

func TestSystemIsZeroSized(t *testing.T) {
	if size := unsafe.Sizeof(clock.System{}); size != 0 {
		t.Errorf("unsafe.Sizeof(System{}) = %d, want 0; the doc calls System 'the zero-sized production clock' and grounds its concurrency safety in holding nothing", size)
	}
}

// ---------------------------------------------------------------------
// Fake: concurrency (best-effort; strongest under -race)
// ---------------------------------------------------------------------

func TestFakeToleratesConcurrentReadsWhileAdvancing(t *testing.T) {
	f := clock.NewFake(baseTime)
	const readers = 8
	var wg sync.WaitGroup
	stop := make(chan struct{})
	fails := make(chan string, readers)

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var prev time.Duration
			seen := false
			for {
				select {
				case <-stop:
					return
				default:
				}
				e := f.Elapsed()
				_ = f.Now()
				if seen && e < prev {
					fails <- "Elapsed() was observed to move backward under concurrent Advance calls from another goroutine; a monotonic reading must never appear to go backwards to any caller"
					return
				}
				prev = e
				seen = true
			}
		}()
	}

	for i := 0; i < 2000; i++ {
		f.Advance(time.Microsecond)
	}
	close(stop)
	wg.Wait()
	close(fails)
	for msg := range fails {
		t.Error(msg)
	}
}
