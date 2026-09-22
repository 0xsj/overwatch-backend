package root

import (
	"net/http"
	"testing"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
)

func TestResearchResolutionRequiresConfirmationAndCanBeReversed(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	source := addResearchSource(t, s, workspace, auth, "The account @harborline is used by the named author.")
	base := "/v1/workspaces/" + workspace.String()
	observation := recordResearchObservation(t, s, base+"/sources/"+source.ID.String(), auth, source.LatestCapture.ID, "@harborline")

	canonicalResponse := s.post(t, base+"/records", researchJSON(t, map[string]any{
		"kind": "person", "name": "Harborline author", "description": "The canonical working record.", "observation_ids": []string{},
	}), auth)
	researchStatus(t, canonicalResponse, http.StatusCreated)
	var canonical researchRecordResponse
	decode(t, canonicalResponse, &canonical)
	aliasResponse := s.post(t, base+"/records", researchJSON(t, map[string]any{
		"kind": "person", "name": "@harborline author", "description": "The possible alias record.", "observation_ids": []string{observation.ID.String()},
	}), auth)
	researchStatus(t, aliasResponse, http.StatusCreated)
	var alias researchRecordResponse
	decode(t, aliasResponse, &alias)

	connectionResponse := s.post(t, base+"/connections", researchJSON(t, map[string]any{
		"from_record_id": alias.RecordID, "to_record_id": canonical.RecordID, "kind": "possible_same_subject", "state": "proposed",
		"rationale":                  "The authored records may describe the same subject and need explicit identity review.",
		"supporting_observation_ids": []string{}, "opposing_observation_ids": []string{},
	}), auth)
	researchStatus(t, connectionResponse, http.StatusCreated)
	var connection researchConnectionResponse
	decode(t, connectionResponse, &connection)

	eventResponse := s.post(t, base+"/events", researchJSON(t, map[string]any{
		"title": "Harborline activity", "description": "An event linked to the alias record.", "reported_time": "2025-01-01", "time_precision": "exact", "sort_date": "2025-01-01", "location": "", "observation_ids": []string{}, "participant_record_ids": []string{alias.RecordID},
	}), auth)
	researchStatus(t, eventResponse, http.StatusCreated)
	var event struct {
		ID string `json:"event_id"`
	}
	decode(t, eventResponse, &event)
	if connection.ConnectionID == "" || event.ID == "" {
		t.Fatalf("impact fixtures were not created: connection=%+v event=%+v", connection, event)
	}

	proposalResponse := s.post(t, base+"/records/"+alias.RecordID+"/resolutions", researchJSON(t, map[string]any{
		"canonical_record_id": canonical.RecordID,
		"rationale":           "The same distinctive handle appears in the retained observation; confirm the alias manually.",
	}), auth)
	researchStatus(t, proposalResponse, http.StatusCreated)
	var proposal researchResolutionResponse
	decode(t, proposalResponse, &proposal)
	if proposal.State != "proposed" || len(proposal.AddedObservationIDs) != 1 || len(proposal.CanonicalObservationIDsAfter) != 1 || proposal.AliasRecordID != alias.RecordID || proposal.CanonicalRecordID != canonical.RecordID {
		t.Fatalf("proposal: %+v", proposal)
	}

	var impact struct {
		Connections []struct {
			ID string `json:"connection_id"`
		} `json:"connections"`
		Events []struct {
			ID string `json:"event_id"`
		} `json:"events"`
		Briefs    []any `json:"briefs"`
		Snapshots []any `json:"snapshots"`
	}
	impactResponse := s.get(t, base+"/resolutions/"+proposal.ResolutionID+"/impact", auth)
	researchStatus(t, impactResponse, http.StatusOK)
	decode(t, impactResponse, &impact)
	if len(impact.Connections) != 1 || impact.Connections[0].ID != connection.ConnectionID || len(impact.Events) != 1 || impact.Events[0].ID != event.ID || len(impact.Briefs) != 0 || len(impact.Snapshots) != 0 {
		t.Fatalf("resolution impact: %+v", impact)
	}

	canonicalBefore := s.get(t, base+"/records/"+canonical.RecordID, auth)
	researchStatus(t, canonicalBefore, http.StatusOK)
	var before researchRecordResponse
	decode(t, canonicalBefore, &before)
	if len(before.ObservationIDs) != 0 {
		t.Fatalf("proposal changed canonical citations: %+v", before)
	}

	acceptedResponse := s.put(t, base+"/resolutions/"+proposal.ResolutionID, researchJSON(t, map[string]any{"decision": "accept"}), auth)
	researchStatus(t, acceptedResponse, http.StatusOK)
	var accepted researchResolutionResponse
	decode(t, acceptedResponse, &accepted)
	if accepted.State != "accepted" || len(accepted.AddedObservationIDs) != 1 || len(accepted.CanonicalObservationIDsAfter) != 1 {
		t.Fatalf("accepted: %+v", accepted)
	}

	canonicalAfter := s.get(t, base+"/records/"+canonical.RecordID, auth)
	researchStatus(t, canonicalAfter, http.StatusOK)
	decode(t, canonicalAfter, &before)
	if len(before.ObservationIDs) != 1 || before.ObservationIDs[0] != observation.ID.String() {
		t.Fatalf("canonical citations after confirmation: %+v", before)
	}
	var canonicalRevisions struct {
		Items []researchRecordRevisionResponse `json:"items"`
	}
	revisionsResponse := s.get(t, base+"/records/"+canonical.RecordID+"/revisions", auth)
	researchStatus(t, revisionsResponse, http.StatusOK)
	decode(t, revisionsResponse, &canonicalRevisions)
	if len(canonicalRevisions.Items) != 2 || len(canonicalRevisions.Items[1].ObservationIDs) != 1 {
		t.Fatalf("canonical history after resolution acceptance: %+v", canonicalRevisions)
	}
	aliasAfter := s.get(t, base+"/records/"+alias.RecordID, auth)
	researchStatus(t, aliasAfter, http.StatusOK)
	decode(t, aliasAfter, &before)
	if len(before.ObservationIDs) != 1 || before.ObservationIDs[0] != observation.ID.String() {
		t.Fatalf("alias record was destroyed: %+v", before)
	}

	reversedResponse := s.post(t, base+"/resolutions/"+proposal.ResolutionID+"/reverse", "", auth)
	researchStatus(t, reversedResponse, http.StatusOK)
	var reversed researchResolutionResponse
	decode(t, reversedResponse, &reversed)
	if reversed.State != "reversed" {
		t.Fatalf("reversed: %+v", reversed)
	}
	canonicalAfterReverse := s.get(t, base+"/records/"+canonical.RecordID, auth)
	researchStatus(t, canonicalAfterReverse, http.StatusOK)
	decode(t, canonicalAfterReverse, &before)
	if len(before.ObservationIDs) != 0 {
		t.Fatalf("reversal removed too little or retained merged citations: %+v", before)
	}
	revisionsResponse = s.get(t, base+"/records/"+canonical.RecordID+"/revisions", auth)
	researchStatus(t, revisionsResponse, http.StatusOK)
	decode(t, revisionsResponse, &canonicalRevisions)
	if len(canonicalRevisions.Items) != 3 || len(canonicalRevisions.Items[2].ObservationIDs) != 0 {
		t.Fatalf("canonical history after resolution reversal: %+v", canonicalRevisions)
	}

	researchStatus(t, s.put(t, base+"/resolutions/"+proposal.ResolutionID, researchJSON(t, map[string]any{"decision": "accept"}), auth), http.StatusConflict)
}
