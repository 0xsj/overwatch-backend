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
//	23505 · 23P01           Conflict        the row is already there
//	23503 · 23502           Invalid         the request named something that is not
//	23514                   Unprocessable   well-formed, and refused by a rule
//	22P02 · 22001 · 22003   Invalid         the value will not fit the column
//	22007 · 22008           Invalid         the date or time will not parse
//	40001 · 40P01 · 40**    Unavailable     serialization failure, deadlock — RETRYABLE
//	55P03 · 53300 · 53400   Unavailable     lock unavailable, out of connections
//	53**                    Unavailable     insufficient resources, generally
//	08**                    Unavailable     the connection went away
//	57P01 · 57P02 · 57P03   Unavailable     the server is shutting down or starting
//	57014                   Canceled/Timeout  see below
//	42501                   Forbidden       insufficient privilege — see below
//	everything else         Internal        42*** and 22012 arrive here
//
// The last row is a default, not a rule: 42*** (broken SQL) and 22012 (our
// arithmetic) are not matched by name, they simply fall through. Anything this
// table does not name is Internal, which is the fail-closed answer — an
// unrecognised failure must become a 500 rather than a code that invites a
// caller to retry or to edit their input.
//
// **23503 is genuinely ambiguous and is mapped anyway.** A foreign key violation
// on INSERT means the caller named a parent that does not exist — Invalid. The
// same SQLSTATE on DELETE means children still reference the row — Conflict.
// PostgreSQL does not distinguish them and neither can this function. Invalid
// wins because inserts referencing a parent vastly outnumber restricted deletes
// here, and a repository that performs a restricted delete should check for
// children explicitly rather than reading a status code out of this table.
//
// **42501 is carved out of the 42 class, and the carve-out is not settled.**
// This file used to claim the whole 42 class was Internal. It is not: the code
// maps 42501 (insufficient_privilege) to Forbidden, and that contradiction was
// found by a suite written from this document, which asserted the documented
// rule against two members of the class and failed on the second.
//
// The argument for Forbidden: it is an authorization failure, not broken SQL,
// and Internal additionally HIDES the message from the caller.
//
// The argument against, which is the stronger one today: Overwatch has one
// database role and no row-level security, so 42501 can only mean the
// application's own role is missing a grant. That is a deployment fault. A
// caller can do nothing about it, and rendering it as 403 tells them they are
// not allowed to do something they are in fact allowed to do. Internal — a 500,
// with the message hidden — is the honest answer while that is true.
//
// The behaviour is documented here as it stands rather than changed, because
// changing it is a decision about whether per-caller database roles are ever
// coming, and that belongs in decisions/ and not in a doc amendment made by a
// test run. If they are not coming, delete the case.
//
// **57014 is two different events, and the context is the SECOND line of
// defence, not the first.** A statement_timeout firing is a Timeout, which is
// retryable. A caller cancelling is Canceled, which is not — retrying spends
// work on somebody who has already gone.
//
// The obvious reading of that — "both arrive as 57014, so [Translate] reads the
// context to tell them apart" — is what this file used to say and it is wrong
// about the mechanism. Measured against a live server: a caller cancelling
// mid-query gets `context canceled` back from pgx with **no SQLSTATE at all**,
// and [Translate] catches it three branches earlier, on `errors.Is(err,
// context.Canceled)`. A statement_timeout firing under a live context is the
// only one of the two that actually reaches the 57014 case. So:
//
//	caller cancels          -> context.Canceled, no SQLSTATE  -> Canceled
//	client deadline expires -> context.DeadlineExceeded       -> Timeout
//	statement_timeout fires -> 57014, context still live      -> Timeout
//	57014 AND context done  -> the race between the two       -> Canceled
//
// The last row is the context check earning its keep: the driver can return the
// server's error before it notices the cancellation. It is a backstop for a
// race, not the primary discriminator.
//
// **An open question, recorded rather than settled.** That last row reads
// `ctx.Err() != nil`, which is also true for a client deadline that has expired
// — so a 57014 arriving under an expired deadline is classified Canceled (not
// retryable) when Timeout (retryable) is the better answer, since nobody
// cancelled and the caller has not gone. It is rare, because the
// DeadlineExceeded branch catches the common shape first. Splitting on
// `errors.Is(ctx.Err(), context.Canceled)` would fix it. Not changed here
// because it is a behavioural decision, not a typo.
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
