// Package memory is check's repository without a database — a first-class
// adapter, not a test double.
//
// **Two things the schema holds are restated here as scans:**
//
//	check_live_name    one live name per org, case-folded
//	SaveChain's order  prune steps, then rewrite flows
//
// The second is not a constraint, it is a sequence — and it is restated for the
// same reason: an adapter that permits an ordering the database refuses moves a
// failure from `make dev` to production.
package memory
