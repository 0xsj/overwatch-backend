package root

import (
	"net/http"
	"testing"

	assistquery "github.com/0xsj/overwatch-backend/internal/assistance/app/query"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	reviewquery "github.com/0xsj/overwatch-backend/internal/review/app/query"
	reviewdomain "github.com/0xsj/overwatch-backend/internal/review/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestResearchEvidenceReviewListsCitationsAndUpdatesOneUnorderedPair(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	first := addResearchSource(t, s, workspace, auth, "The first notice names East Quay café.")
	second := addResearchSource(t, s, workspace, auth, "The second notice names East Quay café at 18:00.")
	firstBase := "/v1/workspaces/" + workspace.String() + "/sources/" + first.ID.String()
	secondBase := "/v1/workspaces/" + workspace.String() + "/sources/" + second.ID.String()
	left := recordResearchObservation(t, s, firstBase, auth, first.LatestCapture.ID, "East Quay café")
	right := recordResearchObservation(t, s, secondBase, auth, second.LatestCapture.ID, "East Quay café")

	var evidence reviewquery.EvidencePage
	res := s.get(t, "/v1/workspaces/"+workspace.String()+"/evidence", auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &evidence)
	if len(evidence.Items) != 2 || evidence.Items[0].SourceTitle == "" || evidence.Items[1].SourceTitle == "" {
		t.Fatalf("evidence projection: %+v", evidence)
	}
	var direct reviewdomain.Evidence
	res = s.get(t, "/v1/workspaces/"+workspace.String()+"/evidence/"+left.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &direct)
	if direct.ID != left.ID || direct.SourceID != first.ID || direct.CaptureID != first.LatestCapture.ID || direct.Quote == "" {
		t.Fatalf("direct evidence read: %+v", direct)
	}

	relationPath := "/v1/workspaces/" + workspace.String() + "/evidence/relations"
	body := researchJSON(t, map[string]any{
		"left_observation_id": right.ID, "right_observation_id": left.ID,
		"kind": "supports", "rationale": "Both notices place the café at the same location.",
	})
	res = s.put(t, relationPath, body, auth)
	researchStatus(t, res, http.StatusOK)
	var firstDecision reviewdomain.Relation
	decode(t, res, &firstDecision)
	if firstDecision.ID.IsZero() || firstDecision.LeftObservationID != left.ID || firstDecision.RightObservationID != right.ID {
		t.Fatalf("relation was not canonicalised: %+v", firstDecision)
	}

	res = s.put(t, relationPath, researchJSON(t, map[string]any{
		"left_observation_id": left.ID, "right_observation_id": right.ID,
		"kind": "unresolved", "rationale": "The location repeats, but the timing still needs another source.",
	}), auth)
	researchStatus(t, res, http.StatusOK)
	var updated reviewdomain.Relation
	decode(t, res, &updated)
	if updated.ID != firstDecision.ID || updated.Kind != reviewdomain.Unresolved || updated.Rationale == "" {
		t.Fatalf("reverse update created the wrong current decision: %+v", updated)
	}

	var decisions reviewquery.RelationPage
	res = s.get(t, relationPath, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &decisions)
	if len(decisions.Items) != 1 || decisions.Items[0].ID != firstDecision.ID || decisions.Items[0].Kind != reviewdomain.Unresolved {
		t.Fatalf("stored decisions: %+v", decisions)
	}
	researchStatus(t, s.post(t, "/v1/workspaces/"+workspace.String()+"/close", "", auth), http.StatusNoContent)
	researchStatus(t, s.get(t, "/v1/workspaces/"+workspace.String()+"/evidence", auth), http.StatusOK)
	researchStatus(t, s.get(t, relationPath, auth), http.StatusOK)
	researchStatus(t, s.put(t, relationPath, body, auth), http.StatusConflict)
}

func TestResearchEvidenceReviewCannotUseAnUnknownObservation(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	source := addResearchSource(t, s, workspace, auth, "A notice.")
	observation := recordResearchObservation(t, s, "/v1/workspaces/"+workspace.String()+"/sources/"+source.ID.String(), auth, source.LatestCapture.ID, "notice")

	res := s.put(t, "/v1/workspaces/"+workspace.String()+"/evidence/relations", researchJSON(t, map[string]any{
		"left_observation_id": observation.ID, "right_observation_id": id.ID{1},
		"kind": "supports", "rationale": "unknown observation",
	}), auth)
	researchStatus(t, res, http.StatusNotFound)
}

func TestResearchEvidenceSynthesisIsSavedAndResumable(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	first := addResearchSource(t, s, workspace, auth, "The notice names @HarborLine.")
	second := addResearchSource(t, s, workspace, auth, "The notice lists user@example.com.")
	left := recordResearchObservation(t, s, "/v1/workspaces/"+workspace.String()+"/sources/"+first.ID.String(), auth, first.LatestCapture.ID, "@HarborLine")
	right := recordResearchObservation(t, s, "/v1/workspaces/"+workspace.String()+"/sources/"+second.ID.String(), auth, second.LatestCapture.ID, "user@example.com")
	path := "/v1/workspaces/" + workspace.String() + "/evidence/syntheses"
	var empty assistquery.SynthesisPage
	res := s.get(t, path, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &empty)
	if len(empty.Items) != 0 {
		t.Fatalf("unexpected synthesis history: %+v", empty)
	}

	res = s.post(t, path, researchJSON(t, map[string]any{"observation_ids": []id.ID{left.ID, right.ID}}), auth)
	researchStatus(t, res, http.StatusCreated)
	var saved map[string]any
	decode(t, res, &saved)
	if saved["provider"] != "local" || saved["method"] != "selected-observations-v1" || saved["output"] == "" {
		t.Fatalf("saved synthesis: %+v", saved)
	}
	synthesisID, ok := saved["synthesis_id"].(string)
	if !ok || synthesisID == "" {
		t.Fatalf("missing synthesis id: %+v", saved)
	}
	if candidates, ok := saved["candidates"].([]any); !ok || len(candidates) != 2 {
		t.Fatalf("candidate citations were not retained: %+v", saved["candidates"])
	}

	var history assistquery.SynthesisPage
	res = s.get(t, path, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &history)
	if len(history.Items) != 1 || history.Items[0].ID.String() != synthesisID || len(history.Items[0].ObservationIDs) != 2 {
		t.Fatalf("synthesis history: %+v", history)
	}
	var direct map[string]any
	res = s.get(t, path+"/"+synthesisID, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &direct)
	if direct["synthesis_id"] != synthesisID || direct["workspace_id"] != workspace.String() {
		t.Fatalf("direct synthesis read: %+v", direct)
	}
}
