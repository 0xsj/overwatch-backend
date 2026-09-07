package root

import (
	"net/http"
	"strings"
	"testing"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }
func idParse(s string) (id.ID, error)       { return id.Parse(s) }
func orgRoleMember() orgdomain.Role         { return orgdomain.RoleMember }
func orgRoleAdmin() orgdomain.Role          { return orgdomain.RoleAdmin }

type inviteRow struct {
	InviteID    string `json:"invite_id"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	WorkspaceID string `json:"workspace_id"`
	Level       string `json:"level"`
	ExpiresAt   string `json:"expires_at"`
}

// The flow the whole slice exists for: a firm invites somebody who has no
// account, they register, and they land in the org WITH access to an engagement.
func TestAnInvitationCarriesTheFirstGrant(t *testing.T) {
	s := tracedSystem(t)
	_, ownerAuth := s.signUp(t, "sam@example.com")
	verify(t, s, ownerAuth)
	org := s.me(t, ownerAuth).Orgs[0].OrgID

	var opened workspaceResponse
	decode(t, s.post(t, "/v1/orgs/"+org+"/workspaces", `{"name":"Acme Q3"}`, ownerAuth), &opened)
	s.drain(t)
	s.sent.Reset()

	res := s.post(t, "/v1/orgs/"+org+"/invites",
		`{"email":"ana@example.com","role":"member","workspace_id":`+
			quote(opened.WorkspaceID)+`,"level":"write"}`, ownerAuth)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("invite: %d", res.StatusCode)
	}
	var sent inviteRow
	decode(t, res, &sent)
	if sent.Role != "member" || sent.Level != "write" {
		t.Errorf("the invitation: %+v", sent)
	}

	// One mail, to the invited address, naming the firm and the inviter.
	msgs := s.sent.Sent()
	if len(msgs) != 1 || msgs[0].To != "ana@example.com" {
		t.Fatalf("mail: %+v", msgs)
	}
	if !contains(msgs[0].Subject, "sam@example.com") || !contains(msgs[0].Subject, "Sam Lee") {
		t.Errorf("the subject names neither the inviter nor the firm: %q", msgs[0].Subject)
	}
	token := s.tokenTo(t, "ana@example.com", "/invite")

	// Ana has no account. She registers — which gives her HER OWN org too,
	// because registration is a chain that does not know an invitation exists.
	ana, anaAuth := s.signUp(t, "ana@example.com")
	if got := len(s.me(t, anaAuth).Orgs); got != 1 {
		t.Fatalf("before accepting, ana is in %d orgs", got)
	}

	if res := s.post(t, "/v1/invites/accept", `{"token":`+quote(token)+`}`, anaAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("accept: %d", res.StatusCode)
	}

	// Two orgs now, and the invited one comes with the engagement — NOT empty,
	// which is the state decisions/0025 exists to avoid.
	seen := s.me(t, anaAuth)
	if len(seen.Orgs) != 2 {
		t.Fatalf("after accepting, ana is in %d orgs", len(seen.Orgs))
	}
	found := workspaceIn(seen, org, opened.WorkspaceID)
	if found == nil {
		t.Fatal("the new member landed in an org with no engagements")
	}
	if found.Access != "write" {
		t.Errorf("the first grant gave %q", found.Access)
	}
	for _, o := range seen.Orgs {
		if o.OrgID == org && o.Role != "member" {
			t.Errorf("joined as %q", o.Role)
		}
	}

	// A second acceptance is refused: the invitation was consumed.
	if res := s.post(t, "/v1/invites/accept", `{"token":`+quote(token)+`}`, anaAuth); res.StatusCode == http.StatusOK {
		t.Error("an accepted invitation was accepted twice")
	}

	s.drain(t)

	// The firm's own log — decisions/0024. Org-scoped, and it names no
	// engagement.
	var firm pageRow
	decode(t, s.get(t, "/v1/orgs/"+org+"/audit", ownerAuth), &firm)
	seenActions := map[string]bool{}
	for _, e := range firm.Entries {
		seenActions[e.Action] = true
		if e.Scope != "org" {
			t.Errorf("a %q entry on the firm's log is %q-scoped", e.Action, e.Scope)
		}
		if e.WorkspaceID != "" {
			t.Errorf("an org-scope entry names a workspace: %+v", e)
		}
	}
	for _, want := range []string{"org.invite.sent", "org.invite.accepted"} {
		if !seenActions[want] {
			t.Errorf("%s missing from the firm's log", want)
		}
	}
	// The membership is WORK and reaches the journal, not audit — 0014.
	if seenActions["org.member.added"] {
		t.Error("org.member.added reached the audit trail")
	}
	if n := s.counted(t, `select count(*) from journal.line where action = $1`,
		"org.member.added"); n != 1 {
		t.Errorf("journal lines for org.member.added: %d", n)
	}
	// And the first grant landed on the ENGAGEMENT's log instead.
	if n := s.counted(t,
		`select count(*) from audit.entry where action = 'org.grant.given' and workspace_id = $1`,
		opened.WorkspaceID); n != 1 {
		t.Errorf("the first grant is not on the engagement's log: %d", n)
	}
	_ = ana
}

// The single comparison that separates an invitation from a bearer credential.
func TestAnInvitationIsBoundToTheAddressItWasSentTo(t *testing.T) {
	s := tracedSystem(t)
	_, ownerAuth := s.signUp(t, "sam@example.com")
	verify(t, s, ownerAuth)
	org := s.me(t, ownerAuth).Orgs[0].OrgID

	if res := s.post(t, "/v1/orgs/"+org+"/invites",
		`{"email":"ana@example.com","role":"member"}`, ownerAuth); res.StatusCode != http.StatusCreated {
		t.Fatalf("invite: %d", res.StatusCode)
	}
	token := s.tokenTo(t, "ana@example.com", "/invite")

	// Kit got the link somehow — forwarded, leaked, guessed at. It is useless.
	_, kitAuth := s.signUp(t, "kit@example.com")
	res := s.post(t, "/v1/invites/accept", `{"token":`+quote(token)+`}`, kitAuth)
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("somebody else's invitation was accepted: %d, want 403", res.StatusCode)
	}
	if got := len(s.me(t, kitAuth).Orgs); got != 1 {
		t.Errorf("kit joined anyway: %d orgs", got)
	}

	// Unauthenticated is refused before anything else.
	if res := s.post(t, "/v1/invites/accept", `{"token":`+quote(token)+`}`, nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("an unauthenticated accept: %d", res.StatusCode)
	}
	// And an invented token is one answer, whoever asks.
	if res := s.post(t, "/v1/invites/accept", `{"token":"nonesuch"}`, kitAuth); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("an invented token: %d", res.StatusCode)
	}
}

// Inviting needs an ORG role — the mirror of granting needing a WORKSPACE level.
func TestWhoMayInvite(t *testing.T) {
	s := tracedSystem(t)
	_, ownerAuth := s.signUp(t, "sam@example.com")
	verify(t, s, ownerAuth)
	orgs := s.me(t, ownerAuth).Orgs[0].OrgID
	org, err := idParse(orgs)
	if err != nil {
		t.Fatal(err)
	}

	// A plain member may not invite, and gets 404 — they cannot administer the
	// org and are told nothing about it.
	kit, kitAuth := s.signUp(t, "kit@example.com")
	s.seat(t, org, kit, orgRoleMember())
	verify(t, s, kitAuth)
	if res := s.post(t, "/v1/orgs/"+orgs+"/invites",
		`{"email":"x@example.com","role":"member"}`, kitAuth); res.StatusCode != http.StatusNotFound {
		t.Errorf("a member invited: %d, want 404", res.StatusCode)
	}

	// An admin may — but not as an owner.
	pat, patAuth := s.signUp(t, "pat@example.com")
	s.seat(t, org, pat, orgRoleAdmin())
	verify(t, s, patAuth)
	if res := s.post(t, "/v1/orgs/"+orgs+"/invites",
		`{"email":"x@example.com","role":"owner"}`, patAuth); res.StatusCode != http.StatusForbidden {
		t.Errorf("an admin invited an owner: %d, want 403", res.StatusCode)
	}
	if res := s.post(t, "/v1/orgs/"+orgs+"/invites",
		`{"email":"x@example.com","role":"member"}`, patAuth); res.StatusCode != http.StatusCreated {
		t.Errorf("an admin could not invite a member: %d", res.StatusCode)
	}
	// The owner may invite an owner.
	if res := s.post(t, "/v1/orgs/"+orgs+"/invites",
		`{"email":"y@example.com","role":"owner"}`, ownerAuth); res.StatusCode != http.StatusCreated {
		t.Errorf("the owner could not invite an owner: %d", res.StatusCode)
	}
	// Re-inviting somebody already in the firm is named, not silently ignored.
	if res := s.post(t, "/v1/orgs/"+orgs+"/invites",
		`{"email":"kit@example.com","role":"member"}`, ownerAuth); res.StatusCode != http.StatusConflict {
		t.Errorf("re-inviting a member: %d, want 409", res.StatusCode)
	}
}

// One live invitation per address per org: sending a second consumes the first,
// for the same reason a verification link consumes the outstanding one.
func TestASecondInvitationConsumesTheFirst(t *testing.T) {
	s := tracedSystem(t)
	_, ownerAuth := s.signUp(t, "sam@example.com")
	verify(t, s, ownerAuth)
	org := s.me(t, ownerAuth).Orgs[0].OrgID

	if res := s.post(t, "/v1/orgs/"+org+"/invites",
		`{"email":"ana@example.com","role":"member"}`, ownerAuth); res.StatusCode != http.StatusCreated {
		t.Fatalf("first invite: %d", res.StatusCode)
	}
	first := s.tokenTo(t, "ana@example.com", "/invite")
	s.sent.Reset()

	if res := s.post(t, "/v1/orgs/"+org+"/invites",
		`{"email":"ana@example.com","role":"admin"}`, ownerAuth); res.StatusCode != http.StatusCreated {
		t.Fatalf("second invite: %d", res.StatusCode)
	}
	second := s.tokenTo(t, "ana@example.com", "/invite")
	if first == second {
		t.Fatal("the second invitation reused the first token")
	}

	_, anaAuth := s.signUp(t, "ana@example.com")
	if res := s.post(t, "/v1/invites/accept", `{"token":`+quote(first)+`}`, anaAuth); res.StatusCode == http.StatusOK {
		t.Error("the superseded invitation still worked")
	}
	if res := s.post(t, "/v1/invites/accept", `{"token":`+quote(second)+`}`, anaAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("the current invitation: %d", res.StatusCode)
	}
	for _, o := range s.me(t, anaAuth).Orgs {
		if o.OrgID == org && o.Role != "admin" {
			t.Errorf("joined as %q, the superseded role", o.Role)
		}
	}
}

func TestARevokedInvitationCannotBeAccepted(t *testing.T) {
	s := tracedSystem(t)
	_, ownerAuth := s.signUp(t, "sam@example.com")
	verify(t, s, ownerAuth)
	org := s.me(t, ownerAuth).Orgs[0].OrgID

	var sent inviteRow
	decode(t, s.post(t, "/v1/orgs/"+org+"/invites",
		`{"email":"ana@example.com","role":"member"}`, ownerAuth), &sent)
	token := s.tokenTo(t, "ana@example.com", "/invite")

	if res := s.do(t, http.MethodDelete, "/v1/orgs/"+org+"/invites/"+sent.InviteID, "", ownerAuth); res.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke: %d", res.StatusCode)
	}
	_, anaAuth := s.signUp(t, "ana@example.com")
	if res := s.post(t, "/v1/invites/accept", `{"token":`+quote(token)+`}`, anaAuth); res.StatusCode == http.StatusOK {
		t.Error("a revoked invitation was accepted")
	}
	if got := len(s.me(t, anaAuth).Orgs); got != 1 {
		t.Errorf("ana joined anyway: %d orgs", got)
	}
}
