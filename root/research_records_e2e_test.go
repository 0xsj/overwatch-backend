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
	var revisions struct {
		Items []researchRecordRevisionResponse `json:"items"`
	}
	revisionsResponse := s.get(t, base+"/records/"+created.RecordID+"/revisions", auth)
	researchStatus(t, revisionsResponse, http.StatusOK)
	decode(t, revisionsResponse, &revisions)
	if len(revisions.Items) != 2 || revisions.Items[0].Revision != 1 || revisions.Items[0].Name != "@harborline" || revisions.Items[1].Revision != 2 || revisions.Items[1].Name != "Harborline author" {
		t.Fatalf("record revisions after edit: %+v", revisions)
	}
	archivedResponse := s.post(t, base+"/records/"+created.RecordID+"/archive", "", auth)
	researchStatus(t, archivedResponse, http.StatusOK)
	var archived researchRecordResponse
	decode(t, archivedResponse, &archived)
	if archived.ArchivedAt == "" || archived.ArchivedBy == "" {
		t.Fatalf("archived research record: %+v", archived)
	}
	var activePage struct {
		Items []researchRecordResponse `json:"items"`
	}
	activeResponse := s.get(t, base+"/records", auth)
	researchStatus(t, activeResponse, http.StatusOK)
	decode(t, activeResponse, &activePage)
	if len(activePage.Items) != 0 {
		t.Fatalf("archived record remained in active list: %+v", activePage)
	}
	var archivedPage struct {
		Items []researchRecordResponse `json:"items"`
	}
	archivedListResponse := s.get(t, base+"/records?archived=archived", auth)
	researchStatus(t, archivedListResponse, http.StatusOK)
	decode(t, archivedListResponse, &archivedPage)
	if len(archivedPage.Items) != 1 || archivedPage.Items[0].RecordID != created.RecordID {
		t.Fatalf("archived record list: %+v", archivedPage)
	}
	researchStatus(t, s.put(t, base+"/records/"+created.RecordID, researchJSON(t, map[string]any{
		"kind": "place", "name": "East Quay", "observation_ids": []string{observation.ID.String()},
	}), auth), http.StatusConflict)
	researchStatus(t, s.post(t, base+"/records/"+created.RecordID+"/restore", "", auth), http.StatusOK)
	revisionsResponse = s.get(t, base+"/records/"+created.RecordID+"/revisions", auth)
	researchStatus(t, revisionsResponse, http.StatusOK)
	decode(t, revisionsResponse, &revisions)
	if len(revisions.Items) != 4 || revisions.Items[2].ArchivedAt == "" || revisions.Items[3].ArchivedAt != "" {
		t.Fatalf("record lifecycle revisions: %+v", revisions)
	}
	researchStatus(t, s.get(t, base+"/records?archived=unknown", auth), http.StatusBadRequest)

	var page struct {
		Items []researchRecordResponse `json:"items"`
	}
	listedResponse := s.get(t, base+"/records", auth)
	researchStatus(t, listedResponse, http.StatusOK)
	decode(t, listedResponse, &page)
	if len(page.Items) != 1 || page.Items[0].RecordID != created.RecordID {
		t.Fatalf("research record list: %+v", page)
	}
	var searched struct {
		Items []researchRecordResponse `json:"items"`
	}
	searchResponse := s.get(t, base+"/records?q=Harborline&kind=person", auth)
	researchStatus(t, searchResponse, http.StatusOK)
	decode(t, searchResponse, &searched)
	if len(searched.Items) != 1 || searched.Items[0].RecordID != created.RecordID {
		t.Fatalf("filtered research record list: %+v", searched)
	}
	var observationSearched struct {
		Items []researchRecordResponse `json:"items"`
	}
	observationSearchResponse := s.get(t, base+"/records?q=East+Quay&kind=person", auth)
	researchStatus(t, observationSearchResponse, http.StatusOK)
	decode(t, observationSearchResponse, &observationSearched)
	if len(observationSearched.Items) != 1 || observationSearched.Items[0].RecordID != created.RecordID {
		t.Fatalf("observation-context record list: %+v", observationSearched)
	}
	researchStatus(t, s.get(t, base+"/records?kind=unknown", auth), http.StatusBadRequest)
	uncitedResponse := s.post(t, base+"/records", researchJSON(t, map[string]any{
		"kind": "account", "name": "Uncited account", "description": "A second authored record.",
	}), auth)
	researchStatus(t, uncitedResponse, http.StatusCreated)
	var uncited researchRecordResponse
	decode(t, uncitedResponse, &uncited)
	resolutionResponse := s.post(t, base+"/records/"+uncited.RecordID+"/resolutions", researchJSON(t, map[string]any{
		"canonical_record_id": created.RecordID, "rationale": "Review whether these authored records refer to the same account.",
	}), auth)
	researchStatus(t, resolutionResponse, http.StatusCreated)
	var summary researchRecordSummaryResponse
	summaryResponse := s.get(t, base+"/records/summary", auth)
	researchStatus(t, summaryResponse, http.StatusOK)
	decode(t, summaryResponse, &summary)
	if summary.RecordCount != 2 || summary.KindCounts["person"] != 1 || summary.KindCounts["account"] != 1 || summary.CitedRecordCount != 1 || summary.UncitedRecordCount != 1 || summary.CitationCount != 1 || summary.OpenResolutionRecordCount != 2 {
		t.Fatalf("research record summary: %+v", summary)
	}
	var uncitedPage struct {
		Items []researchRecordResponse `json:"items"`
	}
	uncitedFilterResponse := s.get(t, base+"/records?citation=uncited", auth)
	researchStatus(t, uncitedFilterResponse, http.StatusOK)
	decode(t, uncitedFilterResponse, &uncitedPage)
	if len(uncitedPage.Items) != 1 || uncitedPage.Items[0].RecordID != uncited.RecordID {
		t.Fatalf("uncited record filter: %+v", uncitedPage)
	}
	var openResolutionPage struct {
		Items []researchRecordResponse `json:"items"`
	}
	openResolutionResponse := s.get(t, base+"/records?resolution=open", auth)
	researchStatus(t, openResolutionResponse, http.StatusOK)
	decode(t, openResolutionResponse, &openResolutionPage)
	if len(openResolutionPage.Items) != 2 {
		t.Fatalf("open resolution record filter: %+v", openResolutionPage)
	}
	researchStatus(t, s.get(t, base+"/records?resolution=unknown", auth), http.StatusBadRequest)

	researchStatus(t, s.post(t, base+"/close", "", auth), http.StatusNoContent)
	researchStatus(t, s.get(t, base+"/records/"+created.RecordID, auth), http.StatusOK)
	researchStatus(t, s.put(t, base+"/records/"+created.RecordID, researchJSON(t, map[string]any{
		"kind": "place", "name": "East Quay", "observation_ids": []string{observation.ID.String()},
	}), auth), http.StatusConflict)
}
