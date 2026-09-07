package root

import (
	"net/http"
	"strings"
	"testing"
)

const pass = "a passphrase nobody guesses"

// deviceResponse is declared here rather than imported: it is identity's wire
// shape, and a test that decodes the SERVER'S struct cannot notice a json tag
// that changed. Restating it is the assertion.
type deviceResponse struct {
	SessionID string `json:"session_id"`
	UserAgent string `json:"user_agent"`
	Address   string `json:"address"`
	IssuedAt  string `json:"issued_at"`
	ExpiresAt string `json:"expires_at"`
	Current   bool   `json:"current"`
}

// The whole account-settings surface, walked as a person would. decisions/0021.
func TestAccountSettings(t *testing.T) {
	s := tracedSystem(t)
	_, auth := s.signUp(t, "sam@example.com")

	t.Run("a name changes with no password", func(t *testing.T) {
		res := s.do(t, http.MethodPatch, "/v1/me", `{"name":"Samantha Lee"}`, auth)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("rename: %d", res.StatusCode)
		}
		var members []memberResponse
		org := s.me(t, auth).Orgs[0].OrgID
		decode(t, s.get(t, "/v1/orgs/"+org+"/members", auth), &members)
		if members[0].Name != "Samantha Lee" {
			t.Errorf("name is %q", members[0].Name)
		}
	})

	t.Run("a wrong current password is refused", func(t *testing.T) {
		res := s.post(t, "/v1/me/password",
			`{"current_password":"not it","password":"an entirely different passphrase"}`, auth)
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("wrong current password: %d, want 401", res.StatusCode)
		}
		// And the old password still works, so nothing was half-applied.
		if res := s.post(t, "/v1/sessions",
			`{"email":"sam@example.com","password":`+quote(pass)+`}`, nil); res.StatusCode != http.StatusCreated {
			t.Errorf("the password changed on a failed attempt: %d", res.StatusCode)
		}
	})
}

// A password change keeps the caller signed in and evicts everyone else —
// decisions/0021. Both halves, because each is a different mistake.
func TestChangingAPasswordKeepsThisSessionAndEndsTheOthers(t *testing.T) {
	s := tracedSystem(t)
	_, first := s.signUp(t, "sam@example.com")

	var second struct {
		Token string `json:"token"`
	}
	decode(t, s.post(t, "/v1/sessions",
		`{"email":"sam@example.com","password":`+quote(pass)+`}`, nil), &second)
	other := map[string]string{"authorization": "Bearer " + second.Token}

	if res := s.get(t, "/v1/me", other); res.StatusCode != http.StatusOK {
		t.Fatalf("the second session does not work: %d", res.StatusCode)
	}

	res := s.post(t, "/v1/me/password",
		`{"current_password":`+quote(pass)+`,"password":"an entirely different passphrase"}`, first)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("change password: %d", res.StatusCode)
	}

	// The caller stays. They just re-authenticated, so they are known, and
	// signing them out would punish the safe action.
	if res := s.get(t, "/v1/me", first); res.StatusCode != http.StatusOK {
		t.Errorf("the caller was signed out of their own password change: %d", res.StatusCode)
	}
	// Everyone else goes.
	if res := s.get(t, "/v1/me", other); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("another session survived a password change: %d", res.StatusCode)
	}

	s.drain(t)
	if n := s.counted(t, `select count(*) from journal.line where detail->>'reason' = $1`,
		"password changed"); n != 1 {
		t.Errorf("session.ended lines with the change reason: %d, want 1", n)
	}
}

// An email change is prove-then-switch. Everything about this test is about what
// does NOT happen until the link is used.
func TestAnEmailChangeDoesNothingUntilTheNewAddressConfirms(t *testing.T) {
	s := tracedSystem(t)
	_, auth := s.signUp(t, "sam@example.com")
	s.sent.Reset()

	res := s.post(t, "/v1/me/email",
		`{"email":"new@example.com","current_password":`+quote(pass)+`}`, auth)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("request email change: %d", res.StatusCode)
	}

	// TWO messages, and which one went where is the security control —
	// decisions/0021 says nothing observes this, so this is the observation.
	sent := s.sent.Sent()
	if len(sent) != 2 {
		t.Fatalf("%d messages sent, want 2 (confirm to the new, notice to the old)", len(sent))
	}
	var confirm, notice bool
	for _, m := range sent {
		switch m.To {
		case "new@example.com":
			confirm = true
			if !strings.Contains(m.Body, "token=") {
				t.Error("the confirmation carries no link")
			}
		case "sam@example.com":
			notice = true
			// The notice must carry NO link. A "wasn't me" link in a message
			// sent to a possibly-hostile mailbox is itself a takeover primitive.
			if strings.Contains(m.Body, "token=") || strings.Contains(m.Body, "http") {
				t.Errorf("the notice to the old address carries a link:\n%s", m.Body)
			}
			if !strings.Contains(m.Body, "new@example.com") {
				t.Error("the notice does not say which address was proposed")
			}
		default:
			t.Errorf("a message went to %q", m.To)
		}
	}
	if !confirm || !notice {
		t.Fatalf("confirm=%v notice=%v", confirm, notice)
	}

	// NOTHING has changed. The old address is still the login and still what
	// /v1/me reports.
	if got := s.me(t, auth).Email; got != "sam@example.com" {
		t.Errorf("the address changed before confirmation: %q", got)
	}
	if res := s.post(t, "/v1/sessions",
		`{"email":"sam@example.com","password":`+quote(pass)+`}`, nil); res.StatusCode != http.StatusCreated {
		t.Errorf("the old address stopped working before confirmation: %d", res.StatusCode)
	}
	if res := s.post(t, "/v1/sessions",
		`{"email":"new@example.com","password":`+quote(pass)+`}`, nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("the new address worked before confirmation: %d", res.StatusCode)
	}

	// Now confirm.
	token := s.tokenTo(t, "new@example.com", "/email")
	res = s.post(t, "/v1/email-changes/confirm", `{"token":`+quote(token)+`}`, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("confirm: %d", res.StatusCode)
	}
	if got := s.me(t, auth).Email; got != "new@example.com" {
		t.Errorf("after confirming, the address is %q", got)
	}
	if res := s.post(t, "/v1/sessions",
		`{"email":"new@example.com","password":`+quote(pass)+`}`, nil); res.StatusCode != http.StatusCreated {
		t.Errorf("the new address does not work: %d", res.StatusCode)
	}
}

// A pending email change must not survive the owner's recovery — decisions/0021.
func TestAPasswordChangeKillsAPendingEmailChange(t *testing.T) {
	s := tracedSystem(t)
	_, auth := s.signUp(t, "sam@example.com")

	if res := s.post(t, "/v1/me/email",
		`{"email":"attacker@example.com","current_password":`+quote(pass)+`}`, auth); res.StatusCode != http.StatusAccepted {
		t.Fatalf("request: %d", res.StatusCode)
	}
	token := s.tokenTo(t, "attacker@example.com", "/email")

	if res := s.post(t, "/v1/me/password",
		`{"current_password":`+quote(pass)+`,"password":"an entirely different passphrase"}`, auth); res.StatusCode != http.StatusNoContent {
		t.Fatalf("change password: %d", res.StatusCode)
	}

	if res := s.post(t, "/v1/email-changes/confirm", `{"token":`+quote(token)+`}`, nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("a pending email change survived a password change: %d", res.StatusCode)
	}
	if got := s.me(t, auth).Email; got != "sam@example.com" {
		t.Errorf("the address moved anyway: %q", got)
	}
}

// A session id is rendered on the session list, so it is not a secret. An
// endpoint that revokes any id handed to it signs out anybody whose id can be
// read.
func TestASessionListShowsOnlyMineAndRevokesOnlyMine(t *testing.T) {
	s := tracedSystem(t)
	_, sam := s.signUp(t, "sam@example.com")
	_, kit := s.signUp(t, "kit@example.com")

	var kits []deviceResponse
	decode(t, s.get(t, "/v1/me/sessions", kit), &kits)
	if len(kits) != 1 || !kits[0].Current {
		t.Fatalf("kit's sessions: %+v", kits)
	}

	var sams []deviceResponse
	decode(t, s.get(t, "/v1/me/sessions", sam), &sams)
	if len(sams) != 1 {
		t.Fatalf("sam's sessions: %d", len(sams))
	}
	if sams[0].SessionID == kits[0].SessionID {
		t.Fatal("the lists are the same session")
	}

	// Sam tries to end Kit's session, with Kit's real id.
	res := s.do(t, http.MethodDelete, "/v1/me/sessions/"+kits[0].SessionID, "", sam)
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("sam ended kit's session: %d, want 404", res.StatusCode)
	}
	if res := s.get(t, "/v1/me", kit); res.StatusCode != http.StatusOK {
		t.Error("kit was signed out by somebody else")
	}

	// Sam ends their own, and is signed out.
	res = s.do(t, http.MethodDelete, "/v1/me/sessions/"+sams[0].SessionID, "", sam)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("ending my own session: %d", res.StatusCode)
	}
	if res := s.get(t, "/v1/me", sam); res.StatusCode != http.StatusUnauthorized {
		t.Error("the ended session still works")
	}
}
