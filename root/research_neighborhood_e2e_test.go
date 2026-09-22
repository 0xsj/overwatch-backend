package root

import (
	"net/http"
	"testing"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
)

func TestResearchRecordNeighborhoodJoinsAuthoredContext(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	source := addResearchSource(t, s, workspace, auth, "The harborline account met its named author at East Quay.")
	base := "/v1/workspaces/" + workspace.String()
	observation := recordResearchObservation(t, s, base+"/sources/"+source.ID.String(), auth, source.LatestCapture.ID, "met its named author")

	anchorResponse := s.post(t, base+"/records", researchJSON(t, map[string]any{
		"kind": "account", "name": "@harborline", "description": "A working account.", "observation_ids": []string{observation.ID.String()},
	}), auth)
	researchStatus(t, anchorResponse, http.StatusCreated)
	var anchor researchRecordResponse
	decode(t, anchorResponse, &anchor)

	personResponse := s.post(t, base+"/records", researchJSON(t, map[string]any{
		"kind": "person", "name": "Harborline author", "description": "A working person.", "observation_ids": []string{observation.ID.String()},
	}), auth)
	researchStatus(t, personResponse, http.StatusCreated)
	var person researchRecordResponse
	decode(t, personResponse, &person)

	connectionResponse := s.post(t, base+"/connections", researchJSON(t, map[string]any{
		"from_record_id": anchor.RecordID, "to_record_id": person.RecordID, "kind": "associated_with", "state": "proposed",
		"rationale": "The source places the account and person in the same meeting.", "supporting_observation_ids": []string{observation.ID.String()},
	}), auth)
	researchStatus(t, connectionResponse, http.StatusCreated)
	var connection researchConnectionResponse
	decode(t, connectionResponse, &connection)

	eventResponse := s.post(t, base+"/events", researchJSON(t, map[string]any{
		"title": "Harborline meeting", "description": "A reported meeting.", "reported_time": "2026-09-20", "time_precision": "exact", "sort_date": "2026-09-20",
		"location": "East Quay", "observation_ids": []string{observation.ID.String()}, "participant_record_ids": []string{person.RecordID},
	}), auth)
	researchStatus(t, eventResponse, http.StatusCreated)

	neighborhoodResponse := s.get(t, base+"/records/"+anchor.RecordID+"/neighborhood", auth)
	researchStatus(t, neighborhoodResponse, http.StatusOK)
	var neighborhood researchRecordNeighborhoodResponse
	decode(t, neighborhoodResponse, &neighborhood)
	if neighborhood.Record.RecordID != anchor.RecordID || len(neighborhood.Records) != 1 || neighborhood.Records[0].RecordID != person.RecordID {
		t.Fatalf("neighborhood records: %+v", neighborhood)
	}
	if len(neighborhood.Connections) != 1 || neighborhood.Connections[0].ConnectionID != connection.ConnectionID {
		t.Fatalf("neighborhood connections: %+v", neighborhood.Connections)
	}
	if len(neighborhood.Events) != 1 || neighborhood.Events[0].Title != "Harborline meeting" {
		t.Fatalf("neighborhood events: %+v", neighborhood.Events)
	}
	if len(neighborhood.Citations) != 1 || neighborhood.Citations[0].ID.String() != observation.ID.String() || neighborhood.Citations[0].SourceTitle == "" {
		t.Fatalf("neighborhood citations: %+v", neighborhood.Citations)
	}
}
