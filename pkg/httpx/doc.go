// Package httpx is the inbound HTTP edge: provenance, recovery, logging, and
// turning an error into a status.
//
// It is the only package in the shared layer that knows what HTTP is. pkg/errors
// deliberately does not — it supplies a [errors.Kind], and the translation to a
// status code lives here, because a domain that knows about 404 is a domain that
// cannot be reused behind anything else.
//
// # Provenance is adopted in one place
//
// [WithProvenance] mints a request identifier for every inbound request and
// **adopts** what the caller supplied — correlation, causation, traceparent. A
// request arriving from another system continues that system's chain; a request
// arriving from nowhere becomes an origin. Both are the same code path, and that
// is the point: one gap disconnects the whole graph, so there is exactly one
// place where a chain can begin.
//
// The origin is [provenance.OriginRequest], which is what that value exists for.
//
// The identifiers go back out on the response, so a caller can quote one in a
// support ticket and it resolves to everything the request touched. Only request
// and correlation are echoed — causation is an internal edge and telling a
// caller what caused their own request is noise.
//
// Adoption is lenient by construction. pkg/provenance drops what it cannot
// parse rather than refusing the request, because a broken trace link is a
// cheaper failure than a rejected request, and nothing branches on these values.
//
// # Identifier is the seam authentication will use
//
// It maps a request to an actor, and nil means anonymous. Authentication does
// not exist yet, so nothing is wired — but the *place* it attaches does, and it
// attaches at the same point provenance is minted rather than somewhere later.
// An actor discovered after the request is already logged is an actor missing
// from the line that matters.
//
// # Kind to status is total, and the default is 500
//
// [Status] switches on every [errors.Kind] and its default arm is 500. An
// unmapped kind is a gap in that table, and the safe reading of a gap is "we do
// not know" — which is an internal error, not a 400 blamed on the caller.
//
// # The body cannot leak an internal message
//
// [WriteError] builds its message from errors.Message, which returns a fixed
// string for errors.Internal regardless of what the error actually says. A
// wrapped SQL failure reaching the edge shows a caller nothing, and the real
// chain stays in the log where it belongs.
//
// That is a property of pkg/errors rather than of this package, and it is why
// the edge needs no scrubbing of its own. FieldsOf is included because it is
// caller-safe by contract; DetailsOf never is, and is not reachable from here.
//
// # Recovery re-panics one value on purpose
//
// http.ErrAbortHandler is how net/http signals a deliberate abort — the server
// recovers it silently and closes the connection. Swallowing it and writing a
// 500 would convert an intentional disconnect into an error page and a
// misleading log line, so it is re-panicked untouched.
//
// Everything else is logged with the request's provenance and answered with a
// 500 whose body says nothing about the panic. A panic value routinely contains
// the thing that broke, which is routinely a connection string.
//
// If the handler had already written a response before panicking, nothing is
// written over it. Half a response is worse than an unexplained one.
//
// # The recorder, and why status defaults to 200
//
// A handler that writes a body without calling WriteHeader has sent a 200, and
// the wrapper reports the same, so a log line matches what the client saw.
//
// Flush passes through, because a product that streams will want it and a
// wrapper that silently breaks streaming is discovered late. Unwrap is
// implemented for http.ResponseController.
//
// # The client address is not a header
//
// X-Forwarded-For is caller-controlled. [TrustedProxies] is empty by default and
// an empty set trusts nothing, so the peer address wins and a forged header
// changes nothing — the zero value is the safe one.
//
// When proxies are configured the walk goes **right to left**, from the peer
// inward, and stops at the first hop that is not trusted. That hop is the client.
// Walking left to right takes the first value the caller wrote, which is the one
// value they fully control.
//
// # Real time, deliberately
//
// Request duration uses time.Since, which reads the monotonic clock. This
// measures an elapsed interval for a log line rather than stamping an instant,
// and injecting a clock to fake a duration nothing asserts on would be ceremony
// — see [[monotonic-and-wall-time]] for why the distinction is made per use.
//
// # Deliberately absent
//
// Routing. Go 1.22's ServeMux is the answer and a middleware package does not
// need to wrap it.
//
// Tracing and metrics. Both want an exporter, and there is no second process to
// correlate with yet. The traceparent is carried verbatim so that decision stays
// open.
//
// Content negotiation, compression, CORS. Each arrives with the first caller
// that needs it, and each is a decorator over an http.Handler when it does.
package httpx
