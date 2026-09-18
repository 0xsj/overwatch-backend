// Author-written, from decisions/0040 §3, written before this code.
package domain_test

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/tool/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var at = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func an(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}

// 0040 §3. A SOURCE tool consumes nothing — it is seeded from the target — so
// there is no upstream fragment for an edge to point back at. A `derived_from`
// mapping on one could never fire, and a mapping that can never fire is one
// somebody believes is working.
func TestASourceToolCannotDeclareWhereItsInputCameFrom(t *testing.T) {
	_, err := domain.NewMapping(an(1), an(2), an(3), an(4), "input", ".input", 1,
		domain.RoleDerivedFrom, domain.FeedNone, at)
	if !errors.Is(err, domain.ErrSourceToolHasNoInput) {
		t.Fatalf("want ErrSourceToolHasNoInput, got %v", err)
	}
	// The SAME mapping on a tool that consumes something is fine.
	got, err := domain.NewMapping(an(1), an(2), an(3), an(4), "input", ".input", 1,
		domain.RoleDerivedFrom, domain.FeedHost, at)
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != domain.RoleDerivedFrom {
		t.Fatalf("role: %s", got.Role)
	}
}

// A source tool's SUBJECT and ATTRIBUTE mappings are unaffected — it is only
// provenance that needs an upstream.
func TestASourceToolStillMapsItsOwnOutput(t *testing.T) {
	for _, role := range []domain.Role{domain.RoleSubject, domain.RoleAttribute} {
		if _, err := domain.NewMapping(an(1), an(2), an(3), an(4), "host", ".host", 1,
			role, domain.FeedNone, at); err != nil {
			t.Fatalf("%s on a source tool: %v", role, err)
		}
	}
}

// The zero value is the HARMLESS one, so a mapping nobody thought about reads a
// value and changes nothing else.
func TestTheDefaultRoleIsAttribute(t *testing.T) {
	var zero domain.Role
	if zero != domain.RoleAttribute || zero.String() != "attribute" {
		t.Fatalf("the zero role must be attribute, got %s", zero)
	}
	got, err := domain.NewMapping(an(1), an(2), an(3), an(4), "title", ".title", 1,
		zero, domain.FeedNone, at)
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != domain.RoleAttribute {
		t.Fatalf("role: %s", got.Role)
	}
}

func TestEveryRoleRoundTripsThroughItsName(t *testing.T) {
	for _, role := range []domain.Role{
		domain.RoleAttribute, domain.RoleSubject, domain.RoleDerivedFrom,
	} {
		got, err := domain.ParseRole(role.String())
		if err != nil || got != role {
			t.Fatalf("%s round-tripped to %s (%v)", role, got, err)
		}
	}
	if _, err := domain.ParseRole("provenance"); !errors.Is(err, domain.ErrRoleUnknown) {
		t.Fatalf("want ErrRoleUnknown, got %v", err)
	}
	// An UNKNOWN role must not silently become `derived_from` — that would draw
	// edges nobody asked for.
	if got, _ := domain.ParseRole("nonsense"); got != domain.RoleAttribute {
		t.Fatalf("an unknown role falls back to the harmless one, got %s", got)
	}
}
