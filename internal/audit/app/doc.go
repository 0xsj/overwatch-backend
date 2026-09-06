// Package app is audit's one behaviour: turn an event into an entry, or into
// nothing.
//
// **The reasoning lives in internal/audit.**
//
// [Subscriber.Handle] is an [events.Handler]. It returns nil for an event that
// is not a decision — **not an error**, because a non-decision is a successful
// outcome of asking the question, and an error would make the dispatcher retry
// an event that will never qualify. The journal takes those.
//
// It returns nil for an entry that already exists, too. Delivery is at-least-once
// (decisions/0007) and the unique index on event_id is what makes the second
// delivery a no-op; the boolean [Ledger.Append] returns says whether a row was
// written, and today nothing needs to know. It exists so a caller that one day
// wants to count real writes does not have to change the port.
//
// [Ledger] is declared HERE and satisfied by internal/audit/infra/postgres —
// interfaces belong to the consumer, and the composition root asserts the match.
// It is one method because that is all this needs; a wider port would be a
// promise about reads that nothing has asked for.
package app
