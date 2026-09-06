package httpx_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/logger"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// ---------------------------------------------------------------------------
// helpers (all spec-prefixed to avoid collision with the sibling test file)
// ---------------------------------------------------------------------------

func specNewRequest() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "198.51.100.7:4242"
	return r
}

func specNewRequestWithPath(method, path string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	r.RemoteAddr = "198.51.100.7:4242"
	return r
}

func specStaticHandler(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
		}
		if body != "" {
			w.Write([]byte(body))
		}
	})
}

func specPanicHandler(v any) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(v)
	})
}

func specWriteThenPanicHandler(status int, body string, v any) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
		panic(v)
	})
}

// specFixedClock exists only because logger.New panics on a nil Clock — a
// requirement that was not in the material the spec-test writer was given.
// Mechanical wiring fix by the runner; no expectation was changed.
type specFixedClock struct{}

func (specFixedClock) Now() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

func specCaptureLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	l := logger.New(logger.Config{Format: logger.FormatJSON, Output: &buf, Clock: specFixedClock{}})
	return l, &buf
}

func specLogLines(buf *bytes.Buffer) []string {
	raw := strings.TrimSpace(buf.String())
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\n")
}

// specTrackedRecorder observes whether anything was ever written to it, so a
// test can tell "nothing was written" apart from "a 200 was written", which
// httptest.ResponseRecorder's own Code field cannot distinguish (it defaults
// to 200 on its own, independent of anything httpx does).
type specTrackedRecorder struct {
	*httptest.ResponseRecorder
	wroteHeader bool
	wroteBody   bool
}

func specNewTrackedRecorder() *specTrackedRecorder {
	return &specTrackedRecorder{ResponseRecorder: httptest.NewRecorder()}
}

func (t *specTrackedRecorder) WriteHeader(code int) {
	t.wroteHeader = true
	t.ResponseRecorder.WriteHeader(code)
}

func (t *specTrackedRecorder) Write(b []byte) (int, error) {
	t.wroteBody = true
	return t.ResponseRecorder.Write(b)
}

func specMarkMiddleware(name string, order *[]string) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*order = append(*order, name+":before")
			next.ServeHTTP(w, r)
			*order = append(*order, name+":after")
		})
	}
}

// ---------------------------------------------------------------------------
// ClientIP / TrustedProxies
// ---------------------------------------------------------------------------

func TestSpecClientIPZeroTrustedProxiesIgnoresForgedHeader(t *testing.T) {
	r := specNewRequest()
	r.RemoteAddr = "203.0.113.9:54321"
	r.Header.Set("X-Forwarded-For", "6.6.6.6")

	got := httpx.ClientIP(r, nil)
	want := netip.MustParseAddr("203.0.113.9")

	if got != want {
		t.Fatalf("the zero value of TrustedProxies must trust nothing, so a forged header must change nothing: got %v, want peer %v", got, want)
	}
}

func TestSpecClientIPWalksRightToLeftStoppingAtFirstUntrustedHop(t *testing.T) {
	trusted, err := httpx.ParseTrustedProxies([]string{"10.0.0.0/24"})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}

	r := specNewRequest()
	r.RemoteAddr = "10.0.0.1:1111"
	// Leftmost is the original client's own claim; rightmost is the hop
	// closest to us. The walk goes right to left from the peer inward.
	r.Header.Set("X-Forwarded-For", "8.8.8.8, 10.0.0.2")

	got := httpx.ClientIP(r, trusted)
	want := netip.MustParseAddr("8.8.8.8")

	if got != want {
		t.Fatalf("the walk must stop at the first untrusted hop counting from the peer inward: got %v, want %v", got, want)
	}
}

func TestSpecClientIPMalformedHopDoesNotPanic(t *testing.T) {
	// INFERENCE: the spec does not say what a malformed hop resolves to,
	// only that trust decisions must fail closed. We only assert the walk
	// survives and still produces a valid address; we do not assert which
	// one, since that would be guessing at unstated behaviour.
	trusted, err := httpx.ParseTrustedProxies([]string{"10.0.0.0/24"})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}

	r := specNewRequest()
	r.RemoteAddr = "10.0.0.1:1111"
	r.Header.Set("X-Forwarded-For", "not-an-ip, 10.0.0.2")

	var got netip.Addr
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("a caller-controlled header full of garbage must not crash the walk: %v", p)
			}
		}()
		got = httpx.ClientIP(r, trusted)
	}()

	if !got.IsValid() {
		t.Fatalf("a malformed hop must still resolve to some valid address, not a zero value silently treated as safe")
	}
}

func TestSpecClientIPMultipleForwardedForHeaders(t *testing.T) {
	// INFERENCE: the spec flags "several X-Forwarded-For headers" as
	// noteworthy but does not state whether repeated header lines are
	// merged or whether only the first is honoured. Both plausible
	// readings converge on the same answer for this fixture, so the
	// assertion holds regardless of which the implementation chose.
	trusted, err := httpx.ParseTrustedProxies([]string{"10.0.0.0/24"})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}

	r := specNewRequest()
	r.RemoteAddr = "10.0.0.1:1111"
	r.Header.Add("X-Forwarded-For", "8.8.8.8")
	r.Header.Add("X-Forwarded-For", "10.0.0.2")

	got := httpx.ClientIP(r, trusted)
	want := netip.MustParseAddr("8.8.8.8")
	if got != want {
		t.Fatalf("repeated X-Forwarded-For headers must not let a forged value win: got %v, want %v", got, want)
	}
}

func TestSpecParseTrustedProxiesBareAddressMatchesOnlyItself(t *testing.T) {
	tp, err := httpx.ParseTrustedProxies([]string{"192.168.1.1"})
	if err != nil {
		t.Fatalf("a bare address must be a valid trusted-proxy entry: %v", err)
	}
	if len(tp) != 1 {
		t.Fatalf("expected exactly one prefix, got %d", len(tp))
	}
	if !tp[0].Contains(netip.MustParseAddr("192.168.1.1")) {
		t.Fatalf("a bare address must trust the address it names")
	}
	if tp[0].Contains(netip.MustParseAddr("192.168.1.2")) {
		t.Fatalf("a bare address must not widen to trust its neighbors")
	}
}

func TestSpecParseTrustedProxiesCIDRPreservesBlock(t *testing.T) {
	tp, err := httpx.ParseTrustedProxies([]string{"10.0.0.0/24"})
	if err != nil {
		t.Fatalf("a CIDR block must be a valid trusted-proxy entry: %v", err)
	}
	if !tp[0].Contains(netip.MustParseAddr("10.0.0.5")) {
		t.Fatalf("a CIDR block must trust addresses inside it")
	}
	if tp[0].Contains(netip.MustParseAddr("10.0.1.5")) {
		t.Fatalf("a CIDR block must not trust addresses outside it")
	}
}

func TestSpecParseTrustedProxiesNonsenseErrors(t *testing.T) {
	if _, err := httpx.ParseTrustedProxies([]string{"this is not an address or a cidr"}); err == nil {
		t.Fatalf("nonsense input must be rejected with an error, not silently accepted as trusting nothing or everything")
	}
}

func TestSpecParseTrustedProxiesEmptyListTrustsNothing(t *testing.T) {
	tp, err := httpx.ParseTrustedProxies(nil)
	if err != nil {
		t.Fatalf("an empty list is not an error: %v", err)
	}
	if len(tp) != 0 {
		t.Fatalf("an empty list must produce a trusted-proxy set that trusts nothing, got %d entries", len(tp))
	}
}

// ---------------------------------------------------------------------------
// Status
// ---------------------------------------------------------------------------

func TestSpecStatusTotalOverAllKinds(t *testing.T) {
	for _, k := range errors.Kinds {
		k := k
		t.Run(k.String(), func(t *testing.T) {
			got := httpx.Status(k)
			if got < 100 || got > 599 {
				t.Fatalf("Status must map every errors.Kind to a valid HTTP status; got %d for %s", got, k)
			}
		})
	}
}

func TestSpecStatusDefaultsTo500ForUnmappedKind(t *testing.T) {
	unmapped := errors.Kind(255)
	for _, k := range errors.Kinds {
		if k == unmapped {
			t.Fatalf("fixture collides with a real Kind, pick a different sentinel")
		}
	}
	if got := httpx.Status(unmapped); got != 500 {
		t.Fatalf("a gap in the kind-to-status table is unknown, not the caller's fault; want 500, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// WriteError
// ---------------------------------------------------------------------------

func TestSpecWriteErrorNeverLeaksInternalMessage(t *testing.T) {
	secret := "conn=postgres://user:hunter2@db.internal/prod"
	err := errors.New(errors.Internal, secret)

	rec := httptest.NewRecorder()
	httpx.WriteError(rec, specNewRequest(), err)

	body := rec.Body.String()
	if strings.Contains(body, secret) {
		t.Fatalf("an internal error's real message must never reach the response body, got: %s", body)
	}

	safe := errors.Message(err)
	if !strings.Contains(body, safe) {
		t.Fatalf("the response must carry the caller-safe message %q, got: %s", safe, body)
	}

	if rec.Code != httpx.Status(errors.Internal) {
		t.Fatalf("WriteError must answer with Status(err's kind): got %d, want %d", rec.Code, httpx.Status(errors.Internal))
	}
}

func TestSpecWriteErrorIncludesCallerSafeFieldsButNeverDetails(t *testing.T) {
	err := errors.New(errors.Unprocessable, "validation failed").
		WithField("email", "must be present").
		WithDetail("trace", "internal-secret-xyz")

	rec := httptest.NewRecorder()
	httpx.WriteError(rec, specNewRequest(), err)
	body := rec.Body.String()

	if !strings.Contains(body, "must be present") {
		t.Fatalf("FieldsOf is caller-safe by contract and must be reachable in the response, got: %s", body)
	}
	if strings.Contains(body, "internal-secret-xyz") {
		t.Fatalf("DetailsOf is never reachable from the edge; it leaked into the response: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Fail
// ---------------------------------------------------------------------------

func TestSpecFailLogsDiagnosticsButBodyOmitsThem(t *testing.T) {
	secretDetail := "leaked-if-in-body"
	err := errors.New(errors.Internal, "db exploded").WithDetail("dsn", secretDetail)

	log, buf := specCaptureLogger()
	rec := httptest.NewRecorder()
	httpx.Fail(log, rec, specNewRequest(), err)

	if strings.Contains(rec.Body.String(), secretDetail) {
		t.Fatalf("Fail's response must not carry a diagnostic detail meant only for logs")
	}
	if !strings.Contains(buf.String(), secretDetail) {
		t.Fatalf("Fail must log the whole chain, including diagnostics the caller never sees")
	}

	safe := errors.Message(err)
	if !strings.Contains(rec.Body.String(), safe) {
		t.Fatalf("Fail must answer with the caller-safe message, got: %s", rec.Body.String())
	}

	lines := specLogLines(buf)
	if len(lines) != 1 {
		t.Fatalf("Fail logs the chain once, not once per something else: got %d log lines", len(lines))
	}
}

// ---------------------------------------------------------------------------
// Recovery
// ---------------------------------------------------------------------------

func TestSpecRecoveryRepanicsErrAbortHandler(t *testing.T) {
	rec := specNewTrackedRecorder()
	h := httpx.Chain(specPanicHandler(http.ErrAbortHandler), httpx.WithRecovery(logger.Nop()))

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		h.ServeHTTP(rec, specNewRequest())
	}()

	if recovered != http.ErrAbortHandler {
		t.Fatalf("http.ErrAbortHandler must be re-panicked untouched, not swallowed into a 500; recovered: %v", recovered)
	}
	if rec.wroteHeader || rec.wroteBody {
		t.Fatalf("a deliberate abort must never be converted into a response")
	}
}

func TestSpecRecoveryAnswers500WithoutLeakingPanicValue(t *testing.T) {
	secret := "panic: dial tcp 10.1.1.1:5432: bad-creds"
	log, buf := specCaptureLogger()
	rec := httptest.NewRecorder()
	h := httpx.Chain(specPanicHandler(secret), httpx.WithRecovery(log))

	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("an ordinary panic must be recovered, not propagated: %v", p)
			}
		}()
		h.ServeHTTP(rec, specNewRequest())
	}()

	if rec.Code != 500 {
		t.Fatalf("a recovered panic must answer 500, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("a panic value routinely contains what broke; it must never reach the response body")
	}
	if !strings.Contains(buf.String(), secret) {
		t.Fatalf("the panic must still be logged even though it is never returned to the caller")
	}
}

func TestSpecRecoveryDoesNotOverwriteAnAlreadyWrittenResponse(t *testing.T) {
	h := httpx.Chain(
		specWriteThenPanicHandler(201, "partial-ok", "boom-after-write"),
		httpx.WithRecovery(logger.Nop()),
	)
	rec := httptest.NewRecorder()

	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("recovery must contain an ordinary panic even after a partial write: %v", p)
			}
		}()
		h.ServeHTTP(rec, specNewRequest())
	}()

	if rec.Code != 201 {
		t.Fatalf("half a response is worse than an unexplained one; recovery must not change a status already sent: got %d", rec.Code)
	}
	if rec.Body.String() != "partial-ok" {
		t.Fatalf("recovery must not write over or append to a body the handler already sent, got: %q", rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// The recorder: default status, Flush, Unwrap; WithLogging's quiet paths
// ---------------------------------------------------------------------------

func TestSpecWithLoggingDefaultsUnwrittenStatusTo200(t *testing.T) {
	// INFERENCE: the exact JSON field key the status is logged under is not
	// given by the spec (only that "a log line matches what the client
	// saw"), so this checks by substring rather than by an assumed key.
	log, buf := specCaptureLogger()

	h1 := httpx.Chain(specStaticHandler(0, "hi"), httpx.WithLogging(log))
	h1.ServeHTTP(httptest.NewRecorder(), specNewRequest())
	lineImplicit := buf.String()
	buf.Reset()

	h2 := httpx.Chain(specStaticHandler(201, "hi"), httpx.WithLogging(log))
	h2.ServeHTTP(httptest.NewRecorder(), specNewRequest())
	lineExplicit := buf.String()

	if !strings.Contains(lineImplicit, "200") {
		t.Fatalf("a handler that writes a body without calling WriteHeader has sent a 200; the log line must say the same, got: %s", lineImplicit)
	}
	if !strings.Contains(lineExplicit, "201") {
		t.Fatalf("an explicit status must be reflected in the log line, got: %s", lineExplicit)
	}
}

func TestSpecWithLoggingSkipsQuietPaths(t *testing.T) {
	log, buf := specCaptureLogger()
	h := httpx.Chain(specStaticHandler(200, "ok"), httpx.WithLogging(log, "/healthz"))

	h.ServeHTTP(httptest.NewRecorder(), specNewRequestWithPath("GET", "/healthz"))
	if buf.Len() != 0 {
		t.Fatalf("a quiet path must not produce a log line, got: %s", buf.String())
	}

	h.ServeHTTP(httptest.NewRecorder(), specNewRequestWithPath("GET", "/other"))
	if len(specLogLines(buf)) != 1 {
		t.Fatalf("a non-quiet path must produce exactly one log line, got %d", len(specLogLines(buf)))
	}
}

func TestSpecFlushPassesThrough(t *testing.T) {
	rec := httptest.NewRecorder()
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("chunk"))
		f, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("the wrapped ResponseWriter must implement http.Flusher")
			return
		}
		f.Flush()
		called = true
	})

	h := httpx.Chain(inner, httpx.WithLogging(logger.Nop()), httpx.WithRecovery(logger.Nop()))
	h.ServeHTTP(rec, specNewRequest())

	if !called {
		t.Fatalf("Flush was never reached")
	}
	if !rec.Flushed {
		t.Fatalf("Flush must pass through to the underlying ResponseWriter; a wrapper that silently breaks streaming is discovered late")
	}
}

func TestSpecResponseControllerUnwrapsToRealConnection(t *testing.T) {
	done := make(chan error, 1)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		done <- rc.SetReadDeadline(time.Now().Add(time.Second))
		w.WriteHeader(200)
	})

	h := httpx.Chain(inner, httpx.WithLogging(logger.Nop()))
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if err := <-done; err != nil {
		t.Fatalf("http.ResponseController must reach the underlying connection through Unwrap: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Chain
// ---------------------------------------------------------------------------

func TestSpecChainNestsMiddlewareLikeAStack(t *testing.T) {
	var order []string
	h := httpx.Chain(
		specStaticHandler(200, "ok"),
		specMarkMiddleware("A", &order),
		specMarkMiddleware("B", &order),
		specMarkMiddleware("C", &order),
	)
	h.ServeHTTP(httptest.NewRecorder(), specNewRequest())

	var befores, afters []string
	for _, e := range order {
		if strings.HasSuffix(e, ":before") {
			befores = append(befores, strings.TrimSuffix(e, ":before"))
		} else {
			afters = append(afters, strings.TrimSuffix(e, ":after"))
		}
	}
	if len(befores) != len(afters) {
		t.Fatalf("every middleware entered must also exit: befores=%v afters=%v", befores, afters)
	}
	for i := range befores {
		if befores[i] != afters[len(afters)-1-i] {
			t.Fatalf("Chain must nest middleware like a stack, so whichever runs first finishes last: befores=%v afters=%v", befores, afters)
		}
	}
}

func TestSpecChainRunsEveryMiddlewareExactlyOnce(t *testing.T) {
	var order []string
	h := httpx.Chain(
		specStaticHandler(200, "ok"),
		specMarkMiddleware("A", &order),
		specMarkMiddleware("B", &order),
		specMarkMiddleware("C", &order),
	)
	h.ServeHTTP(httptest.NewRecorder(), specNewRequest())

	counts := map[string]int{}
	for _, e := range order {
		name := strings.SplitN(e, ":", 2)[0]
		counts[name]++
	}
	for _, name := range []string{"A", "B", "C"} {
		if counts[name] != 2 {
			t.Fatalf("middleware %s must run exactly once (before+after = 2 entries), got %d", name, counts[name])
		}
	}
}

func TestSpecChainWithNoMiddlewareInvokesHandlerDirectly(t *testing.T) {
	called := false
	h := httpx.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	h.ServeHTTP(httptest.NewRecorder(), specNewRequest())

	if !called {
		t.Fatalf("Chain with no middleware must still invoke the handler")
	}
}

// ---------------------------------------------------------------------------
// Provenance: minting, adoption, echoing, Identifier
// ---------------------------------------------------------------------------

func TestSpecProvenanceMintedFreshPerRequest(t *testing.T) {
	minter := id.NewSequence(time.Now())
	var seen []string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := provenance.Current(r.Context())
		if !ok {
			t.Fatalf("WithProvenance must place a Provenance in the request context")
		}
		seen = append(seen, p.Request().String())
		if p.Origin() != provenance.OriginRequest {
			t.Fatalf("a request arriving with no headers must originate as provenance.OriginRequest, got %s", p.Origin())
		}
		if !p.Root() {
			t.Fatalf("a request with nothing to adopt must be a root of its own chain")
		}
	})
	h := httpx.Chain(inner, httpx.WithProvenance(minter, nil))

	h.ServeHTTP(httptest.NewRecorder(), specNewRequest())
	h.ServeHTTP(httptest.NewRecorder(), specNewRequest())

	if len(seen) != 2 || seen[0] == seen[1] {
		t.Fatalf("a request identifier must be minted per request, not reused: %v", seen)
	}
}

func TestSpecProvenanceNilIdentifierMeansAnonymous(t *testing.T) {
	var gotKind provenance.Kind
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := provenance.Current(r.Context())
		gotKind = p.Actor().Kind()
	})
	h := httpx.Chain(inner, httpx.WithProvenance(id.NewSequence(time.Now()), nil))
	h.ServeHTTP(httptest.NewRecorder(), specNewRequest())

	if gotKind != provenance.KindAnonymous {
		t.Fatalf("a nil Identifier must mean anonymous, got kind %s", gotKind)
	}
}

func TestSpecIdentifierInvokedPerRequestAndAttachesBeforeHandlerRuns(t *testing.T) {
	invoked := false
	who := func(r *http.Request) provenance.Actor {
		invoked = true
		return provenance.Anonymous()
	}
	var gotKind provenance.Kind
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := provenance.Current(r.Context())
		gotKind = p.Actor().Kind()
	})
	h := httpx.Chain(inner, httpx.WithProvenance(id.NewSequence(time.Now()), who))
	h.ServeHTTP(httptest.NewRecorder(), specNewRequest())

	if !invoked {
		t.Fatalf("the Identifier must be consulted at the point provenance is minted, not left unused")
	}
	if gotKind != provenance.KindAnonymous {
		t.Fatalf("the actor returned by Identifier must reach the provenance seen by the handler")
	}
}

func TestSpecProvenanceEchoesOnlyRequestAndCorrelation(t *testing.T) {
	h := httpx.Chain(specStaticHandler(200, "ok"), httpx.WithProvenance(id.NewSequence(time.Now()), nil))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, specNewRequest())

	reqID := rec.Header().Get(httpx.HeaderRequestID)
	if reqID == "" {
		t.Fatalf("the request id must be echoed so a caller can quote it in a support ticket")
	}
	if _, err := id.Parse(reqID); err != nil {
		t.Fatalf("the echoed request id must be a well-formed identifier: %v", err)
	}
	if rec.Header().Get(httpx.HeaderCorrelationID) == "" {
		t.Fatalf("the correlation id must be echoed")
	}
	if rec.Header().Get(httpx.HeaderCausationID) != "" {
		t.Fatalf("causation is an internal edge; telling a caller what caused their own request is noise, and it must not be echoed")
	}
}

func TestSpecProvenanceAdoptsCallerCorrelationAndTraceparent(t *testing.T) {
	callerCorrelation := id.NewSequence(time.Now().Add(time.Hour)).NewID().String()
	tp := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	req := specNewRequest()
	req.Header.Set(httpx.HeaderCorrelationID, callerCorrelation)
	req.Header.Set(httpx.HeaderTraceparent, tp)

	var gotCorrelation, gotTrace string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := provenance.Current(r.Context())
		gotCorrelation = p.Correlation().String()
		gotTrace = p.Traceparent()
	})
	h := httpx.Chain(inner, httpx.WithProvenance(id.NewSequence(time.Now()), nil))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if gotCorrelation != callerCorrelation {
		t.Fatalf("a request arriving from another system must continue that system's correlation chain: got %s, want %s", gotCorrelation, callerCorrelation)
	}
	if gotTrace != tp {
		t.Fatalf("traceparent must be carried verbatim: got %q, want %q", gotTrace, tp)
	}
	if rec.Header().Get(httpx.HeaderCorrelationID) != callerCorrelation {
		t.Fatalf("the adopted correlation id must be echoed back unchanged")
	}
}

func TestSpecProvenanceAdoptionIsLenientOnMalformedCorrelation(t *testing.T) {
	garbage := "not-a-valid-id-####"
	req := specNewRequest()
	req.Header.Set(httpx.HeaderCorrelationID, garbage)

	h := httpx.Chain(specStaticHandler(200, "ok"), httpx.WithProvenance(id.NewSequence(time.Now()), nil))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("a broken trace link must be a cheaper failure than a rejected request: got status %d", rec.Code)
	}
	if rec.Header().Get(httpx.HeaderCorrelationID) == garbage {
		t.Fatalf("adoption drops what it cannot parse rather than passing it through verbatim")
	}
}

// ---------------------------------------------------------------------------
// WriteJSON
// ---------------------------------------------------------------------------

func TestSpecWriteJSONRoundTripsBodyAndSetsStatus(t *testing.T) {
	type payload struct {
		OK string `json:"ok"`
	}
	rec := httptest.NewRecorder()
	httpx.WriteJSON(rec, specNewRequest(), 201, payload{OK: "yes"})

	if rec.Code != 201 {
		t.Fatalf("WriteJSON must answer with the given status, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("WriteJSON must declare its content type as JSON, got %q", ct)
	}

	var got payload
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body must be valid JSON: %v", err)
	}
	if got.OK != "yes" {
		t.Fatalf("decode(encode(x)) must equal x: got %+v", got)
	}
}
