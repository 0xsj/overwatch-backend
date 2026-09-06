// Package observation is what ONE SOURCE said, with lineage back to the bytes.
//
// # PLACEHOLDER — not built
//
// This file exists so the directory has a name and a contract before it has
// code. Written 2026-09-06 from drafts/domain-map.md, which binds nothing. It is
// expanded into a real contract by the session that builds this domain, and that
// expansion happens BEFORE the implementation — custody 0010-0013 measured why:
// where the document states the behaviour a barriered suite dominates, and where
// the code moved past the document it can see nothing.
//
// # Owns
//
// The observation: its subject, its field, its value, and lineage — the
// invocation, the artifact, and byte offsets into it.
//
// THREE STATES for every field, and they are three: found, looked for and not
// there, never looked for. omitempty on a bare string collapses two of them,
// which is the bug that writes a live host off as dead.
//
// A rule-derived observation carries NO confidence. Not 1.0 — absent. In fact
// NO observation carries one: confidence is the signature of a claim that two
// things are the same, which is an attribution. Fifty-five screens agree —
// "rule-derived | no confidence" is what the log renders.
//
// An observation may have MANUAL, run-less provenance: verified by hand, with an
// artifact and no invocation. The lineage chain must admit a non-tool source.
//
// COVERAGE is a projection rooted here and it is RAGGED — decisions/0011. The
// denominator is the sum over subjects of the checks APPLICABLE to that
// subject's kind, never subjects times checks. An ASN has no TLS certificate, and
// a cell for one is not a gap in coverage — it is not a pair. Four cell states:
// fresh, stale, never, n/a — and n/a leaves both numerator and denominator.
//
// That is the three-state rule one level up, and getting it wrong inflates the
// never-attempted count in the one report section that exists to avoid implying
// a completeness nobody achieved.
//
// # Does not own
//
// Identity across sources. Two observations agreeing is not an entity.
//
// # Emits
//
// \tobservation.recorded
package observation
