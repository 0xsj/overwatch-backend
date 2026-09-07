package root

import (
	"net/http"
	"testing"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type seatRow struct {
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	Access    string `json:"access"`
}

// firm sets up an owner with a real engagement, plus a seated member, which is
// what every test below needs and none of it can do over HTTP yet.
func firm(t *testing.T, s traced, role orgdomain.Role) (
	org, workspace, owner, other id.ID, ownerAuth, otherAuth map[string]string) {
	t.Helper()

	owner, ownerAuth = s.signUp(t, "sam@example.com")
	verify(t, s, ownerAuth)
	seen := s.me(t, ownerAuth)
	// Checked rather than indexed. A bare Orgs[0] here fails as a panic with a
	// stack trace in a helper, which says nothing about which test broke or why
	// — and the chain not having drained is exactly the case that hits it.
	if len(seen.Orgs) == 0 {
		t.Fatal("the registration chain left the owner with no org")
	}
	org, err := id.Parse(seen.Orgs[0].OrgID)
	if err != nil {
		t.Fatal(err)
	}

	res := s.post(t, "/v1/orgs/"+org.String()+"/workspaces", `{"name":"Acme Q3"}`, ownerAuth)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("open: %d", res.StatusCode)
	}
	var opened workspaceResponse
	decode(t, res, &opened)
	workspace, err = id.Parse(opened.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	s.drain(t)

	other, otherAuth = s.signUp(t, "kit@example.com")
	s.seat(t, org, other, role)
	return org, workspace, owner, other, ownerAuth, otherAuth
}

func (s traced) put(t *testing.T, path, body string, headers map[string]string) *http.Response {
	t.Helper()
	return s.do(t, http.MethodPut, path, body, headers)
}

func TestPuttingSomebodyOnAnEngagement(t *testing.T) {
	s := tracedSystem(t)
	org, ws, owner, kit, ownerAuth, kitAuth := firm(t, s, orgdomain.RoleMember)
	_ = org

	seat := "/v1/workspaces/" + ws.String() + "/members/" + kit.String()

	// Before: kit is a member of the org and the engagement is invisible.
	if found := workspaceIn(s.me(t, kitAuth), org.String(), ws.String()); found != nil {
		t.Fatal("a member with no grant can see the engagement")
	}
	if res := s.get(t, "/v1/workspaces/"+ws.String()+"/members", kitAuth); res.StatusCode != http.StatusNotFound {
		t.Errorf("a member with no grant read the seat list: %d", res.StatusCode)
	}

	// The owner grants read.
	if res := s.put(t, seat, `{"level":"read"}`, ownerAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("grant: %d", res.StatusCode)
	}
	found := workspaceIn(s.me(t, kitAuth), org.String(), ws.String())
	if found == nil || found.Access != "read" {
		t.Fatalf("after a read grant: %+v", found)
	}

	// PUT to the same level is idempotent and writes no second ledger row.
	if res := s.put(t, seat, `{"level":"read"}`, ownerAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("re-grant: %d", res.StatusCode)
	}

	// Raise it, then remove it.
	if res := s.put(t, seat, `{"level":"write"}`, ownerAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("change: %d", res.StatusCode)
	}
	if found := workspaceIn(s.me(t, kitAuth), org.String(), ws.String()); found.Access != "write" {
		t.Errorf("after a change: %+v", found)
	}
	if res := s.do(t, http.MethodDelete, seat, "", ownerAuth); res.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke: %d", res.StatusCode)
	}
	if found := workspaceIn(s.me(t, kitAuth), org.String(), ws.String()); found != nil {
		t.Errorf("the engagement is still visible after a revoke: %+v", found)
	}
	// Revoking twice is the outcome the caller wanted, not an error.
	if res := s.do(t, http.MethodDelete, seat, "", ownerAuth); res.StatusCode != http.StatusNoContent {
		t.Errorf("revoking twice: %d", res.StatusCode)
	}

	s.drain(t)

	// One given, one changed, one revoked — and the second identical PUT wrote
	// nothing, which is what makes the count three rather than four.
	for action, want := range map[string]int{
		"org.grant.given": 1, "org.grant.changed": 1, "org.grant.revoked": 1,
	} {
		if n := s.counted(t, `select count(*) from audit.entry where action = $1`, action); n != want {
			t.Errorf("audit entries for %s: %d, want %d", action, n, want)
		}
	}
	// And they are on the ENGAGEMENT's log, not on nobody's. An untenanted
	// event lands under scope=system and appears on no screen at all.
	if n := s.counted(t,
		`select count(*) from audit.entry where workspace_id = $1 and action like 'org.grant.%'`,
		ws.String()); n != 3 {
		t.Errorf("grant entries scoped to the engagement: %d, want 3", n)
	}
	var from, to string
	if err := s.pool.DB(t.Context()).QueryRow(t.Context(),
		`select detail->>'from', detail->>'to' from audit.entry where action = 'org.grant.changed'`,
	).Scan(&from, &to); err != nil {
		t.Fatal(err)
	}
	if from != "read" || to != "write" {
		t.Errorf("the change recorded %q -> %q", from, to)
	}
	_ = owner
}

// decisions/0023: an ORG admin manages people, not work. Administering the firm
// does not imply putting somebody on Acme Q3.
func TestOnlyWorkspaceAdminMayGrant(t *testing.T) {
	s := tracedSystem(t)
	org, ws, _, kit, ownerAuth, kitAuth := firm(t, s, orgdomain.RoleAdmin)
	pat, _ := s.signUp(t, "pat@example.com")
	s.seat(t, org, pat, orgdomain.RoleMember)

	seat := "/v1/workspaces/" + ws.String() + "/members/" + pat.String()

	// An org admin with NO grant on this engagement cannot grant on it — and
	// gets 404, because they cannot see it at all.
	if res := s.put(t, seat, `{"level":"read"}`, kitAuth); res.StatusCode != http.StatusNotFound {
		t.Errorf("an org admin with no grant granted: %d, want 404", res.StatusCode)
	}

	// Give them read. Still not enough: granting needs admin ON the workspace.
	if res := s.put(t, "/v1/workspaces/"+ws.String()+"/members/"+kit.String(),
		`{"level":"read"}`, ownerAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("owner grants read to the admin: %d", res.StatusCode)
	}
	if res := s.put(t, seat, `{"level":"read"}`, kitAuth); res.StatusCode != http.StatusNotFound {
		t.Errorf("a reader granted: %d, want 404", res.StatusCode)
	}

	// Give them admin on the engagement, and now they may.
	if res := s.put(t, "/v1/workspaces/"+ws.String()+"/members/"+kit.String(),
		`{"level":"admin"}`, ownerAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("owner grants admin: %d", res.StatusCode)
	}
	if res := s.put(t, seat, `{"level":"read"}`, kitAuth); res.StatusCode != http.StatusOK {
		t.Errorf("a workspace admin could not grant: %d", res.StatusCode)
	}
}

// The ergonomic half of decisions/0023: refused at write, so the grid never
// displays a level somebody does not have.
func TestAGrantAboveTheRoleCeilingIsRefused(t *testing.T) {
	s := tracedSystem(t)
	org, ws, _, kit, ownerAuth, _ := firm(t, s, orgdomain.RoleClient)
	_ = org
	seat := "/v1/workspaces/" + ws.String() + "/members/" + kit.String()

	// A client's ceiling is read.
	if res := s.put(t, seat, `{"level":"write"}`, ownerAuth); res.StatusCode != http.StatusConflict {
		t.Errorf("a client was given write: %d, want 409", res.StatusCode)
	}
	if res := s.put(t, seat, `{"level":"read"}`, ownerAuth); res.StatusCode != http.StatusOK {
		t.Errorf("a client could not be given read: %d", res.StatusCode)
	}

	// And a grant to somebody who is not a member at all.
	stranger, _ := s.signUp(t, "nobody@example.com")
	res := s.put(t, "/v1/workspaces/"+ws.String()+"/members/"+stranger.String(),
		`{"level":"read"}`, ownerAuth)
	if res.StatusCode != http.StatusConflict {
		t.Errorf("a non-member was granted: %d, want 409", res.StatusCode)
	}
}

// The grid row. The owner appears with `admin` despite holding no grant row,
// because the exemption is why they can see it.
func TestTheSeatListShowsEffectiveAccessAndIncludesTheOwner(t *testing.T) {
	s := tracedSystem(t)
	org, ws, owner, kit, ownerAuth, _ := firm(t, s, orgdomain.RoleMember)
	_ = org

	var seats []seatRow
	decode(t, s.get(t, "/v1/workspaces/"+ws.String()+"/members", ownerAuth), &seats)
	if len(seats) != 1 {
		t.Fatalf("%d seats before granting: %+v", len(seats), seats)
	}
	if seats[0].AccountID != owner.String() || seats[0].Access != "admin" {
		t.Errorf("the owner's seat: %+v", seats[0])
	}

	if res := s.put(t, "/v1/workspaces/"+ws.String()+"/members/"+kit.String(),
		`{"level":"write"}`, ownerAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("grant: %d", res.StatusCode)
	}
	decode(t, s.get(t, "/v1/workspaces/"+ws.String()+"/members", ownerAuth), &seats)
	if len(seats) != 2 {
		t.Fatalf("%d seats after granting: %+v", len(seats), seats)
	}
	var found bool
	for _, seat := range seats {
		if seat.AccountID != kit.String() {
			continue
		}
		found = true
		if seat.Access != "write" || seat.Role != "member" || seat.Email != "kit@example.com" {
			t.Errorf("kit's seat: %+v", seat)
		}
	}
	if !found {
		t.Error("the granted member is not on the seat list")
	}
}

// Effective, not stored — decisions/0023. A grant that outlives a demotion is
// capped at read time, which is the security property the write-time refusal
// cannot provide.
func TestAGrantIsCappedWhenTheRoleIsLoweredUnderIt(t *testing.T) {
	s := tracedSystem(t)
	org, ws, _, kit, ownerAuth, kitAuth := firm(t, s, orgdomain.RoleMember)

	if res := s.put(t, "/v1/workspaces/"+ws.String()+"/members/"+kit.String(),
		`{"level":"write"}`, ownerAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("grant: %d", res.StatusCode)
	}
	if found := workspaceIn(s.me(t, kitAuth), org.String(), ws.String()); found.Access != "write" {
		t.Fatalf("before the demotion: %+v", found)
	}

	// Demote to client, whose ceiling is read. Nothing revokes the grant.
	ctx := t.Context()
	if _, err := s.pool.DB(ctx).Exec(ctx,
		`update org.member set role = 'client' where org_id = $1 and account_id = $2`,
		org.String(), kit.String()); err != nil {
		t.Fatal(err)
	}

	if found := workspaceIn(s.me(t, kitAuth), org.String(), ws.String()); found.Access != "read" {
		t.Errorf("after the demotion the grant was not capped: %+v", found)
	}
	// And the grid agrees, rather than showing the stored `write`.
	var seats []seatRow
	decode(t, s.get(t, "/v1/workspaces/"+ws.String()+"/members", ownerAuth), &seats)
	for _, seat := range seats {
		if seat.AccountID == kit.String() && seat.Access != "read" {
			t.Errorf("the grid shows %q for a demoted member", seat.Access)
		}
	}
}
