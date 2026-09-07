package domain_test

import (
	"testing"

	"github.com/0xsj/overwatch-backend/internal/audit/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
)

// String and ParseScope are two lists that must agree, and nothing makes them.
// A scope added to one and not the other compiles, passes every test that never
// round-trips it, and fails as `not a scope this system knows` on a READ — long
// after the row was written correctly. That is how ScopeOrg shipped half-done.
func TestEveryScopeRoundTripsThroughItsName(t *testing.T) {
	for _, scope := range []domain.Scope{
		domain.ScopeSystem, domain.ScopeAccount, domain.ScopeWorkspace, domain.ScopeOrg,
	} {
		back, err := domain.ParseScope(scope.String())
		if err != nil {
			t.Errorf("%s does not parse: %v", scope, err)
			continue
		}
		if back != scope {
			t.Errorf("%s round-tripped to %s", scope, back)
		}
	}
	if _, err := domain.ParseScope("engagement"); !errors.Is(err, domain.ErrScopeUnknown) {
		t.Errorf("an unknown scope parsed: %v", err)
	}
	// A scope whose name is the empty string would read back as `system` and
	// silently reclassify every row it touched.
	for _, scope := range []domain.Scope{
		domain.ScopeSystem, domain.ScopeAccount, domain.ScopeWorkspace, domain.ScopeOrg,
	} {
		if scope.String() == "" {
			t.Errorf("scope %d has no name", scope)
		}
	}
}
