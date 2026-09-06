// Package memory is org's repository without a database — a first-class adapter
// chosen at boot, not a test double. decisions/0002: the swappable seam is the
// repository interface a domain declares, and this is the second implementation.
//
// **It enforces what the migration enforces**, or it moves a failure from
// `make dev` to production: the live-membership index and the last-owner rule
// are here as scans returning the same domain sentinels. The scans are O(n)
// deliberately — an index would be a second thing to keep consistent with the
// map, and this adapter exists to be obviously correct rather than fast.
//
// [Store.InTx] snapshots and restores on error or panic, and a nested call joins
// rather than snapshotting again — see internal/identity/infra/memory for why a
// memory transaction that merely calls the function makes the two modes disagree
// about the only property a transaction has.
package memory
