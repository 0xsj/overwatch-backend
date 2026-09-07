// Package query reads the journal. Its whole subject is one question — **what
// caused this** — and its shape follows from that being different from audit's.
//
//	audit    who did what. A DECISION. Kept for as long as the record is kept
//	journal  every unit of work and what caused it. EXPIRABLE — decisions/0014
//
// # A chain, not a list
//
// [Trail.ForCorrelation] returns the lines that share one correlation id, oldest
// first, which is the tree a single act produced: a registration is three hops
// across three domains, and the correlation is the only thing that joins them.
// Ordering by `occurred_at` ascending rather than descending is deliberate —
// this is read as a story, and a story reads forwards.
//
// **`depth` is the shape and `causation` is the edge.** Depth says how far from
// the originating act a line is; causation names the event immediately before
// it. A renderer that has both can draw the tree without a second query, which
// is why both are on every line rather than derived.
//
// # It has no page, and that is a bound rather than an oversight
//
// A correlation is one act. `pkg/provenance` bounds depth, so a chain cannot
// grow without limit, and a chain that somehow did would be a runaway to look at
// rather than to paginate. Audit pages because a ledger grows forever; a chain
// does not.
//
// # This package does not authorise, and a chain crosses domains
//
// The lines of one act name an account, an org and a workspace, and no single
// rule covers all three. So visibility is decided by the caller — the
// composition root, which is the only thing allowed to know that
// `org:<id>` means "ask org whether this person is a member".
//
// **A caller may see part of a chain and not the rest**, and is never told how
// much is missing. That is the same rule as a workspace they have no grant on
// being absent rather than refused.
package query
