package root

import (
	"net/http"
	"testing"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestResearchArtifactsStayInsideTheirWorkspace(t *testing.T) {
	s := tracedSystem(t)
	org, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)

	openedResponse := s.post(t, "/v1/orgs/"+org.String()+"/workspaces", `{"name":"Acme Q4"}`, auth)
	researchStatus(t, openedResponse, http.StatusCreated)
	var opened workspaceResponse
	decode(t, openedResponse, &opened)
	otherWorkspace, err := id.Parse(opened.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	s.drain(t)

	base := "/v1/workspaces/" + workspace.String()
	source := addResearchSource(t, s, workspace, auth, "The account is reported near East Quay.")
	observation := recordResearchObservation(t, s, base+"/sources/"+source.ID.String(), auth, source.LatestCapture.ID, "East Quay")

	fromResponse := s.post(t, base+"/records", researchJSON(t, map[string]any{
		"kind": "account", "name": "@harborline", "description": "Workspace-scoped account.", "observation_ids": []string{observation.ID.String()},
	}), auth)
	researchStatus(t, fromResponse, http.StatusCreated)
	var from researchRecordResponse
	decode(t, fromResponse, &from)

	toResponse := s.post(t, base+"/records", researchJSON(t, map[string]any{
		"kind": "place", "name": "East Quay", "description": "Workspace-scoped place.", "observation_ids": []string{observation.ID.String()},
	}), auth)
	researchStatus(t, toResponse, http.StatusCreated)
	var to researchRecordResponse
	decode(t, toResponse, &to)

	connectionResponse := s.post(t, base+"/connections", researchJSON(t, map[string]any{
		"from_record_id": from.RecordID, "to_record_id": to.RecordID, "kind": "located_at", "state": "proposed",
		"rationale":                  "The source places the account near East Quay; keep the relationship reviewable.",
		"supporting_observation_ids": []string{observation.ID.String()}, "opposing_observation_ids": []string{},
	}), auth)
	researchStatus(t, connectionResponse, http.StatusCreated)
	var connection researchConnectionResponse
	decode(t, connectionResponse, &connection)

	foreignBase := "/v1/workspaces/" + otherWorkspace.String()
	var foreignRecords struct {
		Items []researchRecordResponse `json:"items"`
	}
	foreignRecordsResponse := s.get(t, foreignBase+"/records", auth)
	researchStatus(t, foreignRecordsResponse, http.StatusOK)
	decode(t, foreignRecordsResponse, &foreignRecords)
	if len(foreignRecords.Items) != 0 {
		t.Fatalf("foreign workspace listed records: %+v", foreignRecords.Items)
	}

	for _, path := range []string{
		foreignBase + "/records/" + from.RecordID,
		foreignBase + "/connections/" + connection.ConnectionID,
		foreignBase + "/connections/" + connection.ConnectionID + "/revisions",
	} {
		researchStatus(t, s.get(t, path, auth), http.StatusNotFound)
	}

	// The same authenticated owner can administer both workspaces, so these
	// requests prove tenant scoping rather than merely permission denial.
	researchStatus(t, s.put(t, foreignBase+"/connections/"+connection.ConnectionID, researchJSON(t, map[string]any{
		"kind": "located_at", "state": "accepted", "rationale": "This must remain workspace-scoped.",
	}), auth), http.StatusNotFound)
	researchStatus(t, s.post(t, foreignBase+"/connections", researchJSON(t, map[string]any{
		"from_record_id": from.RecordID, "to_record_id": to.RecordID, "kind": "located_at", "state": "proposed",
		"rationale":                  "Foreign records must not be attachable to another workspace.",
		"supporting_observation_ids": []string{observation.ID.String()}, "opposing_observation_ids": []string{},
	}), auth), http.StatusNotFound)

	var foreignConnections struct {
		Items []researchConnectionResponse `json:"items"`
	}
	foreignConnectionsResponse := s.get(t, foreignBase+"/connections", auth)
	researchStatus(t, foreignConnectionsResponse, http.StatusOK)
	decode(t, foreignConnectionsResponse, &foreignConnections)
	if len(foreignConnections.Items) != 0 {
		t.Fatalf("foreign workspace listed connections: %+v", foreignConnections.Items)
	}
}
