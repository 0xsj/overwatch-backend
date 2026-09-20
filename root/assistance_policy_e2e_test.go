package root

import (
	"net/http"
	"testing"

	assistdomain "github.com/0xsj/overwatch-backend/internal/assistance/domain"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
)

func TestAssistanceProviderPolicyDefaultsClosedAndIsAdminControlled(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, ownerAuth, memberAuth := firm(t, s, orgdomain.RoleMember)
	base := "/v1/workspaces/" + workspace.String() + "/assistance/policy"

	res := s.get(t, base, memberAuth)
	researchStatus(t, res, http.StatusOK)
	var initial assistdomain.ProviderPolicy
	decode(t, res, &initial)
	if initial.WorkspaceID != workspace || initial.AllowExternal {
		t.Fatalf("provider policy did not default to closed: %+v", initial)
	}

	if res = s.put(t, base, `{"allow_external":true}`, memberAuth); res.StatusCode != http.StatusForbidden {
		t.Fatalf("member changed provider policy: %d", res.StatusCode)
	}
	if res = s.put(t, base, `{"allow_external":true}`, ownerAuth); res.StatusCode != http.StatusOK {
		researchStatus(t, res, http.StatusOK)
	}
	s.drain(t)

	res = s.get(t, base, memberAuth)
	researchStatus(t, res, http.StatusOK)
	var enabled assistdomain.ProviderPolicy
	decode(t, res, &enabled)
	if !enabled.AllowExternal || enabled.UpdatedBy.IsZero() || enabled.UpdatedAt.IsZero() {
		t.Fatalf("provider policy update was not persisted: %+v", enabled)
	}
}
