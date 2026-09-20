package root

import (
	"net/http"
	"testing"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
)

func TestResearchResolutionSetRequiresOneDecisionAndPreservesAliases(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	source := addResearchSource(t, s, workspace, auth, "Two authored handles point to the same working subject.")
	base := "/v1/workspaces/" + workspace.String()
	firstObservation := recordResearchObservation(t, s, base+"/sources/"+source.ID.String(), auth, source.LatestCapture.ID, "@harborline")
	secondObservation := recordResearchObservation(t, s, base+"/sources/"+source.ID.String(), auth, source.LatestCapture.ID, "harborline author")

	canonicalResponse := s.post(t, base+"/records", researchJSON(t, map[string]any{
		"kind": "person", "name": "Harborline author", "description": "Canonical working record.", "observation_ids": []string{},
	}), auth)
	researchStatus(t, canonicalResponse, http.StatusCreated)
	var canonical researchRecordResponse
	decode(t, canonicalResponse, &canonical)

	aliasOneResponse := s.post(t, base+"/records", researchJSON(t, map[string]any{
		"kind": "person", "name": "@harborline", "description": "Handle record.", "observation_ids": []string{firstObservation.ID.String()},
	}), auth)
	researchStatus(t, aliasOneResponse, http.StatusCreated)
	var aliasOne researchRecordResponse
	decode(t, aliasOneResponse, &aliasOne)
	aliasTwoResponse := s.post(t, base+"/records", researchJSON(t, map[string]any{
		"kind": "person", "name": "harborline author", "description": "Name record.", "observation_ids": []string{secondObservation.ID.String()},
	}), auth)
	researchStatus(t, aliasTwoResponse, http.StatusCreated)
	var aliasTwo researchRecordResponse
	decode(t, aliasTwoResponse, &aliasTwo)

	proposalResponse := s.post(t, base+"/resolution-sets", researchJSON(t, map[string]any{
		"canonical_record_id": canonical.RecordID,
		"alias_record_ids":    []string{aliasOne.RecordID, aliasTwo.RecordID},
		"rationale":           "The aliases are supported by distinct retained observations and need one explicit identity decision.",
	}), auth)
	researchStatus(t, proposalResponse, http.StatusCreated)
	var proposal researchResolutionSetResponse
	decode(t, proposalResponse, &proposal)
	if proposal.State != "proposed" || len(proposal.AliasRecordIDs) != 2 || len(proposal.AddedObservationIDs) != 2 || len(proposal.CanonicalObservationIDsAfter) != 2 {
		t.Fatalf("proposal: %+v", proposal)
	}

	acceptedResponse := s.put(t, base+"/resolution-sets/"+proposal.ResolutionSetID, researchJSON(t, map[string]any{"decision": "accept"}), auth)
	researchStatus(t, acceptedResponse, http.StatusOK)
	var accepted researchResolutionSetResponse
	decode(t, acceptedResponse, &accepted)
	if accepted.State != "accepted" || len(accepted.CanonicalObservationIDsAfter) != 2 {
		t.Fatalf("accepted: %+v", accepted)
	}

	canonicalAfter := s.get(t, base+"/records/"+canonical.RecordID, auth)
	researchStatus(t, canonicalAfter, http.StatusOK)
	var canonicalRecord researchRecordResponse
	decode(t, canonicalAfter, &canonicalRecord)
	if len(canonicalRecord.ObservationIDs) != 2 {
		t.Fatalf("canonical citations after acceptance: %+v", canonicalRecord)
	}
	for _, aliasID := range []string{aliasOne.RecordID, aliasTwo.RecordID} {
		aliasAfter := s.get(t, base+"/records/"+aliasID, auth)
		researchStatus(t, aliasAfter, http.StatusOK)
		var alias researchRecordResponse
		decode(t, aliasAfter, &alias)
		if len(alias.ObservationIDs) != 1 {
			t.Fatalf("alias record was rewritten: %+v", alias)
		}
	}

	reversedResponse := s.post(t, base+"/resolution-sets/"+proposal.ResolutionSetID+"/reverse", "", auth)
	researchStatus(t, reversedResponse, http.StatusOK)
	var reversed researchResolutionSetResponse
	decode(t, reversedResponse, &reversed)
	if reversed.State != "reversed" {
		t.Fatalf("reversed: %+v", reversed)
	}
	canonicalAfterReverse := s.get(t, base+"/records/"+canonical.RecordID, auth)
	researchStatus(t, canonicalAfterReverse, http.StatusOK)
	decode(t, canonicalAfterReverse, &canonicalRecord)
	if len(canonicalRecord.ObservationIDs) != 0 {
		t.Fatalf("reversal removed too little: %+v", canonicalRecord)
	}
}
