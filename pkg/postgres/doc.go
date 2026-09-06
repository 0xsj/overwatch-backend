// Package postgres is the substrate every real repository is built on: a pool
// with its timeouts already set, transactions carried on the context,
// PostgreSQL error codes translated into this codebase's error kinds, and a
// forward-only migration runner.
//
// # It is not a port, and that is the whole shape
//
// Everything else in the shared layer is a seam with more than one
// implementation. This is not one. It is what the *real* adapters are built on,
// and an in-memory adapter simply does not use it — it satisfies the same domain
// interface a different way, importing nothing from here.
//
// So there is no fake pool and no memory driver. decisions/0002 settles this:
// the swappable seam is the repository interface a domain declares, never the
// storage engine, because an interface that must span Postgres, a document store
// and a time-series store collapses to get-put-delete and every query that
// matters leaks through it.
//
// # The DSN is a parameter, and that is load-bearing for tests
//
// Nothing here reads the environment. [Open] takes a [Config] with a DSN in it,
// because a test needs to append its own search_path to that string to get an
// isolated schema. A package that read DATABASE_URL itself would make isolated
// tests impossible to write, and the property has to be designed in from the
// first line — it cannot be retrofitted once callers exist.
//
// The same reasoning puts the migration set in [Migrate]'s arguments rather than
// in a package-level registry: a test migrates a chosen set into a chosen schema.
//
// # Where the infrastructure-free suite ends
//
// Every package before this one is testable with no fixtures, no fakes and no
// docker. Transactions and migrations cannot be — they need a real server, and
// pretending otherwise tests a mock's opinion of Postgres.
//
// So the split is deliberate: the tests here **skip** when OVERWATCH_TEST_DSN is
// unset rather than hiding behind a build tag. They are compiled by every
// `make check`, which is what stops them rotting, and run by `make test-db`. A
// suite that needs docker to compile is a suite people stop running.
//
// # Errors are translated at this boundary and nowhere else
//
// [Translate] maps a PostgreSQL SQLSTATE onto an [errors.Kind]. Above this line
// nothing knows what 23505 means, and a domain that branches on a driver error
// is a domain welded to a driver.
//
// The mapping is grouped by **what a caller should do**, which is what Kind is
// for, and it is not always the same as what the code literally says:
//
//	23505 · 23P01   Conflict        the row is already there
//	23503 · 23502   Invalid         the request named something that is not
//	23514           Unprocessable   well-formed, and refused by a rule
//	22P02 · 22001   Invalid         the value will not fit the column
//	22003           Invalid
//	40001 · 40P01   Unavailable     serialization failure, deadlock — RETRYABLE
//	55P03 · 53300   Unavailable     lock unavailable, out of connections
//	08***           Unavailable     the connection went away
//	57014           Canceled/Timeout  see below
//	42*** · 22012   Internal        our SQL is wrong, not the caller's
//
// **23503 is genuinely ambiguous and is mapped anyway.** A foreign key violation
// on INSERT means the caller named a parent that does not exist — Invalid. The
// same SQLSTATE on DELETE means children still reference the row — Conflict.
// PostgreSQL does not distinguish them and neither can this function. Invalid
// wins because inserts referencing a parent vastly outnumber restricted deletes
// here, and a repository that performs a restricted delete should check for
// children explicitly rather than reading a status code out of this table.
//
// **57014 is two different events.** A statement_timeout firing is a Timeout,
// which is retryable. A caller cancelling is Canceled, which is not — retrying
// spends work on somebody who has already gone. They arrive with the same code,
// so [Translate] reads the context: done means the caller left.
//
// # Transactions are carried on the context, and nest by joining
//
// [Pool.InTx] puts the transaction in the context and hands it to the function.
// A nested [Pool.InTx] finds it and **joins** rather than opening a second one —
// two independent transactions against one logical unit of work is how half an
// operation gets committed.
//
// [DB] is what a repository calls to get something to run a query on: the
// transaction if one is in flight, the pool otherwise. So the same repository
// method works inside a transaction and outside one with no flag and no second
// method, and a caller composing two repositories in one transaction does not
// have to thread anything through them.
//
// Rollback is on any error and on any panic, and the panic is re-raised after
// the rollback. Swallowing it would turn a bug into a silent no-op commit.
//
// # Migrations are forward-only
//
// There is no down migration. A down migration is written when the schema is
// fresh in mind and run, if ever, in an incident — and the version that gets run
// is the one nobody tested. Rolling forward with a new migration is the same
// amount of work and is exercised by the same path as every other change.
//
// [Migrate] records each applied name in a table it creates itself, takes an
// advisory lock so two processes starting together do not race, and runs each
// migration in its own transaction. A migration that fails leaves everything
// before it applied and everything after it not — which is recoverable, unlike a
// half-applied one.
//
// # Deliberately absent
//
// A query builder. sqlc generates from SQL, so SQL is the source and the schema
// is not inferred from structs.
//
// Retry. Which failures are worth retrying is a policy owned by the caller, and
// [errors.Retryable] already answers it from the translated kind.
//
// Read replicas, sharding, a second pool for long queries. Each is a real answer
// to a problem nothing here has yet.
package postgres
