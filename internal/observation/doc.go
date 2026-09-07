// Package observation holds what ONE SOURCE SAID, per field, with lineage to the
// bytes it was read out of.
//
// **An observation, never a fact.** `PRODUCT.md` calls that the load-bearing
// word: every other tool treats its records as facts, and calling ours a fact
// commits the same error in the vocabulary. A source said something. That is all
// that is ever held.
//
// # Per field — decisions/0035
//
// Six fields read out of one httpx record is SIX rows, not one row with six
// columns. That is what makes lineage a per-VALUE link, and what lets a
// correction change what one field means without touching the other five.
//
//	subject_kind · subject_value    what it is about
//	field · value                   what was said
//	invocation · artifact · mapping the lineage
//	observed_at                     when the TOOL ran
//
// The subject is carried INLINE rather than as a `fragment` reference.
// `fragment` is UNBUILT, and an observation is complete without one — *a source
// said this host has this server header* is a whole statement. A fragment is a
// dedup over those two columns and adds a column here rather than changing what
// a row means.
//
// # A field nobody mapped is RECORDED, never guessed
//
// Extraction enumerates every leaf path in a record and diffs it against the
// live mappings. A path with no mapping becomes an [Unmapped] row:
//
//	FIELDS SEEN   every distinct leaf path
//	MAPPED        paths a live mapping claimed
//	LEFT ALONE    the difference — kept in the artifact, never guessed at
//
// **This is the most tempting rule in the product to break.** `webserver` is
// obviously a server header. Naming it anyway would put a value in the record
// that no source was asked for, carrying lineage that points at bytes which do
// not justify the NAME — and that is the difference between an observation and a
// fact, one level down.
//
// It is also the only measurement of the correction loop that exists, which is
// `PRODUCT.md`'s central economic claim and has never run long enough for
// anybody to know whether it improves.
//
// # The expression language is a dotted path and nothing more
//
//	.host      .a.b      .a[].b
//
// No filters, no functions, no arithmetic. The moment an expression can COMPUTE,
// a mapping stops being a reading and becomes a derivation — and `0003` is
// emphatic that those are different edges and only one of them is a claim.
//
// # These are the engagement's rows — 0031
//
// `workspace_id`, non-null. An observation is a claim about a client.
package observation
