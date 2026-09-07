package root

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	identitycmd "github.com/0xsj/overwatch-backend/internal/identity/app/command"
	identityquery "github.com/0xsj/overwatch-backend/internal/identity/app/query"
	identitypg "github.com/0xsj/overwatch-backend/internal/identity/infra/postgres"
	identityhttp "github.com/0xsj/overwatch-backend/internal/identity/transport/http"
	journalapp "github.com/0xsj/overwatch-backend/internal/journal/app"
	journalpg "github.com/0xsj/overwatch-backend/internal/journal/infra/postgres"
	orgcmd "github.com/0xsj/overwatch-backend/internal/org/app/command"
	orgpg "github.com/0xsj/overwatch-backend/internal/org/infra/postgres"
	workspacecmd "github.com/0xsj/overwatch-backend/internal/workspace/app/command"
	workspacepg "github.com/0xsj/overwatch-backend/internal/workspace/infra/postgres"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/crypto"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/logger"
	"github.com/0xsj/overwatch-backend/pkg/outbox"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
	"github.com/0xsj/overwatch-backend/pkg/testx"
)

// traced is the whole system minus the listener: the real middleware, the real
// handler, the real chain, the real journal. Nothing here is a fake — the point
// is to assert that what pkg/provenance promises survives an HTTP request, a
// transaction, an outbox row, a dispatcher and four subscribers.
type traced struct {
	handler http.Handler
	pump    *outbox.Dispatcher
	pool    *postgres.Pool
}

func tracedSystem(t *testing.T) traced {
	t.Helper()
	p := testx.Postgres(t,
		testx.Schema{Name: "outbox", Migrations: outbox.Migrations, Unqualified: true},
		testx.Schema{Name: identitypg.Schema, Migrations: identitypg.Migrations},
		testx.Schema{Name: orgpg.Schema, Migrations: orgpg.Migrations},
		testx.Schema{Name: workspacepg.Schema, Migrations: workspacepg.Migrations},
		testx.Schema{Name: journalpg.Schema, Migrations: journalpg.Migrations},
	)

	clk := clock.System{}
	ids := id.NewV7(clk, rand.Reader)
	publisher := outbox.NewPublisher(outbox.NewPostgres(p))

	accounts := identitypg.NewStore(p)
	hasher := crypto.NewHasher(cheap, rand.Reader)
	auth := identitycmd.NewAuthenticator(accounts, publisher, hasher,
		crypto.NewMinter(rand.Reader), ids, clk, 0)
	sessions := identityquery.NewSessions(accounts, clk)

	mux := http.NewServeMux()
	identityhttp.NewAPI(
		identitycmd.NewRegistrar(accounts, p, publisher, hasher, ids, clk),
		auth, logger.Nop()).Routes(mux)

	return traced{
		// The real middleware with the real Identifier. A registration carries
		// no token, so its actor is anonymous — but a signed-in request's is
		// not, which is what TestASignedInRequestNamesThePerson asserts.
		handler: httpx.Chain(mux, httpx.WithProvenance(ids, identityhttp.Identifier(sessions))),
		pump: outbox.New(outbox.Config{
			Store: outbox.NewPostgres(p), Clock: clk, Log: logger.Nop(),
			Handlers: []events.Handler{
				orgcmd.NewSubscriber(orgcmd.NewService(orgpg.NewStore(p), publisher, ids, clk), ids).Handle,
				workspacecmd.NewSubscriber(workspacecmd.NewService(workspacepg.NewStore(p), publisher, ids, clk), ids).Handle,
				journalapp.NewSubscriber(journalpg.NewStore(p), ids, clk).Handle,
			},
		}),
		pool: p,
	}
}

func (s traced) register(t *testing.T, email string, headers map[string]string) *http.Response {
	t.Helper()
	body := `{"email":"` + email + `","password":"a passphrase nobody guesses","name":"Sam Lee"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/register", strings.NewReader(body))
	req.Header.Set("content-type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec.Result()
}

// drain runs the pump until the outbox is empty — the chain is three hops, and
// each hop's event is published by the hop before it.
func (s traced) post(t *testing.T, path, body string, headers map[string]string) *http.Response {
	t.Helper()
	return s.do(t, http.MethodPost, path, body, headers)
}

func (s traced) delete(t *testing.T, path string, headers map[string]string) *http.Response {
	t.Helper()
	return s.do(t, http.MethodDelete, path, "", headers)
}

func (s traced) do(t *testing.T, method, path, body string, headers map[string]string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("content-type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec.Result()
}

func decode(t *testing.T, res *http.Response, into any) {
	t.Helper()
	if err := json.NewDecoder(res.Body).Decode(into); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func (s traced) drain(t *testing.T) {
	t.Helper()
	if err := s.pump.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}
}

type line struct {
	Action      string
	EventID     string
	Correlation string
	Causation   string
	Depth       int
	Attempt     int
	Actor       string
	Origin      string
	Workspace   string
}

func (s traced) lines(t *testing.T) []line {
	t.Helper()
	ctx := context.Background()
	rows, err := s.pool.DB(ctx).Query(ctx, `
		select action, event_id::text, coalesce(correlation_id::text,''),
		       coalesce(causation_id::text,''), depth, attempt, actor, origin, workspace_id
		from journal.line order by occurred_at`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []line
	for rows.Next() {
		var l line
		if err := rows.Scan(&l.Action, &l.EventID, &l.Correlation, &l.Causation,
			&l.Depth, &l.Attempt, &l.Actor, &l.Origin, &l.Workspace); err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	return out
}

// ── the ledger, asserted end to end ────────────────────────────────────

func TestProvenanceSurvivesTheWholeChain(t *testing.T) {
	s := tracedSystem(t)

	res := s.register(t, "sam@example.com", nil)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d", res.StatusCode)
	}
	// The edge stamps both onto the response, which is what makes a chain
	// findable from a client's logs without access to the database.
	requestID := res.Header.Get(httpx.HeaderRequestID)
	correlation := res.Header.Get(httpx.HeaderCorrelationID)
	if requestID == "" || correlation == "" {
		t.Fatalf("headers: request=%q correlation=%q", requestID, correlation)
	}
	// At an ORIGIN the correlation IS the root's request id. No second
	// identifier is minted, so there is nothing to drift.
	if requestID != correlation {
		t.Errorf("at an origin correlation should equal request: %s vs %s", correlation, requestID)
	}

	s.drain(t)
	lines := s.lines(t)
	if len(lines) != 3 {
		t.Fatalf("journal holds %d lines, want 3", len(lines))
	}

	byAction := map[string]line{}
	for _, l := range lines {
		byAction[l.Action] = l
		if l.Correlation != correlation {
			t.Errorf("%s carries correlation %s, want the request's %s — the chain is broken at that hop",
				l.Action, l.Correlation, correlation)
		}
		if l.Attempt != 1 {
			t.Errorf("%s is attempt %d on a first delivery", l.Action, l.Attempt)
		}
		if l.Actor != "anonymous" {
			t.Errorf("%s names actor %q — a self-signup is unauthenticated", l.Action, l.Actor)
		}
		if l.Origin != "request" {
			t.Errorf("%s has origin %q; a subscriber inherits WHY the chain exists", l.Action, l.Origin)
		}
	}

	account := byAction["identity.account.created"]
	org := byAction["org.created"]
	workspace := byAction["workspace.created"]

	// Causation is the EDGE: each hop names the event that caused it, not the
	// request that emitted it. Correlation alone would not distinguish this
	// chain from three siblings.
	if account.Causation != "" {
		t.Errorf("the root has causation %s; nothing caused it", account.Causation)
	}
	if org.Causation != account.EventID {
		t.Errorf("org.created is caused by %s, want the account event %s", org.Causation, account.EventID)
	}
	if workspace.Causation != org.EventID {
		t.Errorf("workspace.created is caused by %s, want the org event %s", workspace.Causation, org.EventID)
	}

	for _, tc := range []struct {
		l    line
		want int
	}{{account, 0}, {org, 1}, {workspace, 2}} {
		if tc.l.Depth != tc.want {
			t.Errorf("%s is at depth %d, want %d", tc.l.Action, tc.l.Depth, tc.want)
		}
	}

	// Tenant is whose data it touches, and it cannot be known before the
	// workspace exists. It appears exactly where it becomes true.
	if account.Workspace != "" || org.Workspace != "" {
		t.Errorf("a tenant appeared before the workspace did: account=%q org=%q",
			account.Workspace, org.Workspace)
	}
	if workspace.Workspace == "" {
		t.Error("workspace.created carries no tenant, so audit cannot scope it")
	}
}

// Every unit of work gets its OWN request id. Reusing the parent's would make a
// fan-out indistinguishable from a retry, which is the pair Attempt exists to
// keep apart.
func TestEachHopMintsItsOwnRequestID(t *testing.T) {
	s := tracedSystem(t)
	ctx := context.Background()

	if _, err := s.pool.DB(ctx).Exec(ctx,
		"create temp table seen on commit preserve rows as select * from outbox with no data"); err != nil {
		t.Fatal(err)
	}
	if res := s.register(t, "sam@example.com", nil); res.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d", res.StatusCode)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.pool.DB(ctx).Exec(ctx, "insert into seen select * from outbox"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.pump.Dispatch(ctx); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := s.pool.DB(ctx).Query(ctx, "select distinct name, provenance from seen")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	requests := map[string]string{}
	for rows.Next() {
		var name string
		var raw []byte
		if err := rows.Scan(&name, &raw); err != nil {
			t.Fatal(err)
		}
		var p struct {
			Request string `json:"request_id"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		if p.Request == "" {
			t.Fatalf("%s carries no request id", name)
		}
		for other, seen := range requests {
			if seen == p.Request {
				t.Errorf("%s and %s share request %s — a hop reused its parent's", name, other, seen)
			}
		}
		requests[name] = p.Request
	}
	if len(requests) != 3 {
		t.Fatalf("saw %d events, want 3", len(requests))
	}
}

// The edge adopts an upstream correlation, and the whole chain inherits it. This
// is what makes a trace that starts in the client readable in the journal — and
// the depth-0 value is then NOT a root, because it opened this process's work
// rather than the chain.
func TestAnUpstreamCorrelationIsAdoptedAndCarriedToTheLastHop(t *testing.T) {
	s := tracedSystem(t)
	upstream := id.NewSequence(clock.System{}.Now()).NewID().String()

	res := s.register(t, "sam@example.com", map[string]string{
		httpx.HeaderCorrelationID: upstream,
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d", res.StatusCode)
	}
	if got := res.Header.Get(httpx.HeaderCorrelationID); got != upstream {
		t.Fatalf("the edge did not adopt: %s, want %s", got, upstream)
	}
	if res.Header.Get(httpx.HeaderRequestID) == upstream {
		t.Error("request was adopted too — request is minted here and never taken from a caller")
	}

	s.drain(t)
	for _, l := range s.lines(t) {
		if l.Correlation != upstream {
			t.Errorf("%s carries %s, want the upstream %s", l.Action, l.Correlation, upstream)
		}
	}
}

// Depth and attempt are the two fields something branches on, so neither is
// adoptable — a caller cannot reset a bound it is subject to. Adopted has no
// field for either, which is the compiler enforcing it; this asserts the edge
// does not smuggle them in some other way.
func TestDepthAndAttemptAreNotTakenFromTheCaller(t *testing.T) {
	s := tracedSystem(t)

	res := s.register(t, "sam@example.com", map[string]string{
		httpx.HeaderCorrelationID: id.NewSequence(clock.System{}.Now()).NewID().String(),
		"X-Depth":                 "31",
		"X-Attempt":               "9",
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d", res.StatusCode)
	}
	s.drain(t)
	for _, l := range s.lines(t) {
		if l.Attempt != 1 {
			t.Errorf("%s is attempt %d — a caller set it", l.Action, l.Attempt)
		}
	}
	lines := s.lines(t)
	if lines[0].Depth != 0 {
		t.Errorf("the root is at depth %d — a caller set it", lines[0].Depth)
	}
}

// The payoff, and the first time the record layer does what the product claims.
// Until an Identifier was wired, every audit entry and journal line in the
// system said `anonymous` — including for things a person plainly did.
func TestASignedInRequestNamesThePersonInTheRecord(t *testing.T) {
	s := tracedSystem(t)

	res := s.register(t, "sam@example.com", nil)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d", res.StatusCode)
	}
	var registered struct {
		AccountID string `json:"account_id"`
	}
	decode(t, res, &registered)

	// Signing in carries no token, so this one is anonymous too.
	signIn := s.post(t, "/v1/sessions",
		`{"email":"sam@example.com","password":"a passphrase nobody guesses"}`, nil)
	if signIn.StatusCode != http.StatusCreated {
		t.Fatalf("sign in: %d", signIn.StatusCode)
	}
	var session struct {
		Token  string `json:"token"`
		Status string `json:"status"`
	}
	decode(t, signIn, &session)
	if session.Token == "" {
		t.Fatal("no token")
	}
	// decisions/0018: a pending account signs in.
	if session.Status != "pending" {
		t.Errorf("status %q", session.Status)
	}

	s.drain(t)
	byAction := map[string]line{}
	for _, l := range s.lines(t) {
		byAction[l.Action] = l
	}

	started, ok := byAction["identity.session.started"]
	if !ok {
		t.Fatal("signing in produced no journal line")
	}
	want := "user:" + registered.AccountID
	if started.Actor != want {
		t.Errorf("actor is %q, want %q — the Identifier is not resolving the token", started.Actor, want)
	}
	// And a request made WITH the token names the person too.
	out := s.delete(t, "/v1/sessions/current", map[string]string{
		"Authorization": "Bearer " + session.Token,
	})
	if out.StatusCode != http.StatusNoContent {
		t.Fatalf("sign out: %d", out.StatusCode)
	}
	s.drain(t)
	for _, l := range s.lines(t) {
		if l.Action != "identity.session.ended" {
			continue
		}
		if l.Actor != want {
			t.Errorf("sign-out actor is %q, want %q", l.Actor, want)
		}
		return
	}
	t.Error("signing out produced no journal line")
}

// A bad token must not fail the request at the middleware. Provenance is
// metadata, never the thing that authorises — so it yields the anonymous actor
// and the handler refuses properly.
func TestABadTokenIsAnonymousRatherThanRejectedByTheMiddleware(t *testing.T) {
	s := tracedSystem(t)
	res := s.post(t, "/v1/sessions",
		`{"email":"nobody@example.com","password":"a passphrase nobody guesses"}`,
		map[string]string{"Authorization": "Bearer not-a-real-token"})

	// The handler's answer, not the middleware's: rejected credentials, not a
	// complaint about the header.
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("status %d, want 401 from the handler", res.StatusCode)
	}
	if res.Header.Get(httpx.HeaderRequestID) == "" {
		t.Error("the chain was not installed for a request with a bad token")
	}
}
