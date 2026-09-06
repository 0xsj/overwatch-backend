// Package events is the vocabulary a domain publishes in, and nothing else.
//
// It holds a type, a name rule, and two ports. It has no storage, no delivery,
// no bus and no knowledge of Postgres — those are pkg/outbox's job, and keeping
// them apart is what lets a domain import this while importing no
// infrastructure at all.
//
// # An event is a fact that already happened
//
// The name is past tense and the payload is what was true, not what to do.
// `identity.account.created` is an event; `identity.account.create` is a
// command wearing an event's clothes, and a subscriber cannot refuse it.
//
// That distinction is what makes a subscriber safe to add: nothing a handler
// does can prevent the fact, because the fact is already committed. A publisher
// that waits to hear whether a subscriber approved has built a synchronous call
// with extra machinery.
//
// # The name is namespaced, and the first segment is the domain
//
//	identity.account.created
//	└──────┘ the namespace — a facet the client groups by
//
// Lowercase, dot-separated, `[a-z0-9_]` per segment, at least two segments.
// [ValidName] enforces it and [New] refuses an event without one, because a
// misspelled name is indistinguishable from a new kind of event and neither the
// publisher nor the subscriber can tell.
//
// **Names are declared as constants beside the command that emits them**, never
// written inline. decisions/0006 takes the cost of a string over a closed set —
// a closed set needs a migration for every new recordable act — and this is the
// mitigation that makes the cost bearable.
//
// # Every event names its subject, and that is what keeps a subscriber pure
//
//	account:0198f3c1-...      the thing the act was performed ON
//	└─────┘ kind              └──────────────┘ id
//
// decisions/0013. The form matches [provenance.Actor.String] — `user:acct_1` —
// so the envelope has one convention for "which thing" rather than two.
// [Event.SubjectKind] and [Event.SubjectID] split it, so a subscriber groups by
// kind without parsing and without a lookup.
//
// **It is in the envelope rather than the payload because of who needs it.**
// An audit ledger records who did what to what, and every other field it wants
// is already here: the action is [Event.Name], the actor and on_behalf_of and
// the correlation are in the provenance, the workspace is the tenant, and the
// payload is stored verbatim as detail. The subject was the one field it had to
// reach into a payload for — which would mean importing every emitter's type
// (the import checks forbid it), hand-mirroring every struct (silent drift), or
// guessing a key out of a map (a convention nothing enforces). All three are the
// same mistake: making a cross-cutting field the payload's problem.
//
// **[New] refuses an event without one**, in the same breath as a bad name and
// an absent provenance. An optional field is a field every emitter forgets, and
// the forgetting is invisible until a reader needs it and finds years of blanks.
//
// The subject is **the noun a reader would search for**, not the row that
// happens to have been created. `identity.session.started` has subject
// `account:<id>`, because "what happened to this account" is the question a
// trail is asked; the session's own id is payload detail. An act with no
// narrower target takes `system:<name>`, which keeps the rule total instead of
// carving an exception.
//
// It is **not a foreign key and cannot become one.** It crosses schemas by
// design and nothing enforces that the id names a live row — which is correct
// for a ledger: an entry about a workspace must survive the workspace.
//
// # A unit of work and a decision are two constructors, not a flag
//
//	events.New          a UNIT OF WORK. It has an outcome — it ran, was
//	                    refused, failed. The journal records it
//	events.NewDecision  an act a PERSON is accountable for. Audit records it
//	                    AS WELL, because a decision is usually also work
//
// decisions/0014. The discriminator is **can it fail**: a unit of work has an
// outcome, and a decision does not — sj dismissed a finding, or sj did not.
// There is no "sj dismissed it, failed."
//
// **The two are not exclusive**, which is why [Event.Decision] is a field and
// not a kind. Generating a report is a unit of work — it takes time, omits
// sections, can fail — *and* a decision somebody answers for, because a client
// received a document. One event, two subscribers, two predicates.
//
// **The emitter declares it and a subscriber cannot derive it.** Audit sees the
// actor in the provenance, but "this changed stored state" is knowledge only the
// command has, and inferring it from the event name is a convention nothing
// enforces. The rule, applied at the call site in order: did a person cause it;
// did it change state that outlives the request; if it changed nothing, did it
// disclose something. A per-viewer preference — a saved filter, a theme — is not
// stored state.
//
// **The obvious shortcut is wrong and is worth naming.** "A person is the actor,
// therefore audit" fails on `runner.run.started` with actor `user:sj`: a person
// STARTING something is work. The test is the nature of the act.
//
// Two constructors rather than a bool parameter so the choice is legible where
// it is made, cannot be left to a zero value, and so that grepping NewDecision
// enumerates everything in the tree claiming to be auditable.
//
// # Every event carries the provenance of the work that produced it
//
// Not a copy of some fields: the [provenance.Provenance] value itself, which
// carries correlation, causation, origin, depth, attempt and the actor. So a
// subscriber writing a record months later still knows what caused the thing it
// is recording, which is the property a bare `(name, payload, timestamp)` event
// destroys permanently — see [[correlation-is-the-tree-causation-is-the-edge]].
//
// The event's own id is the **deduplication key**. Delivery is at-least-once, so
// a handler will see the same event twice and must be idempotent on that id.
// decisions/0007 requires it and this is where the id comes from.
//
// # Two ports, both declared here and satisfied elsewhere
//
//	Publisher  Publish(ctx, ...Event) error   pkg/outbox satisfies it
//	Handler    func(ctx, Event) error         a subscriber is a function
//
// [Handler] is a function type rather than an interface because a subscriber has
// exactly one method — [[single-method-ports-as-functions]]. A subscriber with
// state is a closure over it.
//
// A domain declares the narrow publisher interface *it* needs and does not import
// the outbox; the composition root asserts the match. That is why this package
// exists separately at all.
//
// # Deliberately absent
//
// A registry, a router, or subscription by name. Delivery decides who gets what,
// and delivery is not here. A handler that cares about one namespace checks
// [Event.Namespace] in one line.
//
// Versioned or schema'd payloads. The first payload that has to change shape
// while old rows exist is the moment to decide between a version suffix in the
// name and a tolerant reader, and guessing now would pick one for reasons that
// do not exist yet.
//
// Ordering guarantees. Nothing here promises two events arrive in the order they
// were published. A subscriber needing order has the causation chain to
// reconstruct it, which is stronger than a queue's ordering and survives a
// second process.
package events
