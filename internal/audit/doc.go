// Package audit is the Observer: it records who decided what, and imports no
// domain.
//
// # What earns an entry
//
// **A decision, not a unit of work** — decisions/0014. The discriminator is
// whether the act could have failed: a unit of work has an outcome (it ran, was
// refused, failed), and a decision does not. sj dismissed a finding, or sj did
// not; there is no "sj dismissed it, failed."
//
// This package does not apply that rule. **The emitter does**, by calling
// [events.NewDecision] rather than [events.New], and this package writes an
// entry for exactly the events that arrive with Decision set. That is deliberate
// and it is the only arrangement that works: "this changed stored state" is
// knowledge the command has and a subscriber does not, and inferring it from an
// event's name is a convention nothing enforces.
//
// **The shortcut that looks right and is not:** recording an entry whenever the
// actor is a person. `runner.run.started` carries actor `user:sj` and is a log
// line — a person STARTING something is work. The test is the nature of the act
// and never the kind of the actor.
//
// # Owns
//
// One append-only ledger — decisions/0006 for its shape, 0007 for how an entry
// gets written, 0014 for which events become one.
//
// An entry carries a scope of system | account | workspace, a namespaced action,
// the subject, the actor and on_behalf_of, the workspace where the scope is a
// workspace, the correlation and causation of the request that caused it, and
// the payload verbatim as detail.
//
// **Every one of those comes from the envelope, and that is what keeps this
// package pure.** The action is the event name, the subject is [events.Event]'s
// (decisions/0013 — it is there precisely because this package needed it and
// could not reach into a payload for it), and the rest is on the provenance.
// **Nothing here decodes a payload.** It is stored as it arrived.
//
// # Scope is derived, and the derivation is the whole rule
//
//	the provenance has a tenant       -> workspace, and workspace_id is set
//	otherwise, the subject is an
//	  account                         -> account
//	otherwise                         -> system
//
// It is derived rather than declared because an emitter would have to know
// audit's read model to state it, which is the coupling this package exists
// without. Getting it wrong files an entry under the wrong one of 0006's three
// reads, and that is invisible until somebody's activity view is quietly missing
// rows — so it is one small function with its own table test rather than an
// expression inlined at the write.
//
// # One ledger, three reads
//
// An action may have more than one audience — "sj removed a member" is an
// account event for sj and an org event for everyone else — and splitting at
// write time forces a decision about who will ask before anyone has asked. One
// ledger projects three ways; three ledgers never merge. Who may read what is
// authorisation on the read side, not a table shape.
//
// # Append-only, and enforced rather than agreed
//
// The repository exposes no update and no delete. An entry that can be edited is
// a record of what somebody currently wants to have happened.
//
// **Idempotent by event id**, so a redelivered event writes one row. Delivery is
// at-least-once — decisions/0007 — and the event id is the deduplication key all
// the way down. A unique index on it is what turns the second INSERT into a
// no-op rather than a second row claiming the thing happened twice.
//
// **A known gap, recorded rather than papered over.** decisions/0006 asks for
// idempotency by *entry* id so that a retried COMMAND writes one row; this
// gives idempotency by *event* id, which closes a redelivery and not a retry. A
// command that re-runs mints a fresh event id and produces a second entry. The
// fix is a derived event id — from correlation, causation and name — and it is a
// decision that has not been taken because nothing emits yet.
//
// # Does not own
//
// **The journal.** internal/journal records every unit of work; this records the
// subset a person is accountable for, and the same act often produces one of
// each — joined by correlation, not duplicated. They are separate tables because
// their retention differs: the journal is expirable and this is not.
//
// The invocation log. Logs is machine work; audit is human decisions, and v1
// learned this by overloading a single concept and having to split it.
//
// Provenance, which is the causal chain of a request rather than a record of a
// decision. custody/, which records agent runs and is not part of the product.
//
// # Emits
//
// Nothing. It subscribes and imports no domain — it receives an event and writes
// its own rows. That is what makes the boundary structural rather than agreed.
//
// # Storage
//
// Schema `audit`, its own migration sequence, its ledger in that schema, and no
// foreign key crossing out of it. `subject`, `actor` and `workspace_id` name
// rows in other schemas and are deliberately not references: an entry about a
// workspace must survive the workspace.
package audit
