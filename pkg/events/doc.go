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
