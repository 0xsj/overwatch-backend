package root

import (
	"net/http"
	"testing"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
)

type targetRow struct {
	TargetID  string `json:"target_id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Archived  bool   `json:"archived"`
	CreatedAt string `json:"created_at"`
}

// The first product write through the capability gate — decisions/0018 said the
// first workspace operation built owns the check, and until now every caller was
// tenancy.
func TestTheLadderGatesAProductWrite(t *testing.T) {
	s := tracedSystem(t)
	org, ws, _, kit, ownerAuth, kitAuth := firm(t, s, orgdomain.RoleMember)
	_ = org
	base := "/v1/workspaces/" + ws.String() + "/targets"

	// No grant: the engagement is absent, so its targets are too.
	if res := s.get(t, base, kitAuth); res.StatusCode != http.StatusNotFound {
		t.Errorf("a member with no grant listed targets: %d", res.StatusCode)
	}

	// read is enough to LIST and not to ADD.
	seat := "/v1/workspaces/" + ws.String() + "/members/" + kit.String()
	if res := s.put(t, seat, `{"level":"read"}`, ownerAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("grant read: %d", res.StatusCode)
	}
	if res := s.get(t, base, kitAuth); res.StatusCode != http.StatusOK {
		t.Errorf("a reader could not list: %d", res.StatusCode)
	}
	if res := s.post(t, base, `{"name":"Northbeam","kind":"organisation"}`, kitAuth); res.StatusCode != http.StatusNotFound {
		t.Errorf("a reader added a target: %d, want 404", res.StatusCode)
	}

	// write adds, and does NOT archive.
	if res := s.put(t, seat, `{"level":"write"}`, ownerAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("grant write: %d", res.StatusCode)
	}
	res := s.post(t, base, `{"name":"Northbeam","kind":"organisation"}`, kitAuth)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("a writer could not add: %d", res.StatusCode)
	}
	var added targetRow
	decode(t, res, &added)
	if added.Kind != "organisation" || added.Name != "Northbeam" {
		t.Errorf("added: %+v", added)
	}
	if res := s.do(t, http.MethodDelete, base+"/"+added.TargetID, "", kitAuth); res.StatusCode != http.StatusNotFound {
		t.Errorf("a writer archived a target: %d, want 404", res.StatusCode)
	}
	// The owner is admin by exemption, so they may.
	if res := s.do(t, http.MethodDelete, base+"/"+added.TargetID, "", ownerAuth); res.StatusCode != http.StatusNoContent {
		t.Fatalf("archive: %d", res.StatusCode)
	}
}

// The failure decisions/0029 calls the quiet one: a product query that forgets
// workspace_id returns another engagement's rows.
func TestATargetIsNeverVisibleFromAnotherEngagement(t *testing.T) {
	s := tracedSystem(t)
	org, first, _, _, ownerAuth, _ := firm(t, s, orgdomain.RoleMember)

	var second workspaceResponse
	decode(t, s.post(t, "/v1/orgs/"+org.String()+"/workspaces", `{"name":"Globex"}`, ownerAuth), &second)
	s.drain(t)

	var added targetRow
	decode(t, s.post(t, "/v1/workspaces/"+first.String()+"/targets",
		`{"name":"Northbeam","kind":"organisation"}`, ownerAuth), &added)

	// The SAME caller, with admin on both engagements, cannot reach the target
	// through the wrong one. The id is real and the answer is still NotFound.
	other := "/v1/workspaces/" + second.WorkspaceID + "/targets/" + added.TargetID
	if res := s.do(t, http.MethodPatch, other, `{"name":"Stolen"}`, ownerAuth); res.StatusCode != http.StatusNotFound {
		t.Errorf("a target was reachable from another engagement: %d", res.StatusCode)
	}
	var listed []targetRow
	decode(t, s.get(t, "/v1/workspaces/"+second.WorkspaceID+"/targets", ownerAuth), &listed)
	if len(listed) != 0 {
		t.Errorf("the other engagement lists %d targets: %+v", len(listed), listed)
	}
}

// Archiving releases the name and reopening can collide — the same shape as
// closing an engagement, and for the same reason.
func TestArchivingATargetReleasesItsName(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, ownerAuth, _ := firm(t, s, orgdomain.RoleMember)
	base := "/v1/workspaces/" + ws.String() + "/targets"

	var first targetRow
	decode(t, s.post(t, base, `{"name":"Northbeam","kind":"organisation"}`, ownerAuth), &first)

	// Two live targets cannot share a name — folded, so case does not help.
	if res := s.post(t, base, `{"name":"northbeam","kind":"person"}`, ownerAuth); res.StatusCode != http.StatusConflict {
		t.Errorf("a duplicate name was accepted: %d", res.StatusCode)
	}

	if res := s.do(t, http.MethodDelete, base+"/"+first.TargetID, "", ownerAuth); res.StatusCode != http.StatusNoContent {
		t.Fatalf("archive: %d", res.StatusCode)
	}
	// Gone from the list, present with ?archived.
	var live []targetRow
	decode(t, s.get(t, base, ownerAuth), &live)
	if len(live) != 0 {
		t.Errorf("an archived target is still listed: %+v", live)
	}
	var all []targetRow
	decode(t, s.get(t, base+"?archived=1", ownerAuth), &all)
	if len(all) != 1 || !all[0].Archived {
		t.Fatalf("archived listing: %+v", all)
	}

	// The name is free, and taking it makes the reopen collide.
	if res := s.post(t, base, `{"name":"Northbeam","kind":"organisation"}`, ownerAuth); res.StatusCode != http.StatusCreated {
		t.Fatalf("the released name was not reusable: %d", res.StatusCode)
	}
	if res := s.post(t, base+"/"+first.TargetID+"/reopen", "", ownerAuth); res.StatusCode != http.StatusConflict {
		t.Errorf("reopening into a taken name: %d, want 409", res.StatusCode)
	}
}

// Every target event is tenanted, so it lands on the ENGAGEMENT's log.
func TestTargetActsReachTheEngagementsLog(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, ownerAuth, _ := firm(t, s, orgdomain.RoleMember)
	base := "/v1/workspaces/" + ws.String() + "/targets"

	var added targetRow
	decode(t, s.post(t, base, `{"name":"Northbeam","kind":"organisation"}`, ownerAuth), &added)
	if res := s.do(t, http.MethodPatch, base+"/"+added.TargetID, `{"name":"Northbeam Ltd"}`, ownerAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("rename: %d", res.StatusCode)
	}
	s.drain(t)

	var log pageRow
	decode(t, s.get(t, "/v1/workspaces/"+ws.String()+"/audit", ownerAuth), &log)
	seen := map[string]bool{}
	for _, e := range log.Entries {
		seen[e.Action] = true
		if e.Action == "target.added" || e.Action == "target.renamed" {
			if e.Scope != "workspace" || e.WorkspaceID != ws.String() {
				t.Errorf("a target act is not scoped to the engagement: %+v", e)
			}
		}
	}
	for _, want := range []string{"target.added", "target.renamed"} {
		if !seen[want] {
			t.Errorf("%s missing from the engagement's log", want)
		}
	}
	var from, to string
	if err := s.pool.DB(t.Context()).QueryRow(t.Context(),
		`select detail->>'from', detail->>'to' from audit.entry where action = 'target.renamed'`,
	).Scan(&from, &to); err != nil {
		t.Fatal(err)
	}
	if from != "Northbeam" || to != "Northbeam Ltd" {
		t.Errorf("the rename recorded %q -> %q", from, to)
	}
}
