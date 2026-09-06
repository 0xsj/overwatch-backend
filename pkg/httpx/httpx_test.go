package httpx_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/logger"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

func minter() provenance.Minter {
	return id.NewSequence(time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC))
}

func serve(t *testing.T, h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// ── the client address, which is the security-relevant half ────────────

func TestTheZeroTrustedSetBelievesNoHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.9:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := httpx.ClientIP(r, nil); got.String() != "203.0.113.9" {
		t.Fatalf("got %v; with no trusted proxies the header is pure caller input and the peer wins", got)
	}
}

func TestAnUntrustedPeerIsNotBelieved(t *testing.T) {
	trusted, err := httpx.ParseTrustedProxies([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.9:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := httpx.ClientIP(r, trusted); got.String() != "203.0.113.9" {
		t.Fatalf("got %v; a peer outside the trusted set cannot vouch for a header", got)
	}
}

func TestTheWalkGoesRightToLeftAndAForgedPrefixCannotWin(t *testing.T) {
	trusted, err := httpx.ParseTrustedProxies([]string{"10.0.0.0/8", "192.168.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:9999"
	// The caller wrote the first two entries themselves. Only the rightmost
	// hops, added by proxies we trust, mean anything.
	r.Header.Add("X-Forwarded-For", "9.9.9.9, 8.8.8.8")
	r.Header.Add("X-Forwarded-For", "198.51.100.7, 192.168.1.1")
	if got := httpx.ClientIP(r, trusted); got.String() != "198.51.100.7" {
		t.Fatalf("got %v, want 198.51.100.7 — the walk stops at the first hop that is not trusted", got)
	}
}

func TestAMalformedHopStopsTheWalk(t *testing.T) {
	trusted, _ := httpx.ParseTrustedProxies([]string{"10.0.0.0/8"})
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:9999"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, not-an-ip")
	if got := httpx.ClientIP(r, trusted); got.String() != "10.0.0.1" {
		t.Fatalf("got %v; skipping an unreadable hop lets a caller hide the hop before it behind it", got)
	}
}

func TestParseTrustedProxiesTakesBareAddressesAndRefusesNonsense(t *testing.T) {
	got, err := httpx.ParseTrustedProxies([]string{"10.0.0.1", " ", "192.168.0.0/16"})
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v; a bare address is a /32 and blanks are skipped", got, err)
	}
	if _, err := httpx.ParseTrustedProxies([]string{"nope"}); !errors.IsKind(err, errors.Invalid) {
		t.Fatalf("got %v; a bad CIDR is operator input and Invalid is the honest kind", err)
	}
}

func TestAPortOnAHopIsTolerated(t *testing.T) {
	trusted, _ := httpx.ParseTrustedProxies([]string{"10.0.0.0/8"})
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:9999"
	r.Header.Set("X-Forwarded-For", "1.2.3.4:5555")
	if got := httpx.ClientIP(r, trusted); got.String() != "1.2.3.4" {
		t.Fatalf("got %v; proxies do write host:port and refusing it drops the client", got)
	}
}

// ── provenance ─────────────────────────────────────────────────────────

func TestEveryRequestGetsAChainAndTheIdentifiersComeBackOut(t *testing.T) {
	var seen provenance.Provenance
	h := httpx.WithProvenance(minter(), nil)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			seen, _ = provenance.Current(r.Context())
		}))

	w := serve(t, h, httptest.NewRequest("GET", "/", nil))
	if seen.IsZero() {
		t.Fatal("no provenance reached the handler")
	}
	if !seen.Root() || seen.Origin() != provenance.OriginRequest {
		t.Fatalf("a request from nowhere must be an origin: root=%v origin=%v", seen.Root(), seen.Origin())
	}
	if got := w.Header().Get(httpx.HeaderRequestID); got != seen.Request().String() {
		t.Errorf("response carried request id %q, handler saw %q", got, seen.Request())
	}
	if w.Header().Get(httpx.HeaderCausationID) != "" {
		t.Error("causation was echoed; it is an internal edge and telling a caller what caused their own request is noise")
	}
}

func TestACallersCorrelationIsAdoptedAndTheRequestIsNotARoot(t *testing.T) {
	// A DIFFERENT epoch. Two id.Sequence values seeded at the same instant mint
	// the same identifiers, so an "upstream" built from minter() would be the
	// very id the middleware is about to mint.
	upstream := id.NewSequence(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)).NewID()
	var seen provenance.Provenance
	h := httpx.WithProvenance(minter(), nil)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { seen, _ = provenance.Current(r.Context()) }))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set(httpx.HeaderCorrelationID, upstream.String())
	serve(t, h, r)

	if seen.Correlation() != upstream {
		t.Fatalf("correlation %v, want the caller's %v — one gap disconnects the whole graph", seen.Correlation(), upstream)
	}
	if seen.Root() {
		t.Error("still reports as a root: it opened this process's work, not the chain")
	}
}

func TestAnUnparseableCorrelationIsDroppedNotRefused(t *testing.T) {
	var status int
	h := httpx.WithProvenance(minter(), nil)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { status = http.StatusOK; w.WriteHeader(status) }))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set(httpx.HeaderCorrelationID, "../../etc/passwd")
	w := serve(t, h, r)
	if w.Code != http.StatusOK || status != http.StatusOK {
		t.Fatalf("status %d; nothing branches on provenance, so a broken trace link must not cost the caller their request", w.Code)
	}
}

// ── errors to statuses ─────────────────────────────────────────────────

func TestEveryKindHasAStatusAndUnmappedMeansFiveHundred(t *testing.T) {
	// AMENDED after review: this ranged over Kinds and asserted only 200-599,
	// so swapping the NotFound and Conflict arms passed it. A totality test
	// proves totality and reads in a listing as though it proved correctness.
	want := map[errors.Kind]int{
		errors.Internal:             http.StatusInternalServerError,
		errors.Invalid:              http.StatusBadRequest,
		errors.NotFound:             http.StatusNotFound,
		errors.Conflict:             http.StatusConflict,
		errors.Unauthenticated:      http.StatusUnauthorized,
		errors.Forbidden:            http.StatusForbidden,
		errors.RateLimited:          http.StatusTooManyRequests,
		errors.Unavailable:          http.StatusServiceUnavailable,
		errors.Timeout:              http.StatusGatewayTimeout,
		errors.Canceled:             httpx.StatusClientClosed,
		errors.Unprocessable:        http.StatusUnprocessableEntity,
		errors.PreconditionFailed:   http.StatusPreconditionFailed,
		errors.PreconditionRequired: http.StatusPreconditionRequired,
	}
	for _, k := range errors.Kinds {
		w, ok := want[k]
		if !ok {
			t.Errorf("%v is in errors.Kinds and has no expected status here — the table above is the assertion, and a new kind must land in it deliberately", k)
			continue
		}
		if got := httpx.Status(k); got != w {
			t.Errorf("Status(%v) = %d, want %d", k, got, w)
		}
	}
	if len(want) != len(errors.Kinds) {
		t.Errorf("this table has %d kinds and errors.Kinds has %d", len(want), len(errors.Kinds))
	}
	if got := httpx.Status(errors.Kind(200)); got != http.StatusInternalServerError {
		t.Errorf("an unmapped kind gave %d; a gap in the table means we do not know, which is ours and not the caller's", got)
	}
	if got := httpx.Status(errors.Internal); got != http.StatusInternalServerError {
		t.Errorf("Internal gave %d", got)
	}
}

func TestAnInternalErrorTellsTheCallerNothingButCarriesTheRequestID(t *testing.T) {
	boom := errors.Wrap(errors.New(errors.Internal, "dial tcp 10.0.0.5:5432: connection refused"),
		errors.Internal, "postgres://overwatch:hunter2@db/overwatch")

	h := httpx.WithProvenance(minter(), nil)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { httpx.WriteError(w, r, boom) }))
	w := serve(t, h, httptest.NewRequest("GET", "/", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	for _, leak := range []string{"hunter2", "10.0.0.5", "postgres://", "connection refused"} {
		if contains(body, leak) {
			t.Errorf("the body leaked %q: %s", leak, body)
		}
	}
	var p struct {
		Kind      string `json:"kind"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.RequestID == "" {
		t.Error("no request_id in the body; quoting one in a support ticket is what makes the chain reachable")
	}
	if p.Kind != "internal" {
		t.Errorf("kind %q", p.Kind)
	}
}

func TestACallerErrorKeepsItsFields(t *testing.T) {
	err := errors.New(errors.Invalid, "target is not a domain").WithField("target", "must be a hostname")
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { httpx.WriteError(w, r, err) })
	w := serve(t, h, httptest.NewRequest("POST", "/targets", nil))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d", w.Code)
	}
	var p struct {
		Fields map[string]string
	}
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if p.Fields["target"] == "" {
		t.Errorf("fields were dropped: %s — FieldsOf is caller-safe by contract and it is the half that makes a 400 actionable", w.Body)
	}
}

// ── recovery ───────────────────────────────────────────────────────────

func TestAPanicBecomesFiveHundredAndSaysNothing(t *testing.T) {
	h := httpx.WithRecovery(logger.Nop())(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { panic("postgres://overwatch:hunter2@db") }))
	w := serve(t, h, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", w.Code)
	}
	if contains(w.Body.String(), "hunter2") {
		t.Errorf("the panic value reached the caller: %s — a panic routinely contains the thing that broke", w.Body)
	}
}

func TestErrAbortHandlerIsRePanicked(t *testing.T) {
	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Fatalf("recovered %v; ErrAbortHandler is how net/http signals a deliberate abort, and swallowing it turns an intentional disconnect into an error page", v)
		}
	}()
	h := httpx.WithRecovery(logger.Nop())(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { panic(http.ErrAbortHandler) }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}

func TestAPanicAfterAWriteDoesNotOverwriteTheResponse(t *testing.T) {
	h := httpx.WithRecovery(logger.Nop())(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"ok":true}`))
			panic("too late")
		}))
	w := serve(t, h, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusCreated || w.Body.String() != `{"ok":true}` {
		t.Fatalf("got %d %q; half a response is worse than an unexplained one", w.Code, w.Body)
	}
}

func TestTheRecorderReportsTwoHundredForABodyWithNoWriteHeader(t *testing.T) {
	var status int
	// The probe goes AFTER WithRecovery in the chain, so the writer it sees is
	// the recorder that middleware installed rather than the bare one.
	h := httpx.Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("hi")) }),
		httpx.WithRecovery(logger.Nop()),
		func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r)
				rec, ok := w.(interface{ Status() int })
				if !ok {
					t.Fatalf("the writer reaching this middleware is a %T, not a status recorder", w)
				}
				status = rec.Status()
			})
		},
	)
	serve(t, h, httptest.NewRequest("GET", "/", nil))
	if status != http.StatusOK {
		t.Fatalf("recorder said %d; a handler that writes a body without WriteHeader has sent a 200 and the log must match what the client saw", status)
	}
}

func TestChainRunsMiddlewareInTheOrderGiven(t *testing.T) {
	var order []string
	mw := func(name string) httpx.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	h := httpx.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		order = append(order, "handler")
	}), mw("first"), mw("second"))
	serve(t, h, httptest.NewRequest("GET", "/", nil))

	want := []string{"first", "second", "handler"}
	for i := range want {
		if i >= len(order) || order[i] != want[i] {
			t.Fatalf("ran %v, want %v — provenance must wrap logging, so the order is the contract", order, want)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ── added after review: each of these was a real defect ────────────────

func TestANilLoggerFailsWhereItIsWiredNotInsideARecover(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func()
	}{
		{"WithRecovery", func() { httpx.WithRecovery(nil) }},
		{"WithLogging", func() { httpx.WithLogging(nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// AMENDED: a bare recover() cannot tell this guard from the nil
			// dereference it exists to prevent — which is the exact bug, so the
			// test passed with the guard deleted.
			defer func() {
				v := recover()
				if v == nil {
					t.Error("constructed cleanly: a nil logger then fails inside the deferred recover, which replaces the original panic with a nil dereference and writes no status")
					return
				}
				if s, ok := v.(string); !ok || !strings.Contains(s, "nil Logger") {
					t.Errorf("panicked with %v (%T); wanted the eager guard naming a nil Logger", v, v)
				}
			}()
			tc.call()
		})
	}
}

func TestAQuietPathIsQuietOnlyWhileItWorks(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(logger.Config{Output: &buf, Clock: clock.System{}, Level: slog.LevelInfo})

	ok := httpx.WithLogging(log, "/healthz")(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	serve(t, ok, httptest.NewRequest("GET", "/healthz", nil))
	if buf.Len() != 0 {
		t.Fatalf("a working quiet path was logged: %s", buf.String())
	}

	broken := httpx.WithLogging(log, "/healthz")(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	serve(t, broken, httptest.NewRequest("GET", "/healthz", nil))
	if buf.Len() == 0 {
		t.Fatal("a FAILING quiet path was silent; a health check that starts failing is the one line you most want")
	}
}

func TestAnUnencodableBodyIsFiveHundredAndNotAnEmptyTwoHundred(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A channel cannot be marshalled.
		httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"bad": make(chan int)})
	})
	w := serve(t, h, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d body %q; json.Encoder buffers, so writing the status first bought no streaming and cost the ability to report the failure at all", w.Code, w.Body)
	}
}

func TestFailLogsTheDiagnosticsNobodyMaySee(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(logger.Config{Output: &buf, Clock: clock.System{}, Level: slog.LevelInfo})

	err := errors.New(errors.Conflict, "already registered").
		WithDetail("constraint", "target_host_key").
		WithDetail("sqlstate", "23505")

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { httpx.Fail(log, w, r, err) })
	w := serve(t, h, httptest.NewRequest("POST", "/targets", nil))

	line := buf.String()
	for _, want := range []string{"target_host_key", "23505"} {
		if !contains(line, want) {
			t.Errorf("the log dropped %q: %s\npkg/postgres attaches these citing this reader; without it three files are each correct and meet nowhere", want, line)
		}
	}
	if contains(w.Body.String(), "target_host_key") {
		t.Errorf("a detail reached the caller: %s — DetailsOf is for logs and nothing that writes a response may read it", w.Body)
	}
}

func TestATypedErrorCarriesItsSlugToTheCaller(t *testing.T) {
	err := errors.New(errors.Conflict, "already registered").WithType("target-exists")
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { httpx.WriteError(w, r, err) })
	w := serve(t, h, httptest.NewRequest("POST", "/targets", nil))

	var p struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if p.Type != "target-exists" {
		t.Errorf("type = %q; the field was declared, tagged omitempty and never assigned, so errors.WithType reached nothing", p.Type)
	}
}

// ── middleware order, which fails by omission ──────────────────────────

func TestAPanickingIdentifierIsRecoveredAndNamed(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(logger.Config{Output: &buf, Clock: clock.System{}, Level: slog.LevelInfo})

	log = logger.New(logger.Config{Output: &buf, Clock: clock.System{},
		Level: slog.LevelInfo, Context: provenance.Attrs})

	boom := func(*http.Request) provenance.Actor { panic("the authenticator fell over") }
	h := httpx.Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("the handler ran") }),
		httpx.WithRecovery(log),
		httpx.WithProvenance(minter(), boom),
	)
	w := serve(t, h, httptest.NewRequest("GET", "/", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d; the Identifier is injected and does real work, so a panic there must not escape every recover and drop the connection", w.Code)
	}
	if id := w.Header().Get(httpx.HeaderRequestID); id != "" {
		t.Errorf("a request id was echoed for a request that never got past authentication: %q", id)
	}
	if !contains(buf.String(), "request_id=") {
		t.Errorf("the panic was logged without a request id: %s\nthe chain is installed before who() is called, in place, so the recovery above can name the request it died on", buf.String())
	}
}

func TestAPanickingHandlerIsStillNamedInTheRecoveryLine(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(logger.Config{Output: &buf, Clock: clock.System{}, Level: slog.LevelInfo})

	log = logger.New(logger.Config{Output: &buf, Clock: clock.System{},
		Level: slog.LevelInfo, Context: provenance.Attrs})

	h := httpx.Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("boom") }),
		httpx.WithRecovery(log),
		httpx.WithProvenance(minter(), nil),
	)
	w := serve(t, h, httptest.NewRequest("GET", "/", nil))

	want := w.Header().Get(httpx.HeaderRequestID)
	if want == "" {
		t.Fatal("no request id on the response")
	}
	if !contains(buf.String(), want) {
		t.Fatalf("the recovery line does not carry %s: %s\nr.WithContext returns a COPY, so an outer recover would otherwise hold a context predating the chain — on the one line that most needs it", want, buf.String())
	}
}

// MW-17, custody 0012: the one gap both suites shared. doc.go states "if the
// handler had already written a response before panicking, nothing is written
// over it" — and both suites tested it only through a handler that calls
// WriteHeader first. Deleting `r.wrote = true` from recorder.Write survived
// both, because WriteHeader sets the same flag.
//
// The uncovered case is the one doc.go singles out for the 200 default: a
// handler that writes a BODY with no explicit WriteHeader, then panics. Its
// half-response gets overwritten and nothing notices.
func TestAPanicAfterABodyWithNoWriteHeaderDoesNotOverwriteIt(t *testing.T) {
	h := httpx.WithRecovery(logger.Nop())(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"partial":true`)) // no WriteHeader, so this sent a 200
			panic("too late")
		}))
	w := serve(t, h, httptest.NewRequest("GET", "/", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status %d; the 200 was already on the wire before the panic and cannot be revised", w.Code)
	}
	if got := w.Body.String(); got != `{"partial":true` {
		t.Fatalf("body = %q; half a response is worse than an unexplained one, and appending to it makes it neither", got)
	}
}
