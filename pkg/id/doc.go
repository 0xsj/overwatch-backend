// Package id is the identifier every aggregate in the system carries.
//
// An [ID] is a UUIDv7: sixteen bytes, of which the first six are a Unix
// millisecond timestamp, so identifiers sort chronologically as raw bytes and as
// canonical strings. Version 4 is random and sorts arbitrarily, which turns every
// primary-key insert into a write to a random point in the index; version 7 keeps
// inserts at the right-hand edge of the B-tree and makes "the most recent
// incidents" a range scan.
//
// The timestamp is not a substitute for a domain field. See "Observed time" below.
//
// # The type
//
// [ID] is an array, not a slice — comparable with ==, usable as a map key, and
// copied by value with no allocation. [Nil] is the zero value and reads as
// version 0, so an identifier nobody set never passes for a real one.
//
// [ID.IsZero] asks that question directly rather than making every caller
// compare against [Nil], and [ID.Version] reads the four version bits. Version
// is exposed because "which UUID version is this" is a legitimate question with
// one obvious answer, and leaving it to callers means each one reinvents a bit
// shift on a byte index.
//
// [ID.String] emits the canonical lowercase 8-4-4-4-12 form, and [ID] implements
// encoding.TextMarshaler and TextUnmarshaler, so JSON and any text-based format
// carry that form with no adapter.
//
// # Generating
//
// [Generator] has one method, [Generator.NewID], returning an [ID] and nothing
// else. Two implementations, and the split is deliberate:
//
//	V7        production. A clock, an entropy source, a 12-bit sequence counter.
//	Sequence  tests. Deterministic, no entropy, no clock.
//
// [V7] takes its clock as a ONE-method interface declared here — Now, returning
// a wall time — rather than importing a clock package. Minting needs the wall
// clock and nothing else: the monotonic reading cannot go in a UUID, which
// carries a Unix millisecond that another process has to be able to read.
//
// Declaring it here rather than importing keeps this package's dependencies to
// io, sync and time, and it is the ordinary Go rule that an interface belongs to
// the consumer. It also means pkg/clock's [Clock] satisfies it structurally
// without either package knowing about the other — which nothing checks, so the
// composition root asserts it once and turns a rename into a compile error.
//
// # Why not a UUID library
//
// google/uuid has NewV7 and mints from an internal clock that cannot be
// injected. The two behaviours worth testing here — what [ID.Time] reports, and
// that a backwards clock step does not reissue a millisecond already used — are
// unreachable from a test against it. Sixteen bytes and a counter is not the
// part worth outsourcing. STACK.md carries this as a refusal so it is not
// proposed again.
//
// # Monotonicity
//
// Identifiers minted in the same millisecond must still sort in issue order, so
// [V7] carries a 12-bit counter in the bits RFC 9562 calls rand_a. Four thousand
// and ninety-six identifiers fit in one millisecond; the next one borrows from the
// millisecond after rather than wrapping or repeating.
//
// The same counter is what survives a clock correction. If the wall clock steps
// backwards, [V7] holds its high-water mark and keeps counting rather than
// reissuing a millisecond it has already used. Identifiers stay ordered; a handful
// carry a timestamp slightly ahead of the wall clock. That trade is deliberate —
// ordering is a correctness property and the timestamp is an approximation.
//
// # Entropy
//
// [V7] reads its entropy source WITHOUT holding its lock, so the source must
// itself be safe for concurrent use. crypto/rand.Reader is. A seeded
// *math/rand.Rand is not, and racing on one is the obvious mistake to make when
// reaching for deterministic identifiers in a test.
//
// So: pass crypto/rand.Reader in production, and use [Sequence] in tests. That
// keeps the requirement structural rather than remembered — no test has a reason
// to hand [V7] anything else.
//
// [NewID] has no error return, so an entropy source that fails panics. On any
// system this runs on, crypto/rand does not fail.
//
// # Sequence
//
// [Sequence] issues identifiers from a fixed instant with a plain counter: no
// entropy, no clock, same inputs give the same identifiers on every run. They are
// well-formed version 7 with the correct variant, so anything downstream that
// validates or parses them behaves exactly as it would in production, and they
// sort in issue order so fixtures read in the order they were written.
//
// It is safe for concurrent use.
//
// # Parsing
//
// [ID.UnmarshalText] leaves the receiver untouched when the input does not
// parse. A partially decoded identifier is worse than none, because it is
// well-formed enough to be stored.
//
// [NewV7] refuses a nil clock or a nil entropy source. Both are caller errors
// no environment can produce, so they fail at construction rather than at the
// first identifier — see [[caller-error-versus-operator-error]].
//
// [Parse] accepts the canonical 36-character form in either case and returns an
// Invalid error naming the input, which is the caller's own and therefore safe to
// echo.
//
// It REFUSES [Nil]. A well-formed all-zero UUID is exactly what an unset field
// serialises to, and accepting it here is how "nobody set this" arrives at a
// repository wearing a real identifier's clothes. The zero value fails closed
// inside the process; the parser is where it has to fail closed coming in. It normalises: [Parse] followed by [ID.String] returns lowercase whatever
// the input case was, so identifiers are compared as [ID] values and never as
// strings.
//
// It parses; it does not judge. A well-formed version 4 UUID parses fine — only
// [ID.Time] cares about the version, and it reports false for anything but 7.
//
// # Observed time
//
// [ID.Time] answers when an identifier was minted, which is when the system
// recorded something. It is not when the thing happened.
//
// An incident occurring at 14:32 and ingested at 14:35 has an identifier stamped
// 14:35. Sorting incidents by identifier sorts them by arrival, which is the right
// order for a feed and the wrong one for a timeline. Event time is a domain field
// that comes from the source.
//
// # Deliberately absent
//
// A database driver. Postgres stores uuid as sixteen bytes and [ID] already is
// sixteen bytes, but sql/driver.Valuer and sql.Scanner — or pgx's encoder — belong
// with the repositories that need them, and storage is not yet decided.
//
// A MustParse. Identifiers in tests come from [Sequence], which cannot fail.
package id
