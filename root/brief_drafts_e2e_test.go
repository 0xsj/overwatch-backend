package root

import (
	"net/http"
	"testing"

	assistdomain "github.com/0xsj/overwatch-backend/internal/assistance/domain"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestResearchWorkingBriefDraftProposalPersistsExactCitations(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	source := addResearchSource(t, s, workspace, auth, "The notice places the disruption at East Quay.")
	observation := recordResearchObservation(t, s, "/v1/workspaces/"+workspace.String()+"/sources/"+source.ID.String(), auth, source.LatestCapture.ID, "disruption at East Quay")
	base := "/v1/workspaces/" + workspace.String()
	briefPath := base + "/brief"
	researchStatus(t, s.put(t, briefPath, researchJSON(t, map[string]any{
		"title": "East Quay handoff", "question": "What happened at East Quay?", "current_account": "The notice describes a disruption.",
		"limitations": "Only one source is retained.", "next_steps": "Find an independent account.", "observation_ids": []id.ID{observation.ID},
	}), auth), http.StatusOK)

	draftPath := base + "/brief/drafts"
	res := s.get(t, draftPath, auth)
	researchStatus(t, res, http.StatusOK)
	var before struct {
		Items []assistdomain.BriefDraft `json:"items"`
	}
	decode(t, res, &before)
	if len(before.Items) != 0 {
		t.Fatalf("unexpected existing brief drafts: %+v", before)
	}

	res = s.post(t, draftPath, researchJSON(t, map[string]any{"observation_ids": []id.ID{observation.ID}}), auth)
	researchStatus(t, res, http.StatusCreated)
	var created assistdomain.BriefDraft
	decode(t, res, &created)
	if created.WorkspaceID != workspace || created.Input.BriefID.IsZero() || created.Method != "working-brief-diff-v1" || created.TemplateVersion != "brief-draft-v1" || created.Status != assistdomain.BriefDraftCompleted || len(created.Changes) != 3 {
		t.Fatalf("unexpected brief draft: %+v", created)
	}
	if len(created.Input.ObservationIDs) != 1 || created.Input.ObservationIDs[0] != observation.ID {
		t.Fatalf("draft input lost exact citation: %+v", created.Input)
	}
	for _, change := range created.Changes {
		if len(change.ObservationIDs) != 1 || change.ObservationIDs[0] != observation.ID {
			t.Fatalf("draft change lost exact citation: %+v", change)
		}
	}

	res = s.get(t, draftPath+"/"+created.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	var reread assistdomain.BriefDraft
	decode(t, res, &reread)
	if reread.ID != created.ID || reread.Input.BriefID != created.Input.BriefID || reread.Output != created.Output {
		t.Fatalf("draft reread changed persisted proposal: %+v", reread)
	}
	res = s.get(t, draftPath, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &before)
	if len(before.Items) != 1 || before.Items[0].ID != created.ID {
		t.Fatalf("draft list did not include persisted proposal: %+v", before)
	}
}
