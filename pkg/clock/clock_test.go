// AUTHOR-WRITTEN. Not produced behind an information barrier: written by someone
// who had read the implementation, and in most cases the surviving mutants that
// exposed the gap. Separate from spec_test.go so provenance is a property of the
// file rather than of a comment block somebody has to notice.
//
// custody/ records which run each of these came from.
package clock_test

import (
	"context"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/clock"
)

// ---------------------------------------------------------------------
// POST-HOC. Not written behind the information barrier — added after mutation
// testing by an author who had seen the implementation and the survivors.
// custody/0003 records the distinction.
//
// C6  System.Sleep ignoring cancellation DURING the wait survived. The
//     barrier-written test only cancels before the call, which the early
//     ctx.Err() guard catches on its own.
// C11 NewFake not normalising to UTC survived. The writer flagged it as
//     unstated and sidestepped it; the spec now says it.
//
// The third survivor needed no test: the doc promised an ordering for timers on
// the same deadline that no caller can observe, and the promise was removed.
// ---------------------------------------------------------------------

func TestSystemSleepReturnsWhenCancelledMidWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(5 * time.Millisecond); cancel() }()

	start := time.Now()
	err := clock.System{}.Sleep(ctx, time.Hour)
	took := time.Since(start)

	if err == nil {
		t.Errorf("Sleep cancelled mid-wait must return the context's error; a retry loop shut down mid-backoff would otherwise block for the full interval")
	}
	if took > time.Second {
		t.Errorf("Sleep returned after %v — cancellation must be prompt, not deferred to the deadline", took)
	}
}

func TestFakeSleepDoesNotAdvanceWhenTheContextIsDone(t *testing.T) {
	f := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	before := f.Elapsed()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := f.Sleep(ctx, time.Hour); err == nil {
		t.Errorf("Sleep on a done context must return its error rather than advancing")
	}
	if got := f.Elapsed(); got != before {
		t.Errorf("a cancelled sleep advanced the clock by %v — the fake must leave time where a real process that stopped waiting would leave it", got-before)
	}
}

func TestNewFakeNormalisesToUTC(t *testing.T) {
	loc := time.FixedZone("UTC+7", 7*60*60)
	at := time.Date(2026, 1, 1, 12, 0, 0, 0, loc)

	f := clock.NewFake(at)
	if got := f.Now().Location(); got != time.UTC {
		t.Errorf("NewFake(%v).Now() is in %v — Clock.Now is specified as UTC, and a stamped value must not carry a location a database will not", at, got)
	}
	if !f.Now().Equal(at) {
		t.Errorf("normalising to UTC must not change the instant: got %v, want the same instant as %v", f.Now(), at)
	}

	f.Set(time.Date(2026, 2, 1, 12, 0, 0, 0, loc))
	if got := f.Now().Location(); got != time.UTC {
		t.Errorf("Set must normalise too, got %v", got)
	}
}

// POST-HOC, 2026-09-06. Advance gained a return value after the barrier-written
// suite existed, so nothing exercised it — a mutation returning the zero time
// survived. New API added after a suite is written is untested by construction.
func TestAdvanceReturnsTheWallTimeItArrivedAt(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := clock.NewFake(start)

	got := f.Advance(90 * time.Minute)
	if want := start.Add(90 * time.Minute); !got.Equal(want) {
		t.Errorf("Advance returned %v, want %v — advancing and reading are one call so a caller need not follow every Advance with a Now()", got, want)
	}
	if !got.Equal(f.Now()) {
		t.Errorf("Advance returned %v but Now() reports %v; they must agree or the return value is a second source of truth", got, f.Now())
	}
}
