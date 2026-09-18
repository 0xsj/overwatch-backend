// Author-written, for owed item D. `0019` and `0025` both define `guest` and
// `client` as time-boxed and neither built it; `0042` gave `client` real access
// to deliverables, which turned a missing column into a client reading Q3's
// report in perpetuity.
package domain_test

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var (
	seated  = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	ends    = time.Date(2026, 12, 8, 12, 0, 0, 0, time.UTC)
	after   = time.Date(2026, 12, 9, 12, 0, 0, 0, time.UTC)
	someone = boxID(1)
)

func boxID(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}

// EXACTLY the two roles, and it is one function so the domain, the schema
// constraint and the write path cannot disagree about which.
func TestOnlyAGuestAndAClientAreTimeBoxed(t *testing.T) {
	for role, want := range map[domain.Role]bool{
		domain.RoleOwner: false, domain.RoleAdmin: false, domain.RoleMember: false,
		domain.RoleGuest: true, domain.RoleClient: true,
	} {
		if role.TimeBoxed() != want {
			t.Fatalf("%s.TimeBoxed() = %v, want %v", role, role.TimeBoxed(), want)
		}
	}
}

// **A guest or a client without an end date is a rule enforced nowhere**, which
// is CLAUDE.md §5 in one field.
func TestSeatingATimeBoxedRoleNeedsAnEndDate(t *testing.T) {
	for _, role := range []domain.Role{domain.RoleGuest, domain.RoleClient} {
		if _, err := domain.NewMember(boxID(2), boxID(3), someone, role,
			time.Time{}, seated); !errors.Is(err, domain.ErrTimeBoxRequired) {
			t.Fatalf("%s with no end date: want ErrTimeBoxRequired, got %v", role, err)
		}
		got, err := domain.NewMember(boxID(2), boxID(3), someone, role, ends, seated)
		if err != nil {
			t.Fatal(err)
		}
		if !got.ExpiresAt.Equal(ends) || got.Version != 1 {
			t.Fatalf("%s: %+v", role, got)
		}
	}
}

// And ANYBODY ELSE with one is a promise nothing keeps. An owner is the firm.
func TestSeatingAnyoneElseWithAnEndDateIsRefused(t *testing.T) {
	for _, role := range []domain.Role{domain.RoleOwner, domain.RoleAdmin, domain.RoleMember} {
		if _, err := domain.NewMember(boxID(2), boxID(3), someone, role,
			ends, seated); !errors.Is(err, domain.ErrTimeBoxForbidden) {
			t.Fatalf("%s with an end date: want ErrTimeBoxForbidden, got %v", role, err)
		}
	}
}

// An end date already past is not a time box, it is a removal spelled
// confusingly. Somebody meant one of the two and should say which.
func TestAnEndDateAlreadyPastIsRefused(t *testing.T) {
	past := seated.Add(-time.Hour)
	if _, err := domain.NewMember(boxID(2), boxID(3), someone, domain.RoleClient,
		past, seated); !errors.Is(err, domain.ErrTimeBoxPast) {
		t.Fatalf("want ErrTimeBoxPast, got %v", err)
	}
	// The BOUNDARY: exactly now is also past, because a box that expires the
	// instant it is written grants nothing.
	if _, err := domain.NewMember(boxID(2), boxID(3), someone, domain.RoleClient,
		seated, seated); !errors.Is(err, domain.ErrTimeBoxPast) {
		t.Fatalf("an end date of now: want ErrTimeBoxPast, got %v", err)
	}
}

// **THE test of this item.** The gate asks this, not a sweep — a sweep runs on
// an interval, and between two ticks an expired guest holds everything.
func TestAMembershipExpiresAtItsEndDateAndNotBefore(t *testing.T) {
	client, err := domain.NewMember(boxID(2), boxID(3), someone, domain.RoleClient, ends, seated)
	if err != nil {
		t.Fatal(err)
	}
	if client.Expired(seated) {
		t.Fatal("a fresh membership is not expired")
	}
	if client.Expired(ends.Add(-time.Second)) {
		t.Fatal("a second before the end date is still inside the box")
	}
	// AT the instant, and after it.
	if !client.Expired(ends) {
		t.Fatal("the end date is the end")
	}
	if !client.Expired(after) {
		t.Fatal("a day later is certainly expired")
	}
}

// A membership with NO box never expires, which is every owner, admin and
// member — and getting this wrong would lock the firm out of its own product.
func TestAMembershipWithNoBoxNeverExpires(t *testing.T) {
	owner, err := domain.NewMember(boxID(2), boxID(3), someone, domain.RoleOwner,
		time.Time{}, seated)
	if err != nil {
		t.Fatal(err)
	}
	for _, when := range []time.Time{seated, ends, after, after.AddDate(100, 0, 0)} {
		if owner.Expired(when) {
			t.Fatalf("an owner expired at %v", when)
		}
	}
}

// **Changing role MOVES the box.** Promoting a guest clears the date — they are
// no longer time-boxed — and demoting anybody to a boxed role leaves them
// needing one, which is refused rather than defaulted.
func TestChangingRoleMovesTheTimeBoxWithIt(t *testing.T) {
	guest, err := domain.NewMember(boxID(2), boxID(3), someone, domain.RoleGuest, ends, seated)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := guest.ChangeRole(domain.RoleMember, seated)
	if err != nil {
		t.Fatal(err)
	}
	if !promoted.ExpiresAt.IsZero() {
		t.Fatalf("a promotion clears the box: %v", promoted.ExpiresAt)
	}
	// A stale expiry kept here would time-box the firm's own staff.
	if promoted.Expired(after) {
		t.Fatal("a member must not inherit a guest's end date")
	}

	// And the other direction REFUSES without a date.
	member, _ := domain.NewMember(boxID(4), boxID(3), someone, domain.RoleMember,
		time.Time{}, seated)
	if _, err := member.ChangeRole(domain.RoleClient, seated); !errors.Is(err, domain.ErrTimeBoxRequired) {
		t.Fatalf("want ErrTimeBoxRequired, got %v", err)
	}
	demoted, err := member.ChangeRoleUntil(domain.RoleClient, ends, seated)
	if err != nil {
		t.Fatal(err)
	}
	if demoted.Role != domain.RoleClient || !demoted.ExpiresAt.Equal(ends) {
		t.Fatalf("%+v", demoted)
	}
	if demoted.Version != member.Version+1 {
		t.Fatalf("one act is one version bump: %d", demoted.Version)
	}
}

// An INVITATION carries the seat date, because accepting one creates a
// membership the membership rule would otherwise refuse — 0025 said the column
// arrives with the invite.
func TestAnInvitationForATimeBoxedRoleCarriesTheSeatDate(t *testing.T) {
	if _, err := domain.NewInvite(boxID(5), boxID(3), someone, "kit@example.com",
		domain.RoleClient, time.Time{}, "hash", seated); !errors.Is(err, domain.ErrTimeBoxRequired) {
		t.Fatalf("want ErrTimeBoxRequired, got %v", err)
	}
	if _, err := domain.NewInvite(boxID(5), boxID(3), someone, "kit@example.com",
		domain.RoleMember, ends, "hash", seated); !errors.Is(err, domain.ErrTimeBoxForbidden) {
		t.Fatalf("want ErrTimeBoxForbidden, got %v", err)
	}
	got, err := domain.NewInvite(boxID(5), boxID(3), someone, "kit@example.com",
		domain.RoleClient, ends, "hash", seated)
	if err != nil {
		t.Fatal(err)
	}
	if !got.SeatUntil.Equal(ends) {
		t.Fatalf("seat until: %v", got.SeatUntil)
	}
	// **SeatUntil IS NOT ExpiresAt.** One is how long the LINK is good for; the
	// other is how long the PERSON is on the engagement. Confusing them is the
	// mistake the two names exist to prevent.
	if got.ExpiresAt.Equal(got.SeatUntil) {
		t.Fatal("the link TTL and the seat date must not be the same value")
	}
	if !got.ExpiresAt.Equal(seated.Add(domain.InviteTTL)) {
		t.Fatalf("the link TTL is unchanged: %v", got.ExpiresAt)
	}
}
