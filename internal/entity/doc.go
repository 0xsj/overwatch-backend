// Package entity is identity across sources and time.
//
// Built 2026-09-07 — decisions/0036. What follows was the placeholder's contract
// and is now the package's; the additions are marked.
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
// # A fragment IS a tuple — 0036
//
//	(workspace, kind, value)
//
// Deduplicated, and that is not a denormalisation of an id: it is what a
// fragment is. An observation joins to one on exactly those three columns, and
// `observation` carries no fragment id — a subscriber creates fragments AFTER
// the observations that imply them, so such a column could only be written by a
// second domain into a schema it does not own.
//
// # A fragment admits a MANUAL origin — 0036, resolving 0009's own doubt
//
//	observed   read out of an artifact. Carries first_seen and last_seen
//	manual     typed in. Carries neither, and that is not a gap
//
// `0009` said a `/24` typed into a scope rule is a fragment nobody observed, that
// admitting it was "more likely" than the alternative, and that it "is not
// written down anywhere". It is now.
//
// # A RULE decides without a person — 0036 amends 0008
//
//	decided_at   set  iff  state <> proposed
//	decided_by   set  iff  a PERSON decided
//
// `human` and `rule` claimants are born ACCEPTED; only a `model` is born
// proposed, and only a model carries confidence. Asking somebody to agree with a
// scope rule they wrote is the `reviewed vs in scope` pair collapsing backwards.
//
// # Assembled by a SUBSCRIBER, so the graph is eventually consistent
//
// `extract.observation.created` -> upsert fragments -> ask scope's CLAIM gate ->
// attribute the permitted ones to the target's root entity. A finished run's own
// record is complete; the graph over it arrives one outbox delivery later.
//
// # Deliberately absent
//
// **`derivation`.** `0003` is sealed on its shape and nothing can produce one:
// an edge means "this fragment was read out of that one" and needs the upstream
// fragment known at extraction time, which needs the chain to feed itself. The
// first one written must already carry an invocation and an artifact, which is a
// shape better decided against a caller than in advance.
//
// **`entity.merged`.** Identity resolution across sources needs a rule for what
// happens to the attributions on both sides. Two entities for one real-world
// thing is the state the product is in until then.
//
// # Emits
//
//	entity.root.created · entity.fragment.seen
//	entity.attribution.proposed · accepted · rejected
//	entity.judgement.set
package entity
