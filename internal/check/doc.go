// Package check owns the named questions a firm asks, and the chains that
// answer them.
//
// # Two things, stored separately — decisions/0032
//
//	the QUESTION   a sentence a person reads on the coverage screen
//	the CHAIN      the tools that answer it, and how bytes flow between them
//
// Both are kept because "which tools ran" and "what were we asking" are
// different facts, and **coverage is computed from the second**. A chain that is
// rewired is still the same question; a question that changes is a different
// check even if the tools are identical.
//
// # Applicability is declared, not derived — 0011 and 0032
//
// `0011` made coverage RAGGED: the denominator is the sum of applicable checks
// per subject, never `subjects × checks`, because an ASN has no TLS certificate
// and that cell is not a gap in coverage — it is a question that does not exist.
//
// It named the table it did not design. This is that table, as a column:
//
//	applies_to   the subject kinds this check has an answer for
//
// **It is data a person types, not a rule this system can check.** `0011` says
// the first mistake will be `ip` versus `host`, and nothing here improves on
// that — what it does is put the mistake somewhere visible.
//
// # A check with no steps is the human check
//
// `READ BY YOU` is a check whose chain is empty and whose interval is NULL. Both
// fall out of the shape rather than out of a special case: nothing spawns, and a
// human read does not go stale. **A NULL interval means "when somebody asks"**,
// which is a kind of check and not an unset field.
//
// # These are the firm's rows — 0031
//
// `org_id`, non-null, no workspace. "What ports are open" is a question the firm
// knows how to ask and it is the same question for every client. A per-engagement
// "do not run this here" is deliberately absent: that is `scope`, which refuses
// the spawn by a rule somebody wrote and can be shown.
//
// # Who may
//
//	read    org membership
//	write   owner | admin
//
// The write gate is wider than any engagement gate, and worse here than for a
// tool: a check is what spawns processes against a client. It is defensible only
// because `scope` refuses the spawn independently.
//
// # Nothing runs
//
// `run` and `invocation` are UNBUILT, so `interval` and `enabled` are fields
// nothing reads. 0032 §Consequences argues that narrow exception to CLAUDE.md §5.
package check
