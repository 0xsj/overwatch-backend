// Package memory is target's repository without a database — a first-class
// adapter, not a test double.
//
// **The live-name uniqueness is here, case-folded**, because the Postgres index
// is `target_live_name (workspace_id, lower(name)) where status <> 'archived'`
// and an adapter that accepts what the database refuses moves a failure from
// `make dev` to production.
//
// Every read takes the WORKSPACE and checks it. A store that ignores the tenant
// argument passes every test and leaks in production, which is precisely the
// mistake decisions/0005 makes structural.
package memory
