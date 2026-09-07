// Package limit is one token bucket per key, and the two shapes a caller needs
// from it.
//
// # Two callers, one primitive, opposite answers
//
//	Allow   non-blocking. "May I?"  -> false means REFUSE
//	Wait    blocking.     "Tell me when."
//
// A sign-in endpoint wants [Limiter.Allow]: too many attempts from one address
// is a refusal the caller must see. A collector wants [Limiter.Wait]: a host
// budget is politeness, and the work should slow down rather than fail. The same
// bucket answers both, which is why this is one package and not two.
//
// # Why a per-HOST budget cannot be a per-source interval
//
// A source carries its own cadence, and two workspaces both pointing at one site
// each honour theirs while the site sees the sum. Nothing at the source level can
// see that, because the thing being protected is not ours — it is somebody
// else's server, and every source aimed at it shares one.
//
// So the key is the host, deliberately not the source and not the workspace. Two
// engagements scanning the same company share a budget, and that is the correct
// answer rather than an unfortunate one.
//
// # A limiter keyed by untrusted input is a memory exhaustion vector
//
// This is the trap, and it is specific to the two uses above: hosts come from
// targets, and addresses come from whoever is connecting. A map keyed by either
// grows until the process dies, and the attack is one request per key.
//
// **A full bucket is indistinguishable from a bucket that does not exist.** Both
// answer yes to the next request and both refill to the same place, so a full
// bucket can be dropped with **no observable difference** — which makes eviction
// free rather than a trade. [MaxKeys] bounds the map and the sweep drops full
// buckets first; only if that is not enough does it drop the least recently used,
// which is the only case where anything is given up.
//
// # The clock is injected, because every interesting case is a boundary
//
// A limiter tested against wall time is a test that sleeps, and a test that
// sleeps is a test somebody deletes. [Clock] is the same one-method shape
// pkg/id, pkg/logger and pkg/outbox declare — interfaces belong to the consumer,
// and the composition root asserts the match.
//
// # Deliberately absent
//
// **A distributed limiter.** This bounds one process. Two processes each get the
// full budget, which is wrong the day there are two — and the fix is a shared
// counter in Postgres or Redis, which is a different implementation behind this
// same interface rather than a parameter on it.
//
// **A circuit breaker.** A host that fails every request should be backed off,
// and that is a different signal — this counts requests, not outcomes. Trigger:
// the first collector that keeps hammering something already broken.
//
// **Priorities and fairness.** Every caller of one key is equal here. The first
// time an interactive request has to queue behind a bulk backfill is the moment
// that stops being true, and it wants weights rather than a second limiter.
package limit
