package root

import (
	"net/http"
	"strings"
	"testing"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
)

func TestResearchConnectionsKeepAssessmentAndEvidenceExplicit(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	source := addResearchSource(t, s, workspace, auth, "The account may belong to the named author.")
	base := "/v1/workspaces/" + workspace.String()
	observation := recordResearchObservation(t, s, base+"/sources/"+source.ID.String(), auth, source.LatestCapture.ID, "may belong")

	accountResponse := s.post(t, base+"/records", researchJSON(t, map[string]any{
		"kind": "account", "name": "@harborline", "description": "A working account record supported by the source.", "observation_ids": []string{observation.ID.String()},
	}), auth)
	researchStatus(t, accountResponse, http.StatusCreated)
	var account researchRecordResponse
	decode(t, accountResponse, &account)

	personResponse := s.post(t, base+"/records", researchJSON(t, map[string]any{
		"kind": "person", "name": "Harborline author", "description": "A working person record for the named author.", "observation_ids": []string{observation.ID.String()},
	}), auth)
	researchStatus(t, personResponse, http.StatusCreated)
	var person researchRecordResponse
	decode(t, personResponse, &person)

	createdResponse := s.post(t, base+"/connections", researchJSON(t, map[string]any{
		"from_record_id":             account.RecordID,
		"to_record_id":               person.RecordID,
		"kind":                       "may_belong_to",
		"state":                      "proposed",
		"rationale":                  "The account uses the same distinctive handle, but the source does not establish control.",
		"supporting_observation_ids": []string{observation.ID.String()},
		"opposing_observation_ids":   []string{},
	}), auth)
	researchStatus(t, createdResponse, http.StatusCreated)
	var created researchConnectionResponse
	decode(t, createdResponse, &created)
	if created.Kind != "may_belong_to" || created.State != "proposed" || len(created.SupportingObservationIDs) != 1 || len(created.OpposingObservationIDs) != 0 || !created.ReviewFlags.Open || created.ReviewFlags.Conflicted || created.ReviewFlags.Uncited {
		t.Fatalf("created research connection: %+v", created)
	}

	accountUpdate := s.put(t, base+"/records/"+account.RecordID, researchJSON(t, map[string]any{
		"kind": "account", "name": "@harborline", "description": "The current account description was later refined.", "observation_ids": []string{},
	}), auth)
	researchStatus(t, accountUpdate, http.StatusOK)
	personUpdate := s.put(t, base+"/records/"+person.RecordID, researchJSON(t, map[string]any{
		"kind": "person", "name": "Harborline author", "description": "The current person description was later refined.", "observation_ids": []string{},
	}), auth)
	researchStatus(t, personUpdate, http.StatusOK)

	listedResponse := s.get(t, base+"/connections", auth)
	researchStatus(t, listedResponse, http.StatusOK)
	var page struct {
		Items []researchConnectionResponse `json:"items"`
	}
	decode(t, listedResponse, &page)
	if len(page.Items) != 1 || page.Items[0].ConnectionID != created.ConnectionID {
		t.Fatalf("research connection list: %+v", page)
	}
	var searched struct {
		Items []researchConnectionResponse `json:"items"`
	}
	decode(t, s.get(t, base+"/connections?q=Harborline+author", auth), &searched)
	if len(searched.Items) != 1 || searched.Items[0].ConnectionID != created.ConnectionID {
		t.Fatalf("endpoint-name connection search: %+v", searched)
	}
	searched.Items = nil
	decode(t, s.get(t, base+"/connections?q=control", auth), &searched)
	if len(searched.Items) != 1 || searched.Items[0].ConnectionID != created.ConnectionID {
		t.Fatalf("rationale connection search: %+v", searched)
	}

	updatedResponse := s.put(t, base+"/connections/"+created.ConnectionID, researchJSON(t, map[string]any{
		"kind":                       "may_belong_to",
		"state":                      "deferred",
		"rationale":                  "Keep this as a lead until an independent source distinguishes account control from shared naming.",
		"supporting_observation_ids": []string{observation.ID.String()},
		"opposing_observation_ids":   []string{},
	}), auth)
	researchStatus(t, updatedResponse, http.StatusOK)
	var updated researchConnectionResponse
	decode(t, updatedResponse, &updated)
	if updated.State != "deferred" || updated.UpdatedBy == "" || !updated.ReviewFlags.Open || updated.ReviewFlags.Conflicted || updated.ReviewFlags.Uncited {
		t.Fatalf("updated research connection: %+v", updated)
	}
	var filtered struct {
		Items []researchConnectionResponse `json:"items"`
	}
	decode(t, s.get(t, base+"/connections?state=deferred", auth), &filtered)
	if len(filtered.Items) != 1 || filtered.Items[0].ConnectionID != created.ConnectionID {
		t.Fatalf("state-filtered research connections: %+v", filtered)
	}
	filtered.Items = nil
	decode(t, s.get(t, base+"/connections?review=open", auth), &filtered)
	if len(filtered.Items) != 1 || filtered.Items[0].State != "deferred" {
		t.Fatalf("open research connections: %+v", filtered)
	}
	filtered.Items = nil
	decode(t, s.get(t, base+"/connections?kind=may_belong_to", auth), &filtered)
	if len(filtered.Items) != 1 || filtered.Items[0].Kind != "may_belong_to" {
		t.Fatalf("kind-filtered research connections: %+v", filtered)
	}
	filtered.Items = nil
	decode(t, s.get(t, base+"/connections?record_kind=person", auth), &filtered)
	if len(filtered.Items) != 1 || filtered.Items[0].ConnectionID != created.ConnectionID {
		t.Fatalf("record-kind-filtered research connections: %+v", filtered)
	}
	researchStatus(t, s.get(t, base+"/connections?state=unknown", auth), http.StatusBadRequest)
	researchStatus(t, s.get(t, base+"/connections?kind=unknown", auth), http.StatusBadRequest)
	researchStatus(t, s.get(t, base+"/connections?record_kind=unknown", auth), http.StatusBadRequest)
	researchStatus(t, s.get(t, base+"/connections?review=unknown", auth), http.StatusBadRequest)
	researchStatus(t, s.get(t, base+"/connections?q="+strings.Repeat("x", 201), auth), http.StatusBadRequest)
	var summary researchConnectionSummaryResponse
	decode(t, s.get(t, base+"/connections/summary", auth), &summary)
	if summary.ConnectionCount != 1 || summary.StateCounts["deferred"] != 1 || summary.OpenCount != 1 || summary.ConflictedCount != 0 || summary.UncitedCount != 0 {
		t.Fatalf("research connection summary: %+v", summary)
	}

	historyResponse := s.get(t, base+"/connections/"+created.ConnectionID+"/revisions", auth)
	researchStatus(t, historyResponse, http.StatusOK)
	var history struct {
		Items []researchConnectionRevisionResponse `json:"items"`
	}
	decode(t, historyResponse, &history)
	if len(history.Items) != 2 || history.Items[0].Revision != 1 || history.Items[0].State != "proposed" || history.Items[0].FromRecordName != "@harborline" || history.Items[0].FromRecordKind != "account" || history.Items[0].FromRecordDescription != "A working account record supported by the source." || len(history.Items[0].FromRecordObservationIDs) != 1 || history.Items[0].ToRecordName != "Harborline author" || history.Items[0].ToRecordKind != "person" || history.Items[0].ToRecordDescription != "A working person record for the named author." || len(history.Items[0].ToRecordObservationIDs) != 1 || history.Items[1].Revision != 2 || history.Items[1].State != "deferred" || history.Items[1].Rationale != updated.Rationale || history.Items[1].FromRecordName != "@harborline" || history.Items[1].FromRecordDescription != "The current account description was later refined." || len(history.Items[1].FromRecordObservationIDs) != 0 {
		t.Fatalf("connection revision history: %+v", history.Items)
	}
	var detail researchConnectionRevisionResponse
	decode(t, s.get(t, base+"/connections/"+created.ConnectionID+"/revisions/"+history.Items[0].RevisionID, auth), &detail)
	if detail.Revision != 1 || detail.State != "proposed" || detail.FromRecordName != "@harborline" || detail.FromRecordDescription != "A working account record supported by the source." || len(detail.FromRecordObservationIDs) != 1 || detail.ToRecordName != "Harborline author" || detail.ToRecordDescription != "A working person record for the named author." || len(detail.ToRecordObservationIDs) != 1 || detail.Rationale == updated.Rationale {
		t.Fatalf("connection revision detail: %+v", detail)
	}

	reviewResponse := s.post(t, base+"/connections/"+created.ConnectionID+"/reviews", "{}", auth)
	researchStatus(t, reviewResponse, http.StatusCreated)
	var review researchConnectionReviewResponse
	decode(t, reviewResponse, &review)
	if review.ConnectionReviewID == "" || review.ConnectionID != created.ConnectionID || review.ConnectionKind != "may_belong_to" || review.ConnectionState != "deferred" || len(review.SupportingObservationIDs) != 1 || len(review.Findings) < 3 || review.Provider != "local" || review.TemplateVersion != "connection-review-v1" {
		t.Fatalf("connection review: %+v", review)
	}
	for _, finding := range review.Findings {
		if len(finding.ObservationIDs) == 0 {
			t.Fatalf("connection review finding lost citation: %+v", finding)
		}
	}
	reviewsResponse := s.get(t, base+"/connections/"+created.ConnectionID+"/reviews", auth)
	researchStatus(t, reviewsResponse, http.StatusOK)
	var reviews struct {
		Items []researchConnectionReviewResponse `json:"items"`
	}
	decode(t, reviewsResponse, &reviews)
	if len(reviews.Items) != 1 || reviews.Items[0].ConnectionReviewID != review.ConnectionReviewID {
		t.Fatalf("connection review history: %+v", reviews)
	}
	reviewDetailResponse := s.get(t, base+"/connections/"+created.ConnectionID+"/reviews/"+review.ConnectionReviewID, auth)
	researchStatus(t, reviewDetailResponse, http.StatusOK)
	var reviewDetail researchConnectionReviewResponse
	decode(t, reviewDetailResponse, &reviewDetail)
	if reviewDetail.Output == "" || reviewDetail.ConnectionRationale != updated.Rationale {
		t.Fatalf("connection review detail: %+v", reviewDetail)
	}

	researchStatus(t, s.post(t, base+"/close", "", auth), http.StatusNoContent)
	researchStatus(t, s.get(t, base+"/connections/"+created.ConnectionID, auth), http.StatusOK)
	researchStatus(t, s.get(t, base+"/connections/"+created.ConnectionID+"/revisions", auth), http.StatusOK)
	researchStatus(t, s.put(t, base+"/connections/"+created.ConnectionID, researchJSON(t, map[string]any{
		"kind": "may_belong_to", "state": "accepted", "rationale": "This edit should be blocked after close.",
	}), auth), http.StatusConflict)
}
