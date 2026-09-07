// Package memory is tool's repository without a database — a first-class
// adapter, not a test double.
//
// **Three indexes the database holds are re-stated here as scans**, because an
// adapter that accepts what Postgres refuses moves a failure from `make dev` to
// production:
//
//	tool_live_name    one live name per org, case-folded
//	mapping_version   (tool, field, version) assigned once
//	mapping_live      at most one live version per (tool, field)
//
// [Store.InTx] snapshots and restores; see internal/identity/infra/memory.
package memory
