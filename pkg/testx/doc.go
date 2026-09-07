// Package testx is the database harness every suite in this repository was
// writing for itself.
//
// Eleven test files opened a pool, skipped on a missing DSN, dropped schemas,
// migrated, and registered cleanup — the same forty lines, copied. That is the
// evidence for this package; there is no argument beyond it.
//
// # It imports no domain, and that is what makes it legal
//
// U3 forbids shared code from knowing a feature exists. So [Schema] takes a
// migration set and a schema NAME as parameters and never a domain:
//
//	db := testx.Postgres(t, testx.Schema{Name: "identity", Migrations: identitypg.Migrations})
//
// pkg/postgres was built for this — decisions/0002 put the DSN and the migration
// set in the signatures precisely so a test could hand it a chosen set and a
// chosen schema. This is the caller that property was designed for, arriving
// after eleven hand-rolled ones.
//
// # A skip is a result and says how to complete it
//
// Without OVERWATCH_TEST_DSN the suite SKIPS with the command that would make it
// run. It does not fail, because a clone with no database must still pass
// `go test ./...`; and it does not silently return, because a check that agrees
// by omission is worse than no check.
//
// The tests are compiled either way, which is what stops them rotting. A build
// tag would hide them until somebody remembers the tag, and by then they no
// longer compile.
//
// # Every schema is dropped and re-migrated, deliberately
//
// Not truncated. A migration that changed since the last run must be applied,
// and a test that runs against last week's schema passes for a reason nobody can
// see. Dropping is slower and it is the only version that is honest.
//
// **The ledger goes with the table.** A forward-only migrator records what it
// applied and will not re-apply it, so dropping a schema without its
// `schema_migrations` leaves the migrator believing in a table that is not
// there — which cost an afternoon before this package existed.
//
// # Deliberately absent
//
// **Per-test isolation by search_path.** It works only while every migration is
// schema-unqualified, and this repository's are qualified on purpose — that is
// what lets domains share a pool. So suites serialise on a shared database
// rather than pretending to be parallel.
//
// **Fixtures.** A helper that inserts rows has to know what a row means, which
// is knowing a domain. Each suite builds its own through its own constructors,
// which is also the only way the invariants get exercised.
package testx
