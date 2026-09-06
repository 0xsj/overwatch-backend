// Package clock makes time an injectable dependency.
//
// time.Now is a hidden global: code that calls it cannot be tested
// deterministically or replayed. Anything that stamps or compares time receives
// a [Clock] instead.
//
// Domain code stays purer still — it takes a time.Time argument and never holds
// a Clock. Only app and infra do. That is what keeps `domain/` importing nothing
// but the errors package.
//
// # Two kinds of time
//
// The port has two methods because there are two kinds of time, and conflating
// them is a bug that only shows up in production:
//
//	Now()      wall clock, UTC. Stamp with it. Compare persisted times with it.
//	Elapsed()  monotonic. Measure intervals with it.
//
// [Clock.Elapsed] returns a duration from an origin this package does not
// define, so a single reading means nothing and only a difference between two
// does. A fresh [Fake] may report zero or anything else; a test that asserts on
// one reading is asserting on an implementation detail.
//
// The wall clock is what a database column holds and what two processes can
// agree on, and it can jump — an NTP correction, a leap-second smear, a VM
// resuming from suspend. The monotonic clock never jumps and never goes
// backwards, but it is meaningless outside this process.
//
// So a session expiry, an outbox lease and a created_at are [Clock.Now]. A
// circuit breaker's rolling window, a token bucket's refill and a retry backoff
// are [Clock.Elapsed]. Getting it backwards gives you a window that never
// expires after the clock steps back, and it is not reproducible.
//
// # Why this is not just discipline
//
// Go hides the distinction rather than removing it. time.Now returns a value
// carrying both readings, and t.Sub(u) silently uses the monotonic one when both
// have it — but t.UTC() strips it. So the moment a timestamp is normalised for
// storage, every duration measured from it quietly becomes wall arithmetic, with
// no diff and no error.
//
// [System.Now] strips the monotonic reading up front, so a stamped value behaves
// exactly like one read back from a database or from JSON. The footgun is
// removed rather than documented, and [Clock.Elapsed] is the only way to measure
// an interval.
//
// # The two implementations
//
// [System] is the zero-sized production clock: System{} is usable, it holds
// nothing, and it is safe for concurrent use because it holds nothing.
//
// [NewFake] takes the wall time to start at and normalises it to UTC, so
// [Clock.Now] honours the port's contract whatever the caller passed and a
// stamped value never carries a location a database will not. [Fake.Set] does
// the same. A zero [Fake] is not usable —
// there is no sensible instant to default to, and one chosen here would appear
// in test failures as a date nobody wrote.
//
// # Testing
//
// [Fake] never advances on its own. The two ways to move it are not
// interchangeable, and the difference is the whole point:
//
//	Advance   time passed             — moves the wall clock AND the monotonic reading
//	Set       the clock was corrected — moves the wall clock ONLY
//
// [Fake.Advance] returns the wall time it arrived at, so advancing and reading
// are one call rather than two. Advancing by zero is legal and fires anything
// already due; it is only a NEGATIVE duration that panics.
//
// So Set is an NTP step, and anything measuring an interval must be unaffected
// by it. That is a test you can write rather than a rule you have to remember.
//
// Fake is safe for concurrent use. Advance panics on a negative duration —
// monotonic time does not go backwards, and letting it would make the fake model
// something that cannot happen. Use Set to move the wall clock back.
//
// # Sleeping
//
// Not on the port. Waiting needs a context to be cancellable, and a clock that
// takes a context is no longer a clock. [System] and [Fake] both offer Sleep and
// After, so a module that waits declares its own narrow interface — interfaces
// belong to the consumer — and gets a ready implementation without this package
// dictating its shape.
//
// [Fake.Sleep] advances the clock rather than waiting. "Time passed" is the
// faithful reading of sleeping, and it is what makes a suite using it both
// instant and honest: a retry that waits 100ms leaves the clock 100ms further
// on, so anything measuring an interval afterwards sees production's numbers.
//
// Sleeping is advancing, so it fires every timer the interval crossed, exactly
// as [Fake.Advance] does. Sleeping past a deadline does not skip it.
//
// When one advance crosses several deadlines they fire in deadline order,
// earliest first. Advancing exactly onto a deadline fires it — the boundary is
// inclusive, matching time.Timer, so "sleeping past a deadline does not skip it"
// holds at the deadline itself and not only beyond it.
//
// Two timers on the SAME deadline have no defined order, and that is a
// correction rather than an omission. An earlier draft promised creation order;
// nothing can observe it. A caller holding two fired channels reads them with
// select, which chooses at random among ready cases, so the guarantee
// constrained the implementation and could never reach anybody. A promise
// nothing can observe does not belong in a contract — it reads as a property
// under test and is not one. Found 2026-09-06: a mutation deleting the
// tie-break survived the whole suite, and the test writer had already reported
// the claim as untestable from the exported surface.
//
// [Fake.After] of zero is already due and fires on the next advance of any size,
// including zero. It fires on Advance and never on Set, because a timer deadline is
// monotonic. A clock correction can neither bring a timer forward nor hold it
// back. A fired channel carries the wall time at the moment it fired, and it
// fires once — a timer is not a ticker.
//
// # Sleeping and cancellation
//
// Sleep returns the context's error and does not sleep when the context is
// already done. Cancelled DURING the wait it returns the context's error too,
// promptly, having advanced nothing — for [Fake] that means the clock does not
// move, so a cancelled backoff leaves the fake where a real one would leave a
// process that stopped waiting. A completed sleep returns nil.
//
// # Since and Until
//
// Free functions rather than methods on the port. Every implementation would
// compute them identically, and a default nobody varies is not part of a
// contract — putting them on the interface would oblige any future clock to
// reimplement subtraction.
//
// Both are WALL arithmetic, deliberately. They are the right tool for "how long
// ago was this persisted timestamp" — a session's expiry, a row's created_at —
// where the other side of the comparison came out of storage and only the wall
// clock means anything. They are the wrong tool for an interval inside one
// process; use [Clock.Elapsed] there.
//
// # Observed time is not event time
//
// This package only ever answers when something was *observed*. An incident
// occurred at 14:32 and was ingested at 14:35; the first is a domain value that
// arrives from the feed, the second is [Clock.Now].
//
// Conflating them makes a late-arriving incident look like a late-happening one,
// which for a dispatch product is a correctness bug rather than a cosmetic one.
// No clock can tell them apart — the distinction has to live in the model.
//
// # Deliberately absent
//
// A Ticker. Collectors polling feeds on an interval will want one, and the
// waiter machinery behind [Fake.After] is its foundation — but its shape depends
// on whether a missed tick coalesces or queues, which the first collector
// answers and guessing does not.
//
// Truncation to microsecond precision. Postgres timestamptz holds microseconds
// and Go holds nanoseconds, so a stamped value will not compare equal to one
// read back. Storage is undecided, and this layer does not get to presume
// Postgres; the fix belongs at the repository boundary.
package clock
