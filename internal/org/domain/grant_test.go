package domain_test

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestEffectiveIsTheCeilingAppliedToTheBestGrant(t *testing.T) {
	for _, tc := range []struct {
		name string
		role domain.Role
		held []domain.Level
		want domain.Level
	}{
		// The exemption, and the only one — decisions/0019.
		{"an owner needs no grant", domain.RoleOwner, nil, domain.LevelAdmin},
		{"an owner's grant cannot lower them", domain.RoleOwner,
			[]domain.Level{domain.LevelRead}, domain.LevelAdmin},

		// The half 0005 left ambiguous: the intersection of NO grants is not
		// everything. This is the assertion that keeps a member from seeing
		// every engagement in the firm.
		{"an admin with no grant sees nothing", domain.RoleAdmin, nil, domain.LevelNone},
		{"a member with no grant sees nothing", domain.RoleMember, nil, domain.LevelNone},
		{"a guest with no grant sees nothing", domain.RoleGuest, nil, domain.LevelNone},

		// max() among grants — GitHub's union, inside the safe parenthesis.
		{"the best grant wins", domain.RoleAdmin,
			[]domain.Level{domain.LevelRead, domain.LevelAdmin, domain.LevelWrite},
			domain.LevelAdmin},

		// min() against the ceiling — 0005's intersection, which is what stops a
		// forgotten grant surviving a demotion.
		{"a member is capped below admin", domain.RoleMember,
			[]domain.Level{domain.LevelAdmin}, domain.LevelWrite},
		{"a guest is capped below admin", domain.RoleGuest,
			[]domain.Level{domain.LevelAdmin}, domain.LevelWrite},
		{"a client is capped at read", domain.RoleClient,
			[]domain.Level{domain.LevelAdmin}, domain.LevelRead},

		{"a grant below the ceiling is not raised to it", domain.RoleAdmin,
			[]domain.Level{domain.LevelRead}, domain.LevelRead},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := domain.Effective(tc.role, tc.held); got != tc.want {
				t.Errorf("Effective(%s, %v) = %s, want %s", tc.role, tc.held, got, tc.want)
			}
		})
	}
}

// The ladder must stay totally ordered, because Effective is a min() and a
// max() over it. A rung inserted out of order silently changes what every
// existing grant means.
func TestTheLadderIsOrdered(t *testing.T) {
	ladder := []domain.Level{
		domain.LevelNone, domain.LevelRead, domain.LevelWrite, domain.LevelAdmin,
	}
	for i := 1; i < len(ladder); i++ {
		if !(ladder[i-1] < ladder[i]) {
			t.Fatalf("%s is not below %s", ladder[i-1], ladder[i])
		}
	}
	if domain.LevelNone.Visible() {
		t.Error("none is visible, so an ungranted workspace would render")
	}
	if !domain.LevelRead.AtLeast(domain.LevelRead) {
		t.Error("AtLeast is exclusive, so a read grant cannot read")
	}
	if domain.LevelRead.AtLeast(domain.LevelWrite) {
		t.Error("a read grant satisfies a write requirement")
	}
}

func TestEveryRoleRoundTripsThroughItsName(t *testing.T) {
	for _, role := range []domain.Role{
		domain.RoleOwner, domain.RoleAdmin, domain.RoleMember,
		domain.RoleGuest, domain.RoleClient,
	} {
		back, err := domain.ParseRole(role.String())
		if err != nil || back != role {
			t.Errorf("%s round-tripped to %s: %v", role, back, err)
		}
	}
	if _, err := domain.ParseRole("superuser"); !errors.Is(err, domain.ErrRoleUnknown) {
		t.Errorf("an unknown role parsed: %v", err)
	}
	for _, level := range []domain.Level{
		domain.LevelNone, domain.LevelRead, domain.LevelWrite, domain.LevelAdmin,
	} {
		back, err := domain.ParseLevel(level.String())
		if err != nil || back != level {
			t.Errorf("%s round-tripped to %s: %v", level, back, err)
		}
	}
}

// A grant of `none` is a revocation, and revoking deletes the row. Storing it
// would give absence two spellings, and the two disagree the first time a query
// remembers only one.
func TestAGrantOfNoneIsRefused(t *testing.T) {
	at := time.Now()
	one, two, three, four := nonZero(1), nonZero(2), nonZero(3), nonZero(4)

	if _, err := domain.NewGrant(one, two, three, four, domain.LevelNone, at); !errors.Is(err, domain.ErrGrantEmpty) {
		t.Errorf("a grant of none was accepted: %v", err)
	}
	g, err := domain.NewGrant(one, two, three, four, domain.LevelRead, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Retarget(domain.LevelNone, at); !errors.Is(err, domain.ErrGrantEmpty) {
		t.Errorf("a grant was retargeted to none: %v", err)
	}
	next, err := g.Retarget(domain.LevelWrite, at.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if next.Version != g.Version+1 {
		t.Errorf("version %d after a retarget from %d", next.Version, g.Version)
	}
}

// nonZero is a literal identifier. The domain's only rule about an id is that it
// is not the zero value, so a test that needs four distinct ones does not need a
// generator to make them.
func nonZero(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}
