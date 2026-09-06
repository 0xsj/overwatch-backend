// Package entity is identity across sources and time.
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
// The entity, the FRAGMENT, and both edge kinds — decisions/0003 is sealed on
// their shape. An attribution is entity to FRAGMENT and IS a claim. A derivation
// is fragment to fragment and is NOT one. There is no third kind, and in
// particular no similarity edge.
//
// (This file said "observation" in both places until 2026-09-06. 0003 says
// fragment, and the two are not the same noun — a fragment is an identifier a
// source produced; an observation is what that source said about a field.)
//
// An attribution's claimant is who PROPOSED it — decisions/0008. Acceptance
// writes decided_by and decided_at, and never rewrites claimant, because a
// shape that overwrites it retains only the claims the model got wrong.
//
// The FRAGMENT carries a judgement — decisions/0009 — as does the entity. Four
// values: unopened, triaged, watching, dismissed. A finding is not one of them.
// Two identical column groups rather than one polymorphic foreign key.
//
// An ASSET is not a table. It is a fragment of a targetable kind carrying an
// accepted attribution to the target's root entity — decisions/0009.
//
// # Does not own
//
// The observations themselves, and the surface they describe.
//
// # Emits
//
// \tattribution.proposed · accepted · rejected · entity.merged
package entity
