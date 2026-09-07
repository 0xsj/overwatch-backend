// Package memory is run's repository without a database — a first-class
// adapter, not a test double.
//
// **Two things the schema holds are restated here:**
//
//	invocation_exit_needs_a_process   an exit code only where a process ran
//	unique (invocation_id, stream)    one artifact per stream, idempotent
//
// The first is checked by the domain's own `Valid`, which the store calls before
// writing — so this adapter and Postgres refuse the same row for the same
// stated reason rather than one refusing it as a constraint violation.
//
// **Claim is NOT a real claim here.** `for update skip locked` has no meaning in
// a map, so this adapter hands back matching runs and says so. A test that needs
// two workers to contend needs the database.
package memory
