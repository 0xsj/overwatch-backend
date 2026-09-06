// Package domain holds identity's types and their rules, and imports nothing
// that knows about storage, transport or time-of-day.
//
// **The reasoning lives one level up, in internal/identity.** That file is the
// contract a spec-test suite is written from, and splitting the argument across
// two documents is how half of it stops being read. What is here is the part
// that only makes sense beside the code.
//
// Every type is a value and every transition returns a new one. There is no
// method that mutates a receiver, which means a caller cannot half-apply a
// change and a test can hold the before and the after at once.
//
// **Every predicate takes the instant it is evaluated at.** Nothing here reads
// the clock. A rule that calls time.Now cannot be tested at its boundary, and a
// boundary is the only interesting case an expiry has.
//
// **The constructors validate; they do not seal.** Go has no way to stop
// `domain.Account{}` being written by hand, and `Email("NOT@Folded")` is a
// conversion the language permits. So the constructor is where the rule is
// stated once, and the migration's CHECK constraints are what still holds when
// something bypasses it — including a psql prompt, which no amount of care in
// this package reaches.
package domain
