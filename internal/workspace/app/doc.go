// Package app is what workspace does, split by whether an operation writes.
//
// **The reasoning about the domain lives in internal/workspace.**
//
//	command/   changes something. Runs inside the caller's transaction, emits
//	query/     answers something. Takes neither
//
// See internal/identity/app for the argument.
//
// **Light means light**: one database, the same tables, no projections and no
// read models.
package app
