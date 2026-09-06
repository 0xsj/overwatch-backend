// Package memory is identity's repository without a database.
//
// It is a **first-class adapter, not a test double**. decisions/0002 says the
// swappable seam is the repository interface a domain declares, and this is the
// second implementation of it — chosen in one place at boot, so running the
// whole application with no infrastructure is a supported mode.
//
// # InTx snapshots and restores, and that is what makes the mode honest
//
// The obvious memory transaction is to call the function and hope. That adapter
// passes every test until one asks what happens when the function fails halfway,
// at which point memory mode and Postgres mode disagree about the only property
// the transaction exists for.
//
// So [Store.InTx] copies the maps on entry and puts them back on error or panic,
// and a nested call joins rather than snapshotting again — the same rule
// pkg/postgres follows, for the same reason: two independent transactions over
// one unit of work is how half an operation commits.
//
// It is a coarse lock over the whole store rather than per-key. That is correct
// for the size this runs at and it is the honest simple thing; a memory adapter
// competing with Postgres on concurrency has lost sight of what it is for.
//
// # It enforces the constraints the migration enforces
//
// A memory adapter that accepts what the database refuses is worse than none,
// because it moves a failure from `make dev` to production. So the live-email
// index and the one-live-password rule are implemented here as scans, and they
// return the same domain sentinels the Postgres adapter returns.
//
// **The scans are O(n) and that is deliberate.** An index here would be a second
// thing to keep consistent with the maps, and this adapter exists to be obviously
// correct rather than fast.
package memory
