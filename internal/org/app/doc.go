// Package app is what org does, split by whether an operation writes.
//
// **The reasoning about the domain lives in internal/org.**
//
//	command/   changes something. Runs inside the caller's transaction, emits
//	query/     answers something. Takes neither
//
// See internal/identity/app for the argument; the split is uniform across
// domains so that a reader learns it once.
//
// **Light means light**: one database, the same tables, no projections and no
// read models.
package app
