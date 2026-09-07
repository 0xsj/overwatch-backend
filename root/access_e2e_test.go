package root

import (
	"context"
	"crypto/rand"
	"net/http"
	"testing"
	"time"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	orgpg "github.com/0xsj/overwatch-backend/internal/org/infra/postgres"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// A second member is written directly through the store, because no invite
// command exists yet — decisions/0019 defines the roles and says the flow that
// creates them arrives with the invite. Reaching past the (absent) command is
// legitimate here: what is under test is the GATE, not how a member got there.
func (s traced) seat(t *testing.T, org, account id.ID, role orgdomain.Role) {
	t.Helper()
	ctx := context.Background()
	ids := id.NewV7(clock.System{}, rand.Reader)
	member, err := orgdomain.NewMember(ids.NewID(), org, account, role, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := orgpg.NewStore(s.pool).AddMember(ctx, member); err != nil {
		t.Fatal(err)
	}
}

func (s traced) grant(t *testing.T, org, account, workspace id.ID, level orgdomain.Level) {
	t.Helper()
	ctx := context.Background()
	ids := id.NewV7(clock.System{}, rand.Reader)
	g, err := orgdomain.NewGrant(ids.NewID(), org, account, workspace, level, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := orgpg.NewStore(s.pool).AddGrant(ctx, g); err != nil {
		t.Fatal(err)
	}
}

// signUp walks a whole registration and hands back a signed-in caller.
func (s traced) signUp(t *testing.T, email string) (account id.ID, auth map[string]string) {
	t.Helper()
	if res := s.register(t, email, nil); res.StatusCode != http.StatusCreated {
		t.Fatalf("register %s: %d", email, res.StatusCode)
	}
	s.drain(t)

	var in struct {
		Token     string `json:"token"`
		AccountID string `json:"account_id"`
	}
	decode(t, s.post(t, "/v1/sessions",
		`{"email":`+quote(email)+`,"password":"a passphrase nobody guesses"}`, nil), &in)
	parsed, err := id.Parse(in.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	return parsed, map[string]string{"authorization": "Bearer " + in.Token}
}

func (s traced) me(t *testing.T, auth map[string]string) meResponse {
	t.Helper()
	var out meResponse
	decode(t, s.get(t, "/v1/me", auth), &out)
	return out
}

// This is the bug decisions/0019 predicted and the reason it was written before
// any of this shipped: /v1/me listed every workspace of every org the caller was
// a member of, with no grant consulted. It could not fail while every org had
// exactly one member, because that member is the owner and the owner is exempt.
func TestAMemberWithNoGrantCannotSeeAWorkspace(t *testing.T) {
	s := tracedSystem(t)

	owner, ownerAuth := s.signUp(t, "sam@example.com")
	_ = owner

	seen := s.me(t, ownerAuth)
	if len(seen.Orgs) != 1 || len(seen.Orgs[0].Workspaces) != 1 {
		t.Fatalf("the owner sees %d orgs", len(seen.Orgs))
	}
	if got := seen.Orgs[0].Workspaces[0].Access; got != "admin" {
		t.Errorf("the owner's access is %q, not admin — the exemption is gone", got)
	}
	org, err := id.Parse(seen.Orgs[0].OrgID)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := id.Parse(seen.Orgs[0].Workspaces[0].WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}

	// A second person, in the same org, as a plain member. They have their own
	// org from their own registration; this seats them in Sam's.
	kit, kitAuth := s.signUp(t, "kit@example.com")
	s.seat(t, org, kit, orgdomain.RoleMember)

	seen = s.me(t, kitAuth)
	var sams *meOrg
	for i := range seen.Orgs {
		if seen.Orgs[i].OrgID == org.String() {
			sams = &seen.Orgs[i]
		}
	}
	if sams == nil {
		t.Fatal("the new member cannot see the org they belong to")
	}
	if sams.Role != "member" {
		t.Errorf("seated as %q", sams.Role)
	}
	// The whole point. The workspace EXISTS, they are a member of the org that
	// owns it, and they must not see that it is there.
	if len(sams.Workspaces) != 0 {
		t.Fatalf("a member with no grant sees %d workspaces: %+v",
			len(sams.Workspaces), sams.Workspaces)
	}

	// Grant read, and it appears — with the level, not just the name.
	s.grant(t, org, kit, workspace, orgdomain.LevelRead)
	seen = s.me(t, kitAuth)
	for i := range seen.Orgs {
		if seen.Orgs[i].OrgID != org.String() {
			continue
		}
		if len(seen.Orgs[i].Workspaces) != 1 {
			t.Fatalf("after a grant, %d workspaces", len(seen.Orgs[i].Workspaces))
		}
		if got := seen.Orgs[i].Workspaces[0].Access; got != "read" {
			t.Errorf("access %q after a read grant", got)
		}
	}
}

// An admin manages people, not work — decisions/0019. Promoting somebody to
// handle invitations must not hand them every client's wall.
func TestAnAdminIsNotExemptTheWayAnOwnerIs(t *testing.T) {
	s := tracedSystem(t)

	_, ownerAuth := s.signUp(t, "sam@example.com")
	seen := s.me(t, ownerAuth)
	org, err := id.Parse(seen.Orgs[0].OrgID)
	if err != nil {
		t.Fatal(err)
	}

	kit, kitAuth := s.signUp(t, "kit@example.com")
	s.seat(t, org, kit, orgdomain.RoleAdmin)

	for _, o := range s.me(t, kitAuth).Orgs {
		if o.OrgID != org.String() {
			continue
		}
		if o.Role != "admin" {
			t.Fatalf("seated as %q", o.Role)
		}
		if len(o.Workspaces) != 0 {
			t.Errorf("an admin with no grant sees %d workspaces", len(o.Workspaces))
		}
	}
}

// A caller who is not a member gets NotFound and never Forbidden. A 403 tells an
// analyst that a client they cannot see exists, which for the client behind that
// wall is the leak itself — decisions/0005.
func TestAStrangerCannotTellAnOrgApartFromNothing(t *testing.T) {
	s := tracedSystem(t)

	_, ownerAuth := s.signUp(t, "sam@example.com")
	org := s.me(t, ownerAuth).Orgs[0].OrgID

	_, strangerAuth := s.signUp(t, "kit@example.com")

	real := s.get(t, "/v1/orgs/"+org+"/members", strangerAuth)
	invented := s.get(t, "/v1/orgs/01a07b02-0000-7000-0000-000000000000/members", strangerAuth)
	malformed := s.get(t, "/v1/orgs/not-an-id/members", strangerAuth)

	for name, res := range map[string]*http.Response{
		"an org that exists": real, "one that does not": invented, "a malformed id": malformed,
	} {
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", name, res.StatusCode)
		}
	}
}

// The members screen is the join /v1/me already is: org owns the seat, identity
// owns the name, and the root is the only place allowed to know both.
func TestTheMemberListNamesPeopleOrgCannotSee(t *testing.T) {
	s := tracedSystem(t)

	_, ownerAuth := s.signUp(t, "sam@example.com")
	seen := s.me(t, ownerAuth)
	org, err := id.Parse(seen.Orgs[0].OrgID)
	if err != nil {
		t.Fatal(err)
	}
	kit, _ := s.signUp(t, "kit@example.com")
	s.seat(t, org, kit, orgdomain.RoleMember)

	var members []memberResponse
	decode(t, s.get(t, "/v1/orgs/"+org.String()+"/members", ownerAuth), &members)
	if len(members) != 2 {
		t.Fatalf("%d members: %+v", len(members), members)
	}
	if members[0].Role != "owner" || members[0].Email != "sam@example.com" {
		t.Errorf("first seat: %+v", members[0])
	}
	if members[1].Role != "member" || members[1].Email != "kit@example.com" {
		t.Errorf("second seat: %+v", members[1])
	}
	if members[1].JoinedAt == "" {
		t.Error("a seat with no joined_at cannot be sorted or explained")
	}
}

// The other half of decisions/0020: an admin is not exempt, so an engagement
// they open is invisible to them until the grant subscriber runs. If the fourth
// link is ever dropped from the dispatcher this is the test that notices.
func TestWhoeverOpensAnEngagementEndsUpAdminOnIt(t *testing.T) {
	s := tracedSystem(t)

	_, ownerAuth := s.signUp(t, "sam@example.com")
	seen := s.me(t, ownerAuth)
	org, err := id.Parse(seen.Orgs[0].OrgID)
	if err != nil {
		t.Fatal(err)
	}
	// Verified, because opening an engagement is gated on it — decisions/0018.
	verify(t, s, ownerAuth)

	kit, kitAuth := s.signUp(t, "kit@example.com")
	s.seat(t, org, kit, orgdomain.RoleAdmin)
	verify(t, s, kitAuth)

	// An admin may start one. A member may not.
	res := s.post(t, "/v1/orgs/"+org.String()+"/workspaces", `{"name":"Acme Q3"}`, kitAuth)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("an admin could not open an engagement: %d", res.StatusCode)
	}
	var opened workspaceResponse
	decode(t, res, &opened)
	if opened.Name != "Acme Q3" {
		t.Errorf("opened %q", opened.Name)
	}

	// Before the subscriber runs the admin holds no grant, and the workspace is
	// therefore invisible to them. That window is decisions/0020's stated cost.
	if found := workspaceIn(s.me(t, kitAuth), org.String(), opened.WorkspaceID); found != nil {
		t.Error("the admin could see it before the grant was written")
	}

	s.drain(t)

	found := workspaceIn(s.me(t, kitAuth), org.String(), opened.WorkspaceID)
	if found == nil {
		t.Fatal("the admin cannot see the engagement they opened")
	}
	if found.Access != "admin" {
		t.Errorf("the opener has %q on what they opened", found.Access)
	}

	// created_by is a RECORD and not a permission — it is never consulted by
	// the gate. What makes the opener admin is the grant the subscriber wrote.
	var createdBy string
	ctx := context.Background()
	if err := s.pool.DB(ctx).QueryRow(ctx,
		`select coalesce(created_by::text,'') from workspace.workspace where id = $1`,
		opened.WorkspaceID).Scan(&createdBy); err != nil {
		t.Fatal(err)
	}
	if createdBy != kit.String() {
		t.Errorf("created_by is %q, not the opener", createdBy)
	}

	// One act, two records — decisions/0014 and 0020. The work is in the
	// journal; the choice is in audit.
	if n := s.counted(t, `select count(*) from journal.line where action = $1`,
		"workspace.opened"); n != 1 {
		t.Errorf("journal lines for workspace.opened: %d", n)
	}
	if n := s.counted(t, `select count(*) from audit.entry where action = $1`,
		"workspace.opened"); n != 1 {
		t.Errorf("audit entries for workspace.opened: %d — the decision was not recorded", n)
	}
	// And provisioning stays out of audit, exactly as before.
	if n := s.counted(t, `select count(*) from audit.entry where action = $1`,
		"workspace.created"); n != 0 {
		t.Errorf("workspace.created reached audit: %d rows", n)
	}

	// A plain member may not start one, and is told why rather than being shown
	// a 404 — they already know the org exists.
	pat, patAuth := s.signUp(t, "pat@example.com")
	s.seat(t, org, pat, orgdomain.RoleMember)
	verify(t, s, patAuth)
	res = s.post(t, "/v1/orgs/"+org.String()+"/workspaces", `{"name":"CTF1"}`, patAuth)
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("a member opening an engagement: %d, want 403", res.StatusCode)
	}
	_ = pat
}

// verify walks the account behind an authorisation header through its
// verification link, because opening an engagement is gated on a proven address.
func verify(t *testing.T, s traced, auth map[string]string) {
	t.Helper()
	var who meResponse
	decode(t, s.get(t, "/v1/me", auth), &who)
	if res := s.post(t, "/v1/verifications", `{"email":`+quote(who.Email)+`}`, nil); res.StatusCode != http.StatusAccepted {
		t.Fatalf("request verification: %d", res.StatusCode)
	}
	token := s.token(t, "/verify")
	if res := s.post(t, "/v1/verifications/confirm", `{"token":`+quote(token)+`}`, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("confirm: %d", res.StatusCode)
	}
}

func workspaceIn(seen meResponse, org, workspace string) *meWorkspace {
	for i := range seen.Orgs {
		if seen.Orgs[i].OrgID != org {
			continue
		}
		for j := range seen.Orgs[i].Workspaces {
			if seen.Orgs[i].Workspaces[j].WorkspaceID == workspace {
				return &seen.Orgs[i].Workspaces[j]
			}
		}
	}
	return nil
}
