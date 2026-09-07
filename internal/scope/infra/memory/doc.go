// Package memory is scope's repository without a database — a first-class
// adapter, not a test double.
//
// **It is append-only here too** — decisions/0030. There is no Save that
// changes a pattern, a polarity, a gate or a kind, and a caller who wants one
// finds the same absence the SQL has.
//
// Supersede re-states the store's concurrency control: it refuses a rule that
// is already superseded, which is what the `superseded_at is null` predicate
// does in Postgres and what makes two callers retiring one rule produce one
// winner.
package memory
