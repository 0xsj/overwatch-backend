// Package finding is a vulnerability claim with a lifecycle. NOT an observation.
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
// The finding, its lifecycle, and its severity — decisions/0004 is sealed:
// severity is a CLAIM carrying its claimant, a rule-assigned one has no
// confidence, and an override never deletes what it overrode.
//
// The SCOPE PROOF, denormalised onto the finding — the rule that permitted its
// subject, captured at finding time and never resolved at read time, because a
// later scope edit must not silently rewrite what a client was told. The rule may
// sit on either gate — decisions/0010 — which is what lets a finding on a
// repository state one at all.
//
// A finding is NOT a judgement value — decisions/0009. Judgement is what a person
// ruled about a fragment; a finding is what a tool claimed about one. A fragment
// may be "watching" AND carry an open finding at the same time, which is the case
// a single five-valued enum could not represent.
//
// The lifecycle is append-only and not forced-forward: every state is reachable
// from every other, each transition writes an actor, an optional note and a
// timestamp, and a move must not touch the observations, the artifacts, the
// severity or the scope snapshot.
//
// # Does not own
//
// The observation that suggested it. A finding cites evidence; it is not
// evidence.
//
// # Emits
//
// \tfinding.opened · severity.set · finding.moved · closed
package finding
