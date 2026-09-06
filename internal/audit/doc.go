// Package audit is the Observer: it records who did what, and imports no domain.
//
// # PLACEHOLDER — not built
//
// # Owns
//
// One append-only ledger — decisions/0006 for its shape, decisions/0007 for how
// an entry gets written.
//
// An entry carries a scope of system | account | workspace, a namespaced action,
// the subject, the actor and on_behalf_of, the workspace where the scope is a
// workspace, and the correlation and causation of the request that caused it.
//
// **One ledger, three reads.** An action may have more than one audience —
// "sj removed a member" is an account event for sj and an org event for everyone
// else — and splitting at write time forces a decision about who will ask before
// anyone has asked. One ledger projects three ways; three ledgers never merge.
// Who may read what is authorisation on the read side, not a table shape.
//
// **Append-only, and enforced rather than agreed:** the repository exposes no
// update and no delete. An entry that can be edited is a record of what somebody
// currently wants to have happened.
//
// **Idempotent by event id**, so a redelivered event writes one row. Delivery is
// at least once — decisions/0007 — and the event id is the deduplication key all
// the way down.
//
// # Does not own
//
// The invocation log. **Logs is machine work; audit is human decisions**, and v1
// learned this by overloading a single concept and having to split it. They have
// different retention and different read rates, which is why they are different
// tables.
//
// Provenance, which is the causal chain of a request rather than a record of a
// decision. custody/, which records agent runs and is not part of the product.
//
// # Emits
//
// Nothing. It subscribes and imports no domain — it receives an event and writes
// its own rows. That is what makes the boundary structural rather than agreed.
package audit
