package root

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/0xsj/overwatch-backend/internal/identity/app/command"
)

// The first run, end to end: register, receive the link, verify, sign in, and
// ask where you may go. Every hop is the real handler over the real database,
// and the only fake in the system is the mailbox.
//
// It is one test rather than five because the thing worth asserting is the
// SEQUENCE. Each step passing in isolation is consistent with a product nobody
// can actually get into — which is the state this build was in on 2026-09-07,
// when sign-in worked and the client was still calling an endpoint that had
// been renamed.

func (s traced) get(t *testing.T, path string, headers map[string]string) *http.Response {
	t.Helper()
	return s.do(t, http.MethodGet, path, "", headers)
}

// token pulls the verification token out of the last mail. It parses the link
// rather than matching the body text, so a reworded email does not break the
// test and a link pointing at the wrong host does.
func (s traced) token(t *testing.T, wantPath string) string {
	t.Helper()
	msg, ok := s.sent.Last()
	if !ok {
		t.Fatal("no mail was sent")
	}
	return linkToken(t, msg.Body, wantPath)
}

// tokenTo finds the link in the message sent to ONE address. It exists because
// an email change sends two messages and only one of them carries a link — the
// notice to the old address deliberately does not (decisions/0021) — so "the
// last message" is the wrong question to ask.
func (s traced) tokenTo(t *testing.T, address, wantPath string) string {
	t.Helper()
	for _, msg := range s.sent.Sent() {
		if msg.To == address {
			return linkToken(t, msg.Body, wantPath)
		}
	}
	t.Fatalf("no mail was sent to %s", address)
	return ""
}

func linkToken(t *testing.T, body, wantPath string) string {
	t.Helper()
	for _, field := range strings.Fields(body) {
		if !strings.HasPrefix(field, "http") {
			continue
		}
		u, err := url.Parse(field)
		if err != nil {
			continue
		}
		if u.Path != wantPath {
			t.Fatalf("the link points at %q, not %q", u.Path, wantPath)
		}
		if u.Host != "localhost:7010" {
			t.Fatalf("the link points at %q, which is not the client", u.Host)
		}
		if got := u.Query().Get("token"); got != "" {
			return got
		}
	}
	t.Fatalf("no link with a token in:\n%s", body)
	return ""
}

func TestAPersonCanRegisterVerifySignInAndSeeWhereTheyMayGo(t *testing.T) {
	s := tracedSystem(t)
	ctx := context.Background()

	// ── register ────────────────────────────────────────────────────────
	res := s.register(t, "sam@example.com", nil)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d", res.StatusCode)
	}
	var created struct {
		AccountID string `json:"account_id"`
		Status    string `json:"status"`
	}
	decode(t, res, &created)
	if created.Status != "pending" {
		t.Fatalf("a new account is %q, not pending", created.Status)
	}

	// Nothing has been mailed yet, and nothing has been provisioned. The whole
	// of the rest of registration is an event sitting in the outbox.
	if _, sent := s.sent.Last(); sent {
		t.Fatal("mail was sent from inside the registration transaction")
	}

	s.drain(t)

	// ── the chain ran ───────────────────────────────────────────────────
	if n := count(t, s.pool, `select count(*) from org.org`); n != 1 {
		t.Fatalf("orgs after the chain: %d", n)
	}
	if n := count(t, s.pool, `select count(*) from workspace.workspace`); n != 1 {
		t.Fatalf("workspaces after the chain: %d", n)
	}

	// ── sign in BEFORE verifying — decisions/0018 ───────────────────────
	// A pending account authenticates. It is refused at the capability gate,
	// not at the door, and this is the assertion that keeps that true.
	body := `{"email":"sam@example.com","password":"a passphrase nobody guesses"}`
	// 201 and not 200: POST /v1/sessions CREATES a session, the same way
	// POST /v1/accounts creates an account.
	res = s.post(t, "/v1/sessions", body, nil)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("a pending account could not sign in: %d", res.StatusCode)
	}
	var signedIn struct {
		Token  string `json:"token"`
		Status string `json:"status"`
	}
	decode(t, res, &signedIn)
	if signedIn.Status != "pending" {
		t.Fatalf("signed in as %q, not pending", signedIn.Status)
	}
	auth := map[string]string{"authorization": "Bearer " + signedIn.Token}

	// ── /v1/me, unverified ──────────────────────────────────────────────
	var me meResponse
	decode(t, s.get(t, "/v1/me", auth), &me)
	if me.Verified {
		t.Fatal("an unverified account reports itself verified")
	}
	if len(me.Orgs) != 1 {
		t.Fatalf("orgs in /v1/me: %d", len(me.Orgs))
	}
	if me.Orgs[0].Role != "owner" {
		t.Fatalf("the person who registered is %q, not owner", me.Orgs[0].Role)
	}
	if len(me.Orgs[0].Workspaces) != 1 {
		t.Fatalf("workspaces in /v1/me: %d", len(me.Orgs[0].Workspaces))
	}

	// ── verify ──────────────────────────────────────────────────────────
	// The link was mailed by a SUBSCRIBER, which is why it only exists after
	// the drain above. It points at the client's /verify screen, not at this
	// server: a person clicks it in a browser.
	token := s.token(t, "/verify")

	res = s.post(t, "/v1/verifications/confirm", `{"token":`+quote(token)+`}`, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("verify: %d", res.StatusCode)
	}
	var verified struct {
		Status string `json:"status"`
	}
	decode(t, res, &verified)
	if verified.Status != "active" {
		t.Fatalf("after verifying, the account is %q", verified.Status)
	}

	// The same link a second time is refused. A verification link is single
	// use, and the row proving it is consumed rather than deleted.
	res = s.post(t, "/v1/verifications/confirm", `{"token":`+quote(token)+`}`, nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a spent link answered %d", res.StatusCode)
	}

	// ── the session minted before verification still works ──────────────
	// Verifying must not sign anybody out. It is the same person, and the
	// capability they gained is read on every request from the account.
	decode(t, s.get(t, "/v1/me", auth), &me)
	if !me.Verified {
		t.Fatal("the session predates verification and never noticed it")
	}

	s.drain(t)

	// ── audit and the journal recorded all of it ────────────────────────
	// The journal takes everything: it answers "what caused this", and a chain
	// missing its middle links answers nothing.
	for _, want := range []string{
		"identity.account.created",
		"org.created",
		"workspace.created",
		"identity.session.started",
		"identity.account.activated",
	} {
		if n := s.counted(t, `select count(*) from journal.line where action = $1`, want); n != 1 {
			t.Errorf("journal lines for %s: %d, want 1", want, n)
		}
	}

	// Audit takes the DECISIONS only — decisions/0014. Somebody chose to
	// register, chose to sign in, and proved an address; those are acts with an
	// author. Provisioning an org is WORK: it can fail, it was nobody's choice,
	// and an audit trail that lists it is listing the machinery rather than the
	// people.
	for _, want := range []string{
		"identity.account.created",
		"identity.session.started",
		"identity.account.activated",
	} {
		if n := s.counted(t, `select count(*) from audit.entry where action = $1`, want); n != 1 {
			t.Errorf("audit entries for %s: %d, want 1", want, n)
		}
	}
	for _, notThere := range []string{"org.created", "workspace.created"} {
		if n := s.counted(t, `select count(*) from audit.entry where action = $1`, notThere); n != 0 {
			t.Errorf("%s reached the audit trail: %d rows", notThere, n)
		}
	}

	// The activation is a DECISION and the session start is work — 0014. The
	// discriminator is whether it could have failed, and an account that is
	// already active does not fail to activate, it is already there.
	var decision bool
	if err := s.pool.DB(ctx).QueryRow(ctx,
		`select decision from journal.line where action = 'identity.account.activated'`,
	).Scan(&decision); err != nil {
		t.Fatal(err)
	}
	if !decision {
		t.Error("activation was recorded as work rather than as a decision")
	}

	// The activation names the person. It arrived on an UNAUTHENTICATED
	// request — a link carries no bearer token — so the actor comes from the
	// account the link proved, not from the middleware.
	var actor string
	if err := s.pool.DB(ctx).QueryRow(ctx,
		`select actor from audit.entry where action = 'identity.account.activated'`,
	).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(actor, created.AccountID) {
		t.Errorf("the activation is attributed to %q, not to the account it activated", actor)
	}
}

func TestAResetEndsEverySessionItFinds(t *testing.T) {
	s := tracedSystem(t)

	if res := s.register(t, "kit@example.com", nil); res.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d", res.StatusCode)
	}
	s.drain(t)

	body := `{"email":"kit@example.com","password":"a passphrase nobody guesses"}`
	var signedIn struct {
		Token string `json:"token"`
	}
	decode(t, s.post(t, "/v1/sessions", body, nil), &signedIn)
	auth := map[string]string{"authorization": "Bearer " + signedIn.Token}

	if res := s.get(t, "/v1/me", auth); res.StatusCode != http.StatusOK {
		t.Fatalf("the session does not work before the reset: %d", res.StatusCode)
	}

	if res := s.post(t, "/v1/password-resets", `{"email":"kit@example.com"}`, nil); res.StatusCode != http.StatusAccepted {
		t.Fatalf("request reset: %d", res.StatusCode)
	}
	token := s.token(t, "/reset")

	res := s.post(t, "/v1/password-resets/confirm",
		`{"token":`+quote(token)+`,"password":"an entirely different passphrase"}`, nil)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("confirm reset: %d", res.StatusCode)
	}

	// Whoever asked for the reset may not be whoever is signed in. Leaving the
	// session alive means a stolen one survives the recovery meant to stop it.
	if res := s.get(t, "/v1/me", auth); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the session survived the reset: %d", res.StatusCode)
	}

	// And the new password works.
	res = s.post(t, "/v1/sessions",
		`{"email":"kit@example.com","password":"an entirely different passphrase"}`, nil)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("the new password does not work: %d", res.StatusCode)
	}

	s.drain(t)

	// A session that stops working without a record is indistinguishable from
	// one somebody else revoked. The reset names EACH session it ended and why,
	// so "my session died" has an answer.
	if n := s.counted(t, `select count(*) from journal.line where action = $1`,
		"identity.session.ended"); n != 1 {
		t.Errorf("session.ended lines: %d, want 1 — the reset killed a session silently", n)
	}
	var reason string
	if err := s.pool.DB(context.Background()).QueryRow(context.Background(),
		`select detail->>'reason' from journal.line where action = 'identity.session.ended'`,
	).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason != command.ReasonPasswordReset {
		t.Errorf("the session ended for reason %q", reason)
	}
}

// TestAnUnknownAddressIsIndistinguishableFromAKnownOne is the whole reason
// these two endpoints answer 202 unconditionally. An endpoint that says "no
// such account" is a directory anybody can read one address at a time.
func TestAnUnknownAddressIsIndistinguishableFromAKnownOne(t *testing.T) {
	s := tracedSystem(t)
	if res := s.register(t, "kit@example.com", nil); res.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d", res.StatusCode)
	}
	s.drain(t)

	for _, address := range []string{"kit@example.com", "nobody@example.com", "not an address"} {
		res := s.post(t, "/v1/password-resets", `{"email":`+quote(address)+`}`, nil)
		if res.StatusCode != http.StatusAccepted {
			t.Errorf("%s: %d, want 202", address, res.StatusCode)
		}
	}
}

// counted is count with a parameter. quote() produces a JSON string, which
// Postgres reads as a double-quoted IDENTIFIER — the mistake this helper
// exists to stop repeating.
func (s traced) counted(t *testing.T, q string, args ...any) int {
	t.Helper()
	ctx := context.Background()
	var n int
	if err := s.pool.DB(ctx).QueryRow(ctx, q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func quote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
