package root

import (
	"net/http"
	"testing"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
)

func TestResearchRecordsKeepAuthoredKindsAndCitations(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	source := addResearchSource(t, s, workspace, auth, "The public account names East Quay.")
	base := "/v1/workspaces/" + workspace.String()
	observation := recordResearchObservation(t, s, base+"/sources/"+source.ID.String(), auth, source.LatestCapture.ID, "East Quay")

	createdResponse := s.post(t, base+"/records", researchJSON(t, map[string]any{
		"kind": "account", "name": "@harborline", "description": "Possible public account.", "observation_ids": []string{observation.ID.String()},
	}), auth)
	researchStatus(t, createdResponse, http.StatusCreated)
	var created researchRecordResponse
	decode(t, createdResponse, &created)
	if created.RecordID == "" || created.Kind != "account" || len(created.ObservationIDs) != 1 || created.ObservationIDs[0] != observation.ID.String() {
		t.Fatalf("created research record: %+v", created)
	}

	researchStatus(t, s.get(t, base+"/records/"+created.RecordID, auth), http.StatusOK)
	updatedResponse := s.put(t, base+"/records/"+created.RecordID, researchJSON(t, map[string]any{
		"kind": "person", "name": "Harborline author", "description": "Still a provisional association.", "observation_ids": []string{observation.ID.String()},
	}), auth)
	researchStatus(t, updatedResponse, http.StatusOK)
	var updated researchRecordResponse
	decode(t, updatedResponse, &updated)
	if updated.Kind != "person" || updated.Name != "Harborline author" || updated.UpdatedBy == "" {
		t.Fatalf("updated research record: %+v", updated)
	}

	var page struct {
		Items []researchRecordResponse `json:"items"`
	}
	listedResponse := s.get(t, base+"/records", auth)
	researchStatus(t, listedResponse, http.StatusOK)
	decode(t, listedResponse, &page)
	if len(page.Items) != 1 || page.Items[0].RecordID != created.RecordID {
		t.Fatalf("research record list: %+v", page)
	}

	researchStatus(t, s.post(t, base+"/close", "", auth), http.StatusNoContent)
	researchStatus(t, s.get(t, base+"/records/"+created.RecordID, auth), http.StatusOK)
	researchStatus(t, s.put(t, base+"/records/"+created.RecordID, researchJSON(t, map[string]any{
		"kind": "place", "name": "East Quay", "observation_ids": []string{observation.ID.String()},
	}), auth), http.StatusConflict)
}
