package root

import (
	"net/http"
	"testing"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// The invariant that has existed since org's first migration and could not be
// reached until now — decisions/0026.
func TestAnOrgCannotLoseItsLastOwner(t *testing.T) {
	s := tracedSystem(t)
	owner, ownerAuth := s.signUp(t, "sam@example.com")
	verify(t, s, ownerAuth)
	org := s.me(t, ownerAuth).Orgs[0].OrgID
	seat := "/v1/orgs/" + org + "/members/" + owner.String()

	// Three ways out of ownership, and all three are refused. The middle one is
	// the path the old adapter guard did not cover.
	for name, res := range map[string]*http.Response{
		"demoting themselves": s.do(t, http.MethodPatch, seat, `{"role":"admin"}`, ownerAuth),
		"removing themselves": s.do(t, http.MethodDelete, seat, "", ownerAuth),
		"leaving":             s.do(t, http.MethodDelete, "/v1/orgs/"+org+"/members/me", "", ownerAuth),
	} {
		if res.StatusCode != http.StatusConflict {
			t.Errorf("%s: %d, want 409", name, res.StatusCode)
		}
	}
	// And they are still the owner afterwards.
	for _, o := range s.me(t, ownerAuth).Orgs {
		if o.OrgID == org && o.Role != "owner" {
			t.Errorf("the refusal changed the role anyway: %q", o.Role)
		}
	}
}

// Transferring ownership is ChangeRole twice, and the other order is refused —
// decisions/0026. That refusal is the feature: the other order has a moment with
// no owner in it.
func TestTransferringOwnershipIsTwoCallsInOneOrder(t *testing.T) {
	s := tracedSystem(t)
	owner, ownerAuth := s.signUp(t, "sam@example.com")
	verify(t, s, ownerAuth)
	orgs := s.me(t, ownerAuth).Orgs[0].OrgID
	org, err := id.Parse(orgs)
	if err != nil {
		t.Fatal(err)
	}
	kit, kitAuth := s.signUp(t, "kit@example.com")
	s.seat(t, org, kit, orgdomain.RoleAdmin)

	mine := "/v1/orgs/" + orgs + "/members/" + owner.String()
	theirs := "/v1/orgs/" + orgs + "/members/" + kit.String()

	// Wrong order: demote first, and there would be nobody left.
	if res := s.do(t, http.MethodPatch, mine, `{"role":"admin"}`, ownerAuth); res.StatusCode != http.StatusConflict {
		t.Fatalf("demoting before promoting: %d, want 409", res.StatusCode)
	}
	// An admin cannot promote themselves to owner.
	if res := s.do(t, http.MethodPatch, theirs, `{"role":"owner"}`, kitAuth); res.StatusCode != http.StatusForbidden {
		t.Errorf("an admin promoted themselves: %d, want 403", res.StatusCode)
	}

	// Right order.
	if res := s.do(t, http.MethodPatch, theirs, `{"role":"owner"}`, ownerAuth); res.StatusCode != http.StatusNoContent {
		t.Fatalf("promote: %d", res.StatusCode)
	}
	if res := s.do(t, http.MethodPatch, mine, `{"role":"admin"}`, ownerAuth); res.StatusCode != http.StatusNoContent {
		t.Fatalf("demote after promoting: %d", res.StatusCode)
	}
	for _, o := range s.me(t, kitAuth).Orgs {
		if o.OrgID == orgs && o.Role != "owner" {
			t.Errorf("kit is %q after the transfer", o.Role)
		}
	}
	for _, o := range s.me(t, ownerAuth).Orgs {
		if o.OrgID == orgs && o.Role != "admin" {
			t.Errorf("sam is %q after the transfer", o.Role)
		}
	}
	// And now the NEW owner is the last one, so they are stuck the same way.
	if res := s.do(t, http.MethodPatch, theirs, `{"role":"admin"}`, kitAuth); res.StatusCode != http.StatusConflict {
		t.Errorf("the new last owner could demote themselves: %d", res.StatusCode)
	}
}

// The asymmetry decisions/0026 exists for: a demotion CAPS grants and keeps
// them; a removal DELETES them, so rejoining starts from nothing.
func TestRemovalRevokesGrantsAndDemotionDoesNot(t *testing.T) {
	s := tracedSystem(t)
	org, ws, _, kit, ownerAuth, kitAuth := firm(t, s, orgdomain.RoleMember)
	seat := "/v1/orgs/" + org.String() + "/members/" + kit.String()

	if res := s.put(t, "/v1/workspaces/"+ws.String()+"/members/"+kit.String(),
		`{"level":"write"}`, ownerAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("grant: %d", res.StatusCode)
	}
	grants := func() int {
		return s.counted(t, `select count(*) from org.grant where account_id = $1`, kit.String())
	}
	if grants() != 1 {
		t.Fatalf("grants after granting: %d", grants())
	}

	// A demotion keeps the row and caps the level.
	if res := s.do(t, http.MethodPatch, seat, `{"role":"client"}`, ownerAuth); res.StatusCode != http.StatusNoContent {
		t.Fatalf("demote: %d", res.StatusCode)
	}
	if grants() != 1 {
		t.Errorf("a demotion deleted a grant: %d remain", grants())
	}
	if found := workspaceIn(s.me(t, kitAuth), org.String(), ws.String()); found == nil || found.Access != "read" {
		t.Errorf("after the demotion: %+v", found)
	}

	// A removal deletes them.
	if res := s.do(t, http.MethodDelete, seat, "", ownerAuth); res.StatusCode != http.StatusNoContent {
		t.Fatalf("remove: %d", res.StatusCode)
	}
	if grants() != 0 {
		t.Errorf("a removal left %d grants behind — rejoining would resurrect access", grants())
	}
	// The org is gone from their /v1/me between one request and the next.
	for _, o := range s.me(t, kitAuth).Orgs {
		if o.OrgID == org.String() {
			t.Error("a removed member still sees the org")
		}
	}
	// And they are off the members list. MembersOf did not filter archived rows
	// until 2026-09-07, so this endpoint listed removed people — and no test
	// looked, because none of them removed anybody first.
	var left []memberResponse
	decode(t, s.get(t, "/v1/orgs/"+org.String()+"/members", ownerAuth), &left)
	for _, m := range left {
		if m.AccountID == kit.String() {
			t.Error("a removed member is still on the org's member list")
		}
	}
	if len(left) != 1 {
		t.Errorf("%d members after a removal, want 1", len(left))
	}

	s.drain(t)
	// The removal is a DECISION on the firm's log, and it says how much access
	// went with it.
	var firmLog pageRow
	decode(t, s.get(t, "/v1/orgs/"+org.String()+"/audit", ownerAuth), &firmLog)
	var found bool
	for _, e := range firmLog.Entries {
		if e.Action == "org.member.removed" {
			found = true
			if e.Scope != "org" {
				t.Errorf("the removal is %q-scoped", e.Scope)
			}
		}
	}
	if !found {
		t.Error("the removal is not on the firm's log")
	}
	if n := s.counted(t, `select count(*) from audit.entry where action = $1`,
		"org.member.role_changed"); n != 1 {
		t.Errorf("role_changed entries: %d", n)
	}
}

// Leaving and being removed are the same row change and two different events,
// because "did they leave or were they removed" is the first question anybody
// asks about a departure.
func TestLeavingIsItsOwnEventAndAnyoneMayDoIt(t *testing.T) {
	s := tracedSystem(t)
	org, _, _, kit, ownerAuth, kitAuth := firm(t, s, orgdomain.RoleMember)
	_ = kit

	if res := s.do(t, http.MethodDelete, "/v1/orgs/"+org.String()+"/members/me", "", kitAuth); res.StatusCode != http.StatusNoContent {
		t.Fatalf("leave: %d", res.StatusCode)
	}
	for _, o := range s.me(t, kitAuth).Orgs {
		if o.OrgID == org.String() {
			t.Error("still a member after leaving")
		}
	}
	s.drain(t)
	if n := s.counted(t, `select count(*) from audit.entry where action = $1`,
		"org.member.left"); n != 1 {
		t.Errorf("left entries: %d, want 1", n)
	}
	if n := s.counted(t, `select count(*) from audit.entry where action = $1`,
		"org.member.removed"); n != 0 {
		t.Errorf("leaving was recorded as a removal: %d", n)
	}
	_ = ownerAuth
}

func TestRenamingTheFirm(t *testing.T) {
	s := tracedSystem(t)
	org, _, _, kit, ownerAuth, kitAuth := firm(t, s, orgdomain.RoleMember)

	// A member may not.
	if res := s.do(t, http.MethodPatch, "/v1/orgs/"+org.String(), `{"name":"Nope"}`, kitAuth); res.StatusCode != http.StatusNotFound {
		t.Errorf("a member renamed the firm: %d", res.StatusCode)
	}
	if res := s.do(t, http.MethodPatch, "/v1/orgs/"+org.String(), `{"name":"Vertex Labs Security"}`, ownerAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("rename: %d", res.StatusCode)
	}
	for _, o := range s.me(t, ownerAuth).Orgs {
		if o.OrgID == org.String() && o.Name != "Vertex Labs Security" {
			t.Errorf("the firm is called %q", o.Name)
		}
	}
	s.drain(t)
	ctx := t.Context()
	var from, to string
	if err := s.pool.DB(ctx).QueryRow(ctx,
		`select detail->>'from', detail->>'to' from audit.entry where action = 'org.renamed'`,
	).Scan(&from, &to); err != nil {
		t.Fatal(err)
	}
	if to != "Vertex Labs Security" || from == "" {
		t.Errorf("the rename recorded %q -> %q", from, to)
	}
	_ = kit
}
