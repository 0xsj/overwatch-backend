// Package journal records every unit of work and what caused it, and imports no
// domain.
//
// # What it is for
//
// One question: **what caused this.** A row is an event that crossed the outbox,
// kept with its origin, actor, depth, attempt and causation, so the chain from a
// scheduled run down to the observation it produced can be walked without
// guessing.
//
// It is the store behind the Logs surface — the one whose stated purpose is
// *"every unit of work, and what caused it"*, faceted by origin, outcome, actor
// and depth.
//
// # Why it is not internal/audit
//
// decisions/0014. A log line is about work and an audit entry is about a
// decision, and the discriminator is whether the act could have failed: work has
// an outcome, a decision does not.
//
// **The same act often produces one of each**, joined by correlation and not
// duplicated. Generating a report is a unit of work — it took time, omitted two
// sections, could have failed — and also a decision, because a client received a
// document. Two questions, two readers, two retentions.
//
// **Retention is the asymmetry that earns the second table.** Audit is kept
// forever: it is the record this product exists to keep. This is **expirable**,
// and that is the load-bearing claim rather than any particular window —
// `ingest.artifact.parsed · attempt 4 · redelivered` is not worth a year of
// storage, and putting it in the ledger that must never be swept would force one
// of those two rules to break.
//
// # Why it is not pkg/outbox
//
// The outbox is a queue and deletes on delivery. Today the only events that
// survive it are the ones that **failed**, through buried_at — exactly backwards
// for a screen whose first facet is `ran`. This subscribes to the same dispatcher
// and writes what the queue is about to forget.
//
// # Why it is not internal/invocation
//
// An invocation is one process the runner spawned: argv, exit, bytes, or a
// refusal. `ingest.artifact.stored` and `system.boot` are neither that nor a
// decision, and they belong on the same timeline as the runs that caused them.
// An invocation is evidence and is kept forever — decisions/0015 makes a model
// call one of those too; this is the timeline over all of it.
//
// # Owns
//
// One append-only table. Every event, decision or not, with the envelope
// flattened into columns a facet can filter on:
//
//	action        the event name        origin       provenance's
//	subject       decisions/0013        depth        provenance's
//	actor         provenance's          attempt      provenance's
//	on_behalf_of  provenance's          decision     decisions/0014
//	correlation · causation             detail       the payload, verbatim
//
// **Nothing here decodes a payload**, for the same reason nothing in audit does —
// it would mean importing the emitter's type, which the import checks forbid.
// decisions/0013 put every field this needs in the envelope precisely so that
// neither subscriber has to.
//
// **Idempotent by event id.** Delivery is at-least-once and a redelivered event
// writes one row, with `attempt` reflecting the delivery that won rather than
// the count. The same known gap as audit's applies: a retried COMMAND mints a
// fresh event id and produces a second row.
//
// # Deliberately absent
//
// **An outcome column.** The Logs surface facets on `ran · running · refused ·
// failed · unmapped` and nothing in the envelope carries it. It belongs with
// internal/invocation, which is unbuilt, and inventing its vocabulary here would
// fix the runner's shape before the runner exists. Until then a reader gets the
// outcome from the action name, which is worse and is honest about being worse.
//
// **The expiry sweep.** The claim that this table is expirable is what matters;
// a scheduled delete is a process, and there is one process and nothing to
// expire yet.
//
// # Emits
//
// Nothing. It subscribes and imports no domain.
//
// # Storage
//
// Schema `journal`, its own migration sequence, its ledger in that schema, and
// no foreign key crossing out of it.
package journal
