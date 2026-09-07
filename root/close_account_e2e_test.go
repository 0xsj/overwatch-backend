package root

import (
	"net/http"
	"strings"
	"testing"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
)

// A solo account MUST be able to close — decisions/0028. Registration makes
// every account the last owner of its own personal org, so the literal reading
// of 0026 would make this impossible for the user the product is pointed at.
func TestASoloAccountCanClose(t *testing.T) {
	s := tracedSystem(t)
	account, auth := s.signUp(t, "sam@example.com")

	if res := s.post(t, "/v1/me/close", `{"current_password":`+quote(pass)+`}`, auth); res.StatusCode != http.StatusNoContent {
		t.Fatalf("close: %d", res.StatusCode)
	}
	// The caller is signed out by the response.
	if res := s.get(t, "/v1/me", auth); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("the session survived: %d", res.StatusCode)
	}
	// And cannot sign in again.
	if res := s.post(t, "/v1/sessions",
		`{"email":"sam@example.com","password":`+quote(pass)+`}`, nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("an archived account signed in: %d", res.StatusCode)
	}

	s.drain(t)

	// The membership is gone — org's subscriber ran.
	if n := s.counted(t,
		`select count(*) from org.member where account_id = $1 and status <> 'archived'`,
		account.String()); n != 0 {
		t.Errorf("%d live memberships after closing", n)
	}
	// The org itself stays. It has no archived state, deliberately: it is a
	// container, not a lifecycle, and its workspaces are records.
	if n := s.counted(t, `select count(*) from org.org`); n != 1 {
		t.Errorf("orgs after closing: %d, want the memberless one kept", n)
	}

	// THE ADDRESS IS RELEASED, and re-registering makes a STRANGER —
	// decisions/0012 and 0028. Same person, new account id, no history.
	res := s.register(t, "sam@example.com", nil)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("the address was not re-registrable: %d", res.StatusCode)
	}
	var second struct {
		AccountID string `json:"account_id"`
	}
	decode(t, res, &second)
	if second.AccountID == account.String() {
		t.Error("re-registering revived the old account")
	}
}

// The refusal, and it NAMES the orgs — decisions/0028. "You cannot close your
// account" with no reason is a dead end.
func TestClosingIsRefusedWhereSomebodyWouldBeStranded(t *testing.T) {
	s := tracedSystem(t)
	org, _, _, kit, ownerAuth, kitAuth := firm(t, s, orgdomain.RoleMember)

	res := s.post(t, "/v1/me/close", `{"current_password":`+quote(pass)+`}`, ownerAuth)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("the last owner of a shared org closed: %d, want 409", res.StatusCode)
	}
	var refusal struct {
		Message string `json:"message"`
	}
	decode(t, res, &refusal)
	if !strings.Contains(refusal.Message, "Sam Lee") {
		t.Errorf("the refusal does not name the org: %q", refusal.Message)
	}
	if res := s.get(t, "/v1/me", ownerAuth); res.StatusCode != http.StatusOK {
		t.Error("the refusal signed them out anyway")
	}

	// A plain member is stranding nobody and may close.
	if res := s.post(t, "/v1/me/close", `{"current_password":`+quote(pass)+`}`, kitAuth); res.StatusCode != http.StatusNoContent {
		t.Fatalf("a member could not close: %d", res.StatusCode)
	}
	s.drain(t)

	// Now the owner is alone in the org, so they can too.
	if res := s.post(t, "/v1/me/close", `{"current_password":`+quote(pass)+`}`, ownerAuth); res.StatusCode != http.StatusNoContent {
		t.Errorf("the owner still could not close once alone: %d", res.StatusCode)
	}
	_ = org
	_ = kit
}

// The most destructive self-service act re-authenticates — decisions/0021 at its
// strongest.
func TestClosingNeedsTheCurrentPassword(t *testing.T) {
	s := tracedSystem(t)
	_, auth := s.signUp(t, "sam@example.com")

	if res := s.post(t, "/v1/me/close", `{"current_password":"not it"}`, auth); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a wrong password closed the account: %d", res.StatusCode)
	}
	if res := s.get(t, "/v1/me", auth); res.StatusCode != http.StatusOK {
		t.Error("a failed close ended the session anyway")
	}
	if res := s.post(t, "/v1/me/close", "{}", auth); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("an empty password closed the account: %d", res.StatusCode)
	}
}

// Grants go with the membership, and the closure is recorded on both ledgers.
func TestClosingRevokesGrantsAndIsRecorded(t *testing.T) {
	s := tracedSystem(t)
	org, ws, _, kit, ownerAuth, kitAuth := firm(t, s, orgdomain.RoleMember)

	if res := s.put(t, "/v1/workspaces/"+ws.String()+"/members/"+kit.String(),
		`{"level":"write"}`, ownerAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("grant: %d", res.StatusCode)
	}
	if res := s.post(t, "/v1/me/close", `{"current_password":`+quote(pass)+`}`, kitAuth); res.StatusCode != http.StatusNoContent {
		t.Fatalf("close: %d", res.StatusCode)
	}
	s.drain(t)

	if n := s.counted(t, `select count(*) from org.grant where account_id = $1`, kit.String()); n != 0 {
		t.Errorf("%d grants survived a closed account", n)
	}
	// The account's own history says what happened, on their activity page.
	if n := s.counted(t, `select count(*) from audit.entry where action = $1 and subject = $2`,
		"identity.account.archived", "account:"+kit.String()); n != 1 {
		t.Errorf("the closure is not on the account's own record: %d", n)
	}
	// And every session it ended says why.
	var reason string
	if err := s.pool.DB(t.Context()).QueryRow(t.Context(),
		`select detail->>'reason' from journal.line
		 where action = 'identity.session.ended' and detail->>'account_id' = $1 limit 1`,
		kit.String()).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason != "account closed" {
		t.Errorf("the sessions ended for reason %q", reason)
	}
	// The owner's seat list no longer shows them.
	var seats []seatRow
	decode(t, s.get(t, "/v1/workspaces/"+ws.String()+"/members", ownerAuth), &seats)
	for _, seat := range seats {
		if seat.AccountID == kit.String() {
			t.Error("a closed account is still on the seat list")
		}
	}
	_ = org
}
