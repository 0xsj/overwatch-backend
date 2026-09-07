// Package egress is every connection this process makes outward, and the policy
// that decides whether it may.
//
// pkg/httpx is the inbound edge; this is the outbound one. They are split by
// DIRECTION rather than by protocol, because the questions are different: inbound
// asks who is calling, outbound asks whether we are allowed to call that.
//
// # Why this exists at all, in this product specifically
//
// Overwatch fetches hosts an attacker chose. That is the job — a target names a
// domain, a tool resolves it, and something connects. So the ordinary SSRF
// question is not a hypothetical here; it is the normal case:
//
//	*.example.com  ->  169.254.169.254   cloud metadata, credentials
//	*.example.com  ->  127.0.0.1:5432    this process's own database
//	*.example.com  ->  10.0.0.0/8        whatever else is on the network
//
// Without a guard, a scope rule permitting a customer's domain turns the runner
// into a proxy pointed at our own infrastructure, and the request looks exactly
// like the work it was asked to do.
//
// # The guard runs at DIAL time, and that is the whole design
//
// Checking the URL is not enough and the reason is DNS rebinding: parse, resolve,
// approve — and then the transport resolves again and connects somewhere else.
// The gap between the check and the connect is the vulnerability, and it cannot
// be closed by checking harder.
//
// [net.Dialer.Control] runs **after** the address is resolved and **before**
// connect, and returning an error there refuses that specific connection. So the
// check is on the address actually being dialled, and there is no gap to race.
//
// It also covers redirects for free: a 302 to `http://169.254.169.254/` is a new
// dial, so it meets the same hook rather than a second implementation of the
// same rule.
//
// # Two gates, and neither implies the other
//
//	scope    permits a TARGET     decisions/0010, at the domain
//	egress   permits an ADDRESS   here, at the socket
//
// A target can be perfectly in scope and still resolve somewhere forbidden, and
// an address can be fine while the target is out of scope. Both must pass. They
// are the same shape as CLAUDE.md's never-collapse pairs and for the same
// reason: one field that means both cannot say which one refused.
//
// # What is blocked, and the two that are usually forgotten
//
//	loopback · unspecified · multicast    the obvious ones
//	private 10/8 · 172.16/12 · 192.168/16
//	link-local 169.254/16 · fe80::/10     cloud metadata lives here
//	carrier-grade NAT 100.64/10
//	IPv4-mapped IPv6  ::ffff:127.0.0.1    a bypass, not an address family
//	NAT64  64:ff9b::/96                   maps IPv4 — including private — into v6
//
// The last two are the ones a hand-rolled check misses. [netip.Addr.Unmap] is
// applied before every test, so `::ffff:10.0.0.1` is judged as `10.0.0.1` rather
// than as an ordinary IPv6 address that passes every v4 rule.
//
// # The policy is a parameter, and that is not a weakness
//
// A guard with the rules hard-coded cannot be tested: every test server binds
// loopback, so a guard that always refuses loopback refuses its own suite. So
// [Policy] carries the exceptions, [Deny] wins over [Allow], and the zero value
// is the strict one — a caller who configures nothing gets the safe behaviour
// rather than the convenient one.
//
// # A refusal is not a failure
//
// [ErrBlocked] carries [errors.Forbidden], which is **not retryable**: retrying
// a refusal spends the budget re-refusing. A network failure is Unavailable and
// is. Collapsing them is how a permanent policy decision becomes a retry loop.
//
// decisions/0010 calls the equivalent at the domain "a refusal", and it is a
// recorded outcome rather than an error — the same distinction lives here, one
// layer down.
//
// # What a response carries, and why the address is on it
//
// [Response] holds the status, the headers, a capped body — and **the address
// actually connected to**, taken from httptrace rather than from a second
// resolution that might disagree.
//
// That field is not diagnostics. "example.com resolved to 93.184.216.34 at
// 14:02" is an observation this product exists to record, and it is free here
// because the guard had to know it anyway. Anywhere else it is a second DNS
// lookup that may answer differently.
//
// # Deliberately absent
//
// **Retry.** Which failures are worth retrying is the caller's policy and
// [errors.Retryable] already answers it from the kind.
//
// **A cookie jar.** Two targets sharing a jar is one target's session reaching
// another's host. Jar is nil and stays nil.
//
// **Rate limiting.** A per-host budget is a real requirement and a different
// concern — it needs to outlive one client and be shared across every source
// pointing at one site. pkg/limit, when it exists.
//
// **A proxy.** An HTTP proxy resolves the name at the far end, which puts the
// address outside this guard's reach and silently disables it. Adding one means
// deciding what replaces the guard, not just setting a field.
package egress
