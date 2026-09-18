package root

import (
	"net/http"
	"testing"

	leadquery "github.com/0xsj/overwatch-backend/internal/lead/app/query"
	leaddomain "github.com/0xsj/overwatch-backend/internal/lead/domain"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestResearchQuestionsKeepUncertaintySeparateAndCanResolveIt(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	first := addResearchSource(t, s, workspace, auth, "The first notice names East Quay café.")
	second := addResearchSource(t, s, workspace, auth, "The second notice names East Quay café at 18:00.")
	firstBase := "/v1/workspaces/" + workspace.String() + "/sources/" + first.ID.String()
	secondBase := "/v1/workspaces/" + workspace.String() + "/sources/" + second.ID.String()
	left := recordResearchObservation(t, s, firstBase, auth, first.LatestCapture.ID, "East Quay café")
	right := recordResearchObservation(t, s, secondBase, auth, second.LatestCapture.ID, "East Quay café")

	questionsPath := "/v1/workspaces/" + workspace.String() + "/questions"
	res := s.post(t, questionsPath, researchJSON(t, map[string]any{
		"question": "Which notice is the original account?",
		"context":  "The reports use similar wording and may not be independent.",
		"state":    "open", "observation_ids": []id.ID{right.ID, left.ID},
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var created leaddomain.Question
	decode(t, res, &created)
	if created.State != leaddomain.Open || len(created.ObservationIDs) != 2 ||
		created.ObservationIDs[0] != left.ID || created.ObservationIDs[1] != right.ID {
		t.Fatalf("created question: %+v", created)
	}

	var page leadquery.Page
	res = s.get(t, questionsPath, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &page)
	if len(page.Items) != 1 || page.Items[0].ID != created.ID || page.Items[0].Context == "" {
		t.Fatalf("question list: %+v", page)
	}

	res = s.put(t, questionsPath+"/"+created.ID.String(), researchJSON(t, map[string]any{
		"question": "Which notice is the original account?",
		"context":  "The reports use similar wording and may not be independent.",
		"state":    "answered", "resolution": "The second notice was published later.",
		"observation_ids": []id.ID{left.ID},
	}), auth)
	researchStatus(t, res, http.StatusOK)
	var answered leaddomain.Question
	decode(t, res, &answered)
	if answered.ID != created.ID || answered.Author != created.Author || answered.State != leaddomain.Answered ||
		len(answered.ObservationIDs) != 1 || answered.Resolution == "" {
		t.Fatalf("answered question: %+v", answered)
	}

	researchStatus(t, s.post(t, "/v1/workspaces/"+workspace.String()+"/close", "", auth), http.StatusNoContent)
	researchStatus(t, s.get(t, questionsPath, auth), http.StatusOK)
	researchStatus(t, s.get(t, questionsPath+"/"+created.ID.String(), auth), http.StatusOK)
	researchStatus(t, s.put(t, questionsPath+"/"+created.ID.String(), researchJSON(t, map[string]any{
		"question": "changed", "state": "open", "observation_ids": []id.ID{},
	}), auth), http.StatusConflict)
}

func TestResearchQuestionCannotLinkAnObservationFromOutsideItsWorkspace(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	res := s.post(t, "/v1/workspaces/"+workspace.String()+"/questions", researchJSON(t, map[string]any{
		"question": "Which source should we check next?",
		"state":    "open", "observation_ids": []id.ID{{1}},
	}), auth)
	researchStatus(t, res, http.StatusNotFound)
}
