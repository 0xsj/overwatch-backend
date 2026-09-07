package root

import (
	"net/http"
	"testing"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
)

type engagementRow struct {
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Access      string `json:"access"`
	Closed      bool   `json:"closed"`
}

// The whole of decisions/0027 in one walk: closing hides an engagement from the
// switcher, refuses work in it, KEEPS its record readable, and is reversible.
func TestClosingAnEngagementKeepsItsRecordAndIsReversible(t *testing.T) {
	s := tracedSystem(t)
	org, ws, _, kit, ownerAuth, kitAuth := firm(t, s, orgdomain.RoleMember)
	seat := "/v1/workspaces/" + ws.String() + "/members/" + kit.String()

	if res := s.put(t, seat, `{"level":"write"}`, ownerAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("grant: %d", res.StatusCode)
	}
	s.drain(t)

	if res := s.post(t, "/v1/workspaces/"+ws.String()+"/close", "", ownerAuth); res.StatusCode != http.StatusNoContent {
		t.Fatalf("close: %d", res.StatusCode)
	}
	s.drain(t)

	// Gone from the switcher, for everybody.
	if found := workspaceIn(s.me(t, ownerAuth), org.String(), ws.String()); found != nil {
		t.Error("a closed engagement is still in /v1/me")
	}
	if found := workspaceIn(s.me(t, kitAuth), org.String(), ws.String()); found != nil {
		t.Error("a closed engagement is still in a member's /v1/me")
	}

	// ACTING is refused — 409, not 404: the caller can see it and knows it
	// exists, so what they need is the reason.
	for name, res := range map[string]*http.Response{
		"granting":      s.put(t, seat, `{"level":"read"}`, ownerAuth),
		"renaming":      s.do(t, http.MethodPatch, "/v1/workspaces/"+ws.String(), `{"name":"Acme Q4"}`, ownerAuth),
		"closing again": s.post(t, "/v1/workspaces/"+ws.String()+"/close", "", ownerAuth),
	} {
		if res.StatusCode != http.StatusConflict {
			t.Errorf("%s a closed engagement: %d, want 409", name, res.StatusCode)
		}
	}

	// READING its record is NOT. This is the bug 0027 was written to fix: the
	// record is kept precisely to be read afterwards.
	var log pageRow
	res := s.get(t, "/v1/workspaces/"+ws.String()+"/audit", ownerAuth)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("a closed engagement's audit log: %d, want 200", res.StatusCode)
	}
	decode(t, res, &log)
	var closed bool
	for _, e := range log.Entries {
		if e.Action == "workspace.archived" {
			closed = true
			if e.Scope != "workspace" {
				t.Errorf("the close is %q-scoped", e.Scope)
			}
		}
	}
	if !closed {
		t.Error("the close is not on the engagement's own log")
	}
	// And the member who was on it can still read it — grants survive closing.
	if res := s.get(t, "/v1/workspaces/"+ws.String()+"/audit", kitAuth); res.StatusCode != http.StatusOK {
		t.Errorf("a member lost the record when it closed: %d", res.StatusCode)
	}
	if res := s.get(t, "/v1/workspaces/"+ws.String()+"/members", kitAuth); res.StatusCode != http.StatusOK {
		t.Errorf("the seat list of a closed engagement: %d", res.StatusCode)
	}

	// It is reachable through the org listing, which is what makes reopening
	// possible at all.
	var all []engagementRow
	decode(t, s.get(t, "/v1/orgs/"+org.String()+"/workspaces", ownerAuth), &all)
	var listed bool
	for _, e := range all {
		if e.WorkspaceID == ws.String() {
			listed = true
			if !e.Closed {
				t.Error("the listing does not mark it closed")
			}
		}
	}
	if !listed {
		t.Fatal("a closed engagement is unreachable — nothing can reopen it")
	}

	// Reopen, and everything comes back.
	if res := s.post(t, "/v1/workspaces/"+ws.String()+"/reopen", "", ownerAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("reopen: %d", res.StatusCode)
	}
	found := workspaceIn(s.me(t, kitAuth), org.String(), ws.String())
	if found == nil || found.Access != "write" {
		t.Errorf("after reopening, the member's access is %+v — grants should have survived", found)
	}
	if res := s.post(t, "/v1/workspaces/"+ws.String()+"/reopen", "", ownerAuth); res.StatusCode != http.StatusConflict {
		t.Errorf("reopening an open engagement: %d, want 409", res.StatusCode)
	}

	s.drain(t)
	if n := s.counted(t, `select count(*) from audit.entry where action = $1`,
		"workspace.reopened"); n != 1 {
		t.Errorf("reopened entries: %d", n)
	}
}

// Closing releases the name, so reopening can collide — decisions/0027. The
// refusal is correct: two live engagements with one name is invisible on a
// switcher.
func TestReopeningCanCollideOnTheNameItReleased(t *testing.T) {
	s := tracedSystem(t)
	org, ws, _, _, ownerAuth, _ := firm(t, s, orgdomain.RoleMember)

	if res := s.post(t, "/v1/workspaces/"+ws.String()+"/close", "", ownerAuth); res.StatusCode != http.StatusNoContent {
		t.Fatalf("close: %d", res.StatusCode)
	}
	// The name is free, so a new engagement may take it.
	res := s.post(t, "/v1/orgs/"+org.String()+"/workspaces", `{"name":"Acme Q3"}`, ownerAuth)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("the released name was not reusable: %d", res.StatusCode)
	}
	s.drain(t)

	if res := s.post(t, "/v1/workspaces/"+ws.String()+"/reopen", "", ownerAuth); res.StatusCode != http.StatusConflict {
		t.Errorf("reopening into a taken name: %d, want 409", res.StatusCode)
	}
}

// Renaming needs admin ON the engagement, by symmetry with granting.
func TestRenamingAnEngagement(t *testing.T) {
	s := tracedSystem(t)
	org, ws, _, kit, ownerAuth, kitAuth := firm(t, s, orgdomain.RoleMember)

	if res := s.put(t, "/v1/workspaces/"+ws.String()+"/members/"+kit.String(),
		`{"level":"write"}`, ownerAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("grant: %d", res.StatusCode)
	}
	// write is not admin.
	if res := s.do(t, http.MethodPatch, "/v1/workspaces/"+ws.String(), `{"name":"Nope"}`, kitAuth); res.StatusCode != http.StatusNotFound {
		t.Errorf("a writer renamed the engagement: %d", res.StatusCode)
	}
	if res := s.do(t, http.MethodPatch, "/v1/workspaces/"+ws.String(), `{"name":"Acme Q4"}`, ownerAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("rename: %d", res.StatusCode)
	}
	if found := workspaceIn(s.me(t, ownerAuth), org.String(), ws.String()); found == nil || found.Name != "Acme Q4" {
		t.Errorf("after renaming: %+v", found)
	}

	// Onto a live sibling's name is a conflict.
	var second workspaceResponse
	decode(t, s.post(t, "/v1/orgs/"+org.String()+"/workspaces", `{"name":"Globex"}`, ownerAuth), &second)
	s.drain(t)
	if res := s.do(t, http.MethodPatch, "/v1/workspaces/"+second.WorkspaceID, `{"name":"acme q4"}`, ownerAuth); res.StatusCode != http.StatusConflict {
		t.Errorf("renaming onto a live sibling: %d, want 409", res.StatusCode)
	}

	s.drain(t)
	ctx := t.Context()
	var from, to string
	if err := s.pool.DB(ctx).QueryRow(ctx,
		`select detail->>'from', detail->>'to' from audit.entry where action = 'workspace.renamed'`,
	).Scan(&from, &to); err != nil {
		t.Fatal(err)
	}
	if from != "Acme Q3" || to != "Acme Q4" {
		t.Errorf("the rename recorded %q -> %q", from, to)
	}
}
