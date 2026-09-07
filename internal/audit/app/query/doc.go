// Package query reads the audit trail. It never writes, and there is nothing
// here that could: an entry is written by the subscriber from an event, and a
// read side that could append would be a way to put a row in the ledger that
// nothing actually did.
//
// # Two reads, and the second one is the whole surface
//
//	ForSubject     one subject's history. "what happened to this account"
//	ForWorkspace   one engagement's history. "what happened on Acme Q3"
//
// **There is no org-wide read, and that is not an omission.** An entry carries a
// scope of `system`, `account` or `workspace` and no org id — decisions/0006 —
// so "the audit log for my firm" is not a question this table can answer. It is
// also not a question it SHOULD: an org-wide log lists rows about engagements
// the reader may not be on, and `0005` says an engagement they cannot see must
// be invisible rather than refused. A firm-wide view is the union of the
// workspaces the caller can reach, composed by whoever knows both vocabularies.
//
// # Paging is keyset, because the ledger grows at the head
//
// Offset pagination is wrong for an append-only table. Rows arrive ABOVE page
// one while somebody reads it, so page two re-shows rows they have seen and
// hides rows they have not — silently, and worse the busier the system is.
//
// A [Cursor] names the last row a reader saw. It is `(occurred_at, id)` and not
// the instant alone, because the registration chain writes several entries
// inside one millisecond and a cursor that cannot break that tie either repeats
// a row or skips one. `id` is a UUIDv7, so the pair is a total order that
// matches the order rows were written.
//
// **A page is returned with the cursor for the next one, and never a total.**
// Counting an append-only ledger is a full scan whose answer is stale before it
// is rendered, and `CLAUDE.md` says an unmeasured total renders as `–` and never
// as a number nothing computed.
//
// # This package does not authorise
//
// [Ledger.ForWorkspace] answers for any workspace id it is handed and says so.
// It cannot see a grant — org owns that table and decisions/0017 forbids the
// join — so the caller resolves access first. A query that silently authorises
// is the shape of an access bug: it looks safe at the call site and is wrong at
// every other one.
package query
