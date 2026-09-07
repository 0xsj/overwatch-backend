// Package tool owns what the firm can run, and how its output becomes a field.
//
// **A tool is a definition and a field mapping — never an integration.** The
// distinction is `CLAUDE.md`'s and it is load-bearing: an integration is code
// per tool and a definition is a row, and the difference is whether adding
// `subfinder` is a deploy or an insert.
//
// # These are the firm's rows, not an engagement's — decisions/0031
//
// `0005` says every product row carries a `workspace_id`. `0031` narrows that to
// rows recording something **observed**, and a tool is not one:
//
//	observed    a claim about a client   -> workspace_id
//	capability  a thing the firm owns    -> org_id
//
// **The correction loop is why.** Fixing a mapping once must fix it everywhere;
// per-workspace mappings mean a firm re-corrects the same parser for every
// client it takes on, which is the cost the whole loop exists to avoid.
//
// # A mapping version is never edited
//
// An observation cites the version that produced it, so a version whose
// expression changed under a citation makes that observation's lineage a lie.
// Correcting a mapping is **adding a version** — the same reasoning `0030`
// applies to a scope rule, and for the same reason: three surfaces cite an id.
//
// **`correction` gets no table.** It is creating a version whose author is a
// person, and the distinction worth keeping — who authored it — is a field.
//
// # Promotion is what makes a version live
//
// A version is drafted, then promoted. Exactly one version of a tool's mapping
// is live at a time, and promoting a new one retires the old — so "which mapping
// produced this" always has an answer and "which mapping is running" has exactly
// one.
//
// # Who may
//
//	read    org membership
//	write   owner | admin — 0019 places "tool definitions" on that rung
//
// This is the first product surface the CAPABILITY GATE does not protect, and it
// is deliberate: an admin with no grant on any engagement can edit a parser every
// engagement uses. That is the same reach `0019` gives them over people.
package tool
