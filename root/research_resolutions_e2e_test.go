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

	researchStatus(t, s.put(t, base+"/resolutions/"+proposal.ResolutionID, researchJSON(t, map[string]any{"decision": "accept"}), auth), http.StatusConflict)
}
