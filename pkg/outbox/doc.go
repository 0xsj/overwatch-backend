// Package outbox delivers events at least once, and writes them in the
// transaction that produced the fact.
//
// decisions/0007 is the reason it exists and states the property it must have:
// the fact and its event commit **together or not at all**. A plain in-process
// bus cannot do that — the row commits, the process dies, the event is gone —
// and that gap is the whole difference between a bus and an outbox.
//
//	domain command ──┐ one transaction
//	                 ├─ the fact         (an accounts row)
//	                 └─ the event        (an outbox row)      atomic
//	                       │
//	                  Dispatcher · at least once
//	                       │
//	                  handlers ──> their own durable records
//
// # It is a queue, not a log
//
// A row is drained once delivered. This table is hot, narrow and short-lived;
// the durable records are whatever the handlers write. Keeping every event here
// forever would force a queue and an audit ledger to share retention and
// indexing, and they want opposite things — 0007 §Alternatives.
//
// So **the depth of this table is a health metric, not a history.** A number
// that keeps rising means the dispatcher is behind.
//
// # Publish takes the caller's transaction, and that is not optional
//
// [Publisher.Publish] writes through whatever [postgres.Pool.DB] returns for the
// context it is given — the transaction if one is in flight, the pool otherwise.
// Publishing outside a transaction compiles, runs, and silently gives up the one
// property this package exists for. Nothing can detect it from inside; it is the
// review property 0007 names.
//
// # At least once, so a handler is idempotent or it is wrong
//
// A handler may see the same event twice: a delivery that succeeded and then
// failed to be marked, a process killed between the two. The event id is the
// deduplication key, and 0006 already requires the audit ledger to be idempotent
// on it.
//
// **All handlers share one delivery attempt.** If any handler fails, the event is
// retried and every handler sees it again — including the ones that succeeded.
// That is the cost of not tracking delivery per (event, handler), and it is
// deliberate: per-handler tracking is a second table and a second thing to get
// wrong, and idempotent handlers make it unnecessary. It becomes the wrong trade
// the day a handler is expensive rather than merely repeated.
//
// # One poisoned event does not stop the queue
//
// A handler that keeps failing must not block every other event, so a failed
// delivery increments an attempt count and backs off. Past [Config.MaxAttempts]
// the row is **buried** — marked dead, left in place, and skipped forever.
//
// Buried, not deleted. A deleted poison event is a fact this system was told and
// then lost, and the reason it could not be delivered is the only place the bug
// is visible. It stays queryable, which is what makes it findable.
//
// # Claiming is `FOR UPDATE SKIP LOCKED`
//
// Two dispatchers may run. Each claims a batch, the other skips those rows
// rather than blocking on them, so a second process adds throughput instead of
// contention. It is also why [Store] is a port rather than an implementation
// detail: the memory adapter exists so a test of the dispatcher's *policy*
// — backoff, burial, poison isolation — needs no database at all.
//
// # Deliberately absent
//
// Ordering. Nothing here promises two events arrive in the order they were
// written; `SKIP LOCKED` with more than one dispatcher makes that impossible to
// promise cheaply, and the causation chain is a stronger answer for a subscriber
// that needs one.
//
// Subscription by name. The dispatcher hands every event to every handler and a
// handler checks [events.Event.Namespace] if it cares. A routing table is worth
// building when there are enough handlers for the check to be a cost.
//
// A retry schedule per handler, dead-letter replay, and a metrics exporter. Each
// is a real thing to want; each arrives with the operator who wants it.
package outbox
