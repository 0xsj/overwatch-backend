// Author-written, against a live database, for owed item D.
//
// The end-to-end claim is the one the domain cannot make: **an expired
// membership is as invisible as no membership**, and it goes invisible at the
// GATE rather than when a sweep next runs.
package root

import (
	"net/http"
	"testing"
	"time"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// box moves an existing membership onto a time-boxed role with a given end date,
// **in SQL**, because the domain refuses to CREATE a box that has already
// passed — `ErrTimeBoxPast`, on the grounds that an expiry in the past is a
// removal spelled confusingly.
//
// That refusal is correct and it is exactly why this helper exists: an expired
// membership is a state time produces, not one anybody writes, and a test about
// what happens next has to arrive at it some other way. `firm` has already
// seated the account, so this updates rather than inserts.
func (s traced) box(t *testing.T, org, account id.ID, role orgdomain.Role, until time.Time) {
	t.Helper()
	ctx := t.Context()
	if _, err := s.pool.DB(ctx).Exec(ctx,
		`update org.member set role = $3, expires_at = $4
		 where org_id = $1 and account_id = $2`,
		org.String(), account.String(), role.String(), until); err != nil {
		t.Fatal(err)
	}
}

// **THE test of this item.** A client seated until yesterday reaches nothing —
// not the report they were invited for, not the org, not even a 403 saying an
// org exists.
func TestAnExpiredMembershipIsAsInvisibleAsNoMembership(t *testing.T) {
	s := tracedSystem(t)
	org, workspace, _, kit, owner, kitAuth := firm(t, s, orgdomain.RoleMember)

	// Seat kit as a CLIENT whose box ended yesterday, and grant them read.
	s.box(t, org, kit, orgdomain.RoleClient, time.Now().Add(-24*time.Hour))
	seat := "/v1/workspaces/" + workspace.String() + "/members/" + kit.String()
	if res := s.put(t, seat, `{"level":"read"}`, owner); res.StatusCode != http.StatusOK {
		t.Fatalf("grant read: %d", res.StatusCode)
	}

	// The one route a client is ever given.
	res := s.get(t, "/v1/workspaces/"+workspace.String()+"/reports", kitAuth)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("an expired client read reports: got %d, want 404", res.StatusCode)
	}
	// AND THE ORG IS GONE FROM /me. Telling a lapsed client the org still
	// exists tells them the engagement did.
	seen := s.me(t, kitAuth)
	if found := workspaceIn(seen, org.String(), workspace.String()); found != nil {
		t.Fatalf("an expired membership still lists the engagement: %+v", found)
	}
}

// The same seat, one day EARLIER, works. Without this the test above passes for
// a membership that never worked at all.
func TestAMembershipInsideItsBoxStillReaches(t *testing.T) {
	s := tracedSystem(t)
	org, workspace, _, kit, owner, kitAuth := firm(t, s, orgdomain.RoleMember)

	s.box(t, org, kit, orgdomain.RoleClient, time.Now().Add(24*time.Hour))
	seat := "/v1/workspaces/" + workspace.String() + "/members/" + kit.String()
	if res := s.put(t, seat, `{"level":"read"}`, owner); res.StatusCode != http.StatusOK {
		t.Fatalf("grant read: %d", res.StatusCode)
	}
	if res := s.get(t, "/v1/workspaces/"+workspace.String()+"/reports", kitAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("a client inside its box: got %d, want 200", res.StatusCode)
	}
}

// The ROW IS NOT DELETED. Deleting access and deleting the record are different
// acts, and a members screen has to be able to say "expired 3 Jul" so somebody
// can extend it.
func TestAnExpiredMembershipKeepsItsRow(t *testing.T) {
	s := tracedSystem(t)
	org, _, _, kit, _, _ := firm(t, s, orgdomain.RoleMember)
	s.box(t, org, kit, orgdomain.RoleClient, time.Now().Add(-24*time.Hour))

	n := s.counted(t, `select count(*) from org.member
	                   where org_id = $1 and account_id = $2 and status <> 'archived'`,
		org.String(), kit.String())
	if n != 1 {
		t.Fatalf("the row survives expiry: %d", n)
	}
}

// Promoting somebody OFF a boxed role clears the date over the wire, so a guest
// who becomes staff stops expiring.
func TestPromotingAGuestClearsTheBoxOverTheWire(t *testing.T) {
	s := tracedSystem(t)
	org, workspace, _, kit, owner, kitAuth := firm(t, s, orgdomain.RoleMember)
	s.box(t, org, kit, orgdomain.RoleGuest, time.Now().Add(24*time.Hour))
	seat := "/v1/workspaces/" + workspace.String() + "/members/" + kit.String()
	if res := s.put(t, seat, `{"level":"read"}`, owner); res.StatusCode != http.StatusOK {
		t.Fatalf("grant: %d", res.StatusCode)
	}

	orgSeat := "/v1/orgs/" + org.String() + "/members/" + kit.String()
	if res := s.do(t, http.MethodPatch, orgSeat, `{"role":"member"}`, owner); res.StatusCode != http.StatusNoContent {
		t.Fatalf("promote: %d", res.StatusCode)
	}
	n := s.counted(t, `select count(*) from org.member
	                   where org_id = $1 and account_id = $2 and expires_at is null`,
		org.String(), kit.String())
	if n != 1 {
		t.Fatalf("a promotion clears the box: %d rows with no date", n)
	}
	// And they still reach the engagement, now without an end.
	if res := s.get(t, "/v1/workspaces/"+workspace.String()+"/runs", kitAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("a promoted guest: got %d, want 200", res.StatusCode)
	}
}

// Demoting somebody TO a boxed role without a date is refused, and the refusal
// is a 400 naming the rule rather than a constraint violation.
func TestDemotingToABoxedRoleWithoutADateIsRefused(t *testing.T) {
	s := tracedSystem(t)
	org, _, _, kit, owner, _ := firm(t, s, orgdomain.RoleMember)
	orgSeat := "/v1/orgs/" + org.String() + "/members/" + kit.String()

	if res := s.do(t, http.MethodPatch, orgSeat, `{"role":"guest"}`, owner); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", res.StatusCode)
	}
	if res := s.do(t, http.MethodPatch, orgSeat,
		`{"role":"guest","seat_until":"2027-06-01"}`, owner); res.StatusCode != http.StatusNoContent {
		t.Fatalf("with a date: %d", res.StatusCode)
	}
}
