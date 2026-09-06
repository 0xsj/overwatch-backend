// Package memory is workspace's repository without a database — a first-class
// adapter, not a test double.
//
// **The live-name uniqueness is here, case-folded**, because the Postgres index
// is on `lower(name)` and an adapter that accepts what the database refuses
// moves a failure from `make dev` to production.
//
// [Store.InTx] snapshots and restores; see internal/identity/infra/memory.
package memory
