package root

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cleanupdomain "github.com/0xsj/overwatch-backend/internal/artifactcleanup/domain"
	assistquery "github.com/0xsj/overwatch-backend/internal/assistance/app/query"
	assistdomain "github.com/0xsj/overwatch-backend/internal/assistance/domain"
	eventquery "github.com/0xsj/overwatch-backend/internal/event/app/query"
	eventdomain "github.com/0xsj/overwatch-backend/internal/event/domain"
	obsquery "github.com/0xsj/overwatch-backend/internal/observation/app/query"
	obsdomain "github.com/0xsj/overwatch-backend/internal/observation/domain"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	sourcecmd "github.com/0xsj/overwatch-backend/internal/source/app/command"
	sourcequery "github.com/0xsj/overwatch-backend/internal/source/app/query"
	sourcedomain "github.com/0xsj/overwatch-backend/internal/source/domain"
	extractioncmd "github.com/0xsj/overwatch-backend/internal/source/extraction/app/command"
	extractionquery "github.com/0xsj/overwatch-backend/internal/source/extraction/app/query"
	extractiondomain "github.com/0xsj/overwatch-backend/internal/source/extraction/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/testx"
)

func researchJSON(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
func researchStatus(t *testing.T, res *http.Response, want int) {
	t.Helper()
	if res.StatusCode != want {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status=%d want=%d body=%s", res.StatusCode, want, body)
	}
}
func addResearchSource(t *testing.T, s traced, ws id.ID, auth map[string]string, content string) sourcedomain.Summary {
	t.Helper()
	res := s.post(t, "/v1/workspaces/"+ws.String()+"/sources", researchJSON(t, map[string]any{"title": "Transit notice", "origin": "paste", "content": content}), auth)
	researchStatus(t, res, http.StatusCreated)
	var out sourcedomain.Summary
	decode(t, res, &out)
	return out
}
func recordResearchObservation(t *testing.T, s traced, base string, auth map[string]string, capture id.ID, quote string) obsdomain.Manual {
	t.Helper()
	res := s.post(t, base+"/observations", researchJSON(t, map[string]any{"capture_id": capture, "statement": "The notice reports the café location.", "quote": quote}), auth)
	researchStatus(t, res, http.StatusCreated)
	var out obsdomain.Manual
	decode(t, res, &out)
	return out
}

func TestResearchSourceCitationKeepsTheExactCaptureAfterANewVersion(t *testing.T) {
	s := tracedSystem(t)
	_, ws, owner, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	content := "📍 East Quay café — interruption at 18:00.\n" + strings.Repeat("Additional retained material.\n", 600)
	source := addResearchSource(t, s, ws, auth, content)
	if source.LatestCapture == nil || source.LatestCapture.Version != 1 {
		t.Fatalf("missing first capture: %+v", source)
	}
	base := "/v1/workspaces/" + ws.String() + "/sources/" + source.ID.String()
	quote := "East Quay café"
	observation := recordResearchObservation(t, s, base, auth, source.LatestCapture.ID, quote)
	var exact obsdomain.Manual
	exactResponse := s.get(t, base+"/observations/"+observation.ID.String(), auth)
	researchStatus(t, exactResponse, http.StatusOK)
	decode(t, exactResponse, &exact)
	if exact.ID != observation.ID || exact.CaptureID != source.LatestCapture.ID {
		t.Fatal("direct citation lookup returned a different observation")
	}

	if observation.QuoteStart != 2 || observation.QuoteEnd != 16 || observation.Author != owner || observation.Origin != "manual" {
		t.Fatalf("incorrect Unicode citation or authorship: %+v", observation)
	}
	res := s.post(t, base+"/captures", researchJSON(t, map[string]string{"content": "A revised notice naming a different location.", "media_type": "text/plain"}), auth)
	researchStatus(t, res, http.StatusCreated)
	var revised sourcedomain.Capture
	decode(t, res, &revised)
	if revised.Version != 2 || revised.ID == source.LatestCapture.ID {
		t.Fatal("capture append replaced the old version")
	}
	var detail sourcequery.Detail
	res = s.get(t, base, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &detail)
	if len(detail.Captures) != 2 || detail.Source.LatestCapture.ID != revised.ID {
		t.Fatalf("incorrect source history: %+v", detail)
	}
	var retained sourcequery.Captured
	res = s.get(t, base+"/captures/"+source.LatestCapture.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &retained)
	if retained.Content != content || retained.SHA256 != source.LatestCapture.SHA256 {
		t.Fatal("original cited bytes changed")
	}
	var observations obsquery.ManualPage
	res = s.get(t, base+"/observations", auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &observations)
	if len(observations.Items) != 1 || observations.Items[0].CaptureID != source.LatestCapture.ID || observations.Items[0].Quote != quote {
		t.Fatalf("citation moved to another version: %+v", observations)
	}
	if n := testx.Count(t, s.pool, "select count(*) from run.invocation"); n != 0 {
		t.Fatalf("source intake invented %d invocations", n)
	}
}

func TestResearchCitationShareIsRecipientSafeAndRevocable(t *testing.T) {
	s := tracedSystem(t)
	org, ws, _, client, ownerAuth, clientAuth := firm(t, s, orgdomain.RoleClient)
	s.grant(t, org, client, ws, orgdomain.LevelRead)
	source := addResearchSource(t, s, ws, ownerAuth, "A public notice with a private retained body.")
	base := "/v1/workspaces/" + ws.String() + "/sources/" + source.ID.String()
	observation := recordResearchObservation(t, s, base, ownerAuth, source.LatestCapture.ID, "public notice")

	res := s.post(t, base+"/observations/"+observation.ID.String()+"/shares", `{}`, ownerAuth)
	researchStatus(t, res, http.StatusCreated)
	var created struct {
		ShareID string `json:"share_id"`
		Token   string `json:"token"`
	}
	decode(t, res, &created)
	if created.ShareID == "" || created.Token == "" {
		t.Fatalf("share did not return an opaque token once: %+v", created)
	}

	res = s.get(t, base+"/observations/"+observation.ID.String()+"/shares", ownerAuth)
	researchStatus(t, res, http.StatusOK)
	var listed []map[string]any
	decode(t, res, &listed)
	if len(listed) != 1 {
		t.Fatalf("share list=%v", listed)
	}
	if _, exists := listed[0]["token"]; exists {
		t.Fatal("share list returned the raw token after creation")
	}

	res = s.get(t, "/v1/workspaces/"+ws.String()+"/observations/shared/"+created.Token, clientAuth)
	researchStatus(t, res, http.StatusOK)
	var safe map[string]any
	decode(t, res, &safe)
	for _, forbidden := range []string{"source_id", "observation_id", "capture_id", "author", "raw_source_bytes"} {
		if _, exists := safe[forbidden]; exists {
			t.Fatalf("recipient projection leaked %q: %v", forbidden, safe)
		}
	}
	if safe["statement"] != observation.Statement || safe["quote"] != observation.Quote || safe["source_title"] != source.Title {
		t.Fatalf("recipient projection lost citation context: %v", safe)
	}
	if res = s.get(t, base+"/observations/"+observation.ID.String()+"/shares", clientAuth); res.StatusCode != http.StatusNotFound {
		t.Fatalf("client reached internal share management: %d", res.StatusCode)
	}

	researchStatus(t, s.post(t, "/v1/workspaces/"+ws.String()+"/sources/shares/"+created.ShareID+"/revoke", `{}`, ownerAuth), http.StatusOK)
	if res = s.get(t, "/v1/workspaces/"+ws.String()+"/observations/shared/"+created.Token, clientAuth); res.StatusCode != http.StatusNotFound {
		t.Fatalf("revoked citation share remained readable: %d", res.StatusCode)
	}
}

func TestResearchSourceRetentionScheduleIsAuditableAndClearable(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	source := addResearchSource(t, s, ws, auth, "A retained notice.")
	base := "/v1/workspaces/" + ws.String() + "/sources/" + source.ID.String()
	want := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	res := s.put(t, base+"/retention", researchJSON(t, map[string]any{"retention_until": want.Format(time.RFC3339)}), auth)
	researchStatus(t, res, http.StatusOK)
	var detail sourcequery.Detail
	decode(t, res, &detail)
	if detail.Source.RetentionUntil == nil || !detail.Source.RetentionUntil.Equal(want) || detail.Source.RetentionUpdatedBy.IsZero() {
		t.Fatalf("retention schedule was not persisted with author: %+v", detail.Source)
	}
	res = s.put(t, base+"/retention", `{"retention_until":null}`, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &detail)
	if detail.Source.RetentionUntil != nil || detail.Source.RetentionUpdatedBy.IsZero() {
		t.Fatalf("retention schedule was not clearable: %+v", detail.Source)
	}
}

func TestResearchSourcePublicationMetadataIsCorrectableWithoutChangingCaptures(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	source := addResearchSource(t, s, ws, auth, "A notice with a publication date.")
	base := "/v1/workspaces/" + ws.String() + "/sources/" + source.ID.String()
	want := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	res := s.put(t, base+"/publication", researchJSON(t, map[string]any{"published_at": want.Format(time.RFC3339Nano)}), auth)
	researchStatus(t, res, http.StatusOK)
	var detail sourcequery.Detail
	decode(t, res, &detail)
	if detail.Source.PublishedAt == nil || !detail.Source.PublishedAt.Equal(want) || detail.Source.LatestCapture == nil {
		t.Fatalf("publication metadata was not persisted: %+v", detail.Source)
	}
	captureID := detail.Source.LatestCapture.ID
	res = s.put(t, base+"/publication", `{"published_at":null}`, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &detail)
	if detail.Source.PublishedAt != nil || detail.Source.LatestCapture == nil || detail.Source.LatestCapture.ID != captureID {
		t.Fatalf("publication clear changed retained capture context: %+v", detail)
	}
}

func TestResearchSourceDuplicatePolicyCanBlockIdenticalCapture(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	content := "A notice whose bytes repeat."
	source := addResearchSource(t, s, ws, auth, content)
	base := "/v1/workspaces/" + ws.String() + "/sources/" + source.ID.String()
	res := s.put(t, base+"/duplicate-policy", researchJSON(t, map[string]any{"duplicate_policy": "block"}), auth)
	researchStatus(t, res, http.StatusOK)
	var detail sourcequery.Detail
	decode(t, res, &detail)
	if detail.Source.DuplicatePolicy != sourcedomain.DuplicatePolicyBlock {
		t.Fatalf("duplicate policy was not persisted: %+v", detail.Source)
	}
	res = s.post(t, base+"/captures", researchJSON(t, map[string]string{"content": content, "media_type": "text/plain"}), auth)
	researchStatus(t, res, http.StatusConflict)
	res = s.put(t, base+"/duplicate-policy", researchJSON(t, map[string]any{"duplicate_policy": "allow"}), auth)
	researchStatus(t, res, http.StatusOK)
	res = s.post(t, base+"/captures", researchJSON(t, map[string]string{"content": content, "media_type": "text/plain"}), auth)
	researchStatus(t, res, http.StatusCreated)
	res = s.get(t, base, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &detail)
	if len(detail.Captures) != 2 || detail.Captures[0].SHA256 != detail.Captures[1].SHA256 {
		t.Fatalf("allow policy did not retain duplicate history: %+v", detail.Captures)
	}
}

func TestResearchSourceIntakeRequiresReviewBeforeReferenceCreation(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	path := "/v1/workspaces/" + ws.String() + "/source-intake"
	res := s.post(t, path, researchJSON(t, map[string]string{"title": "Harbor bulletin", "url": "https://example.test/harbor", "note": "Discovered in a monitored public feed."}), auth)
	researchStatus(t, res, http.StatusCreated)
	var candidate sourcedomain.IntakeCandidate
	decode(t, res, &candidate)
	if candidate.Status != sourcedomain.IntakePending || candidate.SourceID != (id.ID{}) {
		t.Fatalf("candidate was retained as a source before review: %+v", candidate)
	}
	res = s.get(t, path+"?status=pending", auth)
	researchStatus(t, res, http.StatusOK)
	var queue sourcequery.IntakePage
	decode(t, res, &queue)
	if len(queue.Items) != 1 || queue.Items[0].ID != candidate.ID {
		t.Fatalf("pending intake queue=%+v", queue)
	}
	res = s.put(t, path+"/"+candidate.ID.String()+"/review", researchJSON(t, map[string]string{"decision": "approved", "note": "Relevant to the active investigation."}), auth)
	researchStatus(t, res, http.StatusOK)
	var result sourcecmd.IntakeReviewResult
	decode(t, res, &result)
	if result.Source == nil || result.Source.Origin != "reference" || result.Source.URL != candidate.URL || result.Candidate.Status != sourcedomain.IntakeApproved || result.Candidate.SourceID != result.Source.ID {
		t.Fatalf("approved intake did not create a linked reference: %+v", result)
	}
	var detail sourcequery.Detail
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/sources/"+result.Source.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &detail)
	if detail.Source.Origin != "reference" || len(detail.Captures) != 0 {
		t.Fatalf("approval retained bytes unexpectedly: %+v", detail)
	}
	res = s.put(t, path+"/"+candidate.ID.String()+"/review", researchJSON(t, map[string]string{"decision": "rejected", "note": "The candidate is no longer needed."}), auth)
	researchStatus(t, res, http.StatusConflict)
}

func TestResearchImportedIntakeRetainsFileOnlyAfterApproval(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	path := "/v1/workspaces/" + ws.String() + "/source-intake"
	content := []byte("%PDF-1.7\nretained only after review")
	res := s.post(t, path, researchJSON(t, map[string]any{"origin": "import", "title": "Notice PDF", "filename": "notice.pdf", "media_type": "application/pdf", "content_base64": base64.StdEncoding.EncodeToString(content), "note": "Imported from a reviewed case bundle."}), auth)
	researchStatus(t, res, http.StatusCreated)
	var candidate sourcedomain.IntakeCandidate
	decode(t, res, &candidate)
	if candidate.Origin != sourcedomain.IntakeImport || candidate.Filename != "notice.pdf" || candidate.Status != sourcedomain.IntakePending {
		t.Fatalf("import candidate: %+v", candidate)
	}
	res = s.get(t, path+"/"+candidate.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &candidate)
	if candidate.ContentBytes != nil {
		t.Fatal("staged import bytes leaked through the intake response")
	}
	res = s.put(t, path+"/"+candidate.ID.String()+"/review", researchJSON(t, map[string]string{"decision": "approved", "note": "The imported document belongs to this investigation."}), auth)
	researchStatus(t, res, http.StatusOK)
	var result sourcecmd.IntakeReviewResult
	decode(t, res, &result)
	if result.Source == nil || result.Source.Origin != sourcedomain.IntakeImport || result.Source.LatestCapture == nil || result.Source.LatestCapture.MediaType != "application/pdf" {
		t.Fatalf("approved import did not create capture: %+v", result)
	}
	var captured sourcequery.Captured
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/sources/"+result.Source.ID.String()+"/captures/"+result.Source.LatestCapture.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &captured)
	if captured.ContentBase64 != base64.StdEncoding.EncodeToString(content) {
		t.Fatalf("approved import changed bytes: %+v", captured)
	}
}

func TestResearchSourcePrivacyReviewAndExplicitPurgeRevokeBytes(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	source := addResearchSource(t, s, ws, auth, "A source that can be purged.")
	base := "/v1/workspaces/" + ws.String() + "/sources/" + source.ID.String()
	researchStatus(t, s.put(t, base+"/retention", researchJSON(t, map[string]any{"retention_until": "2026-01-01T00:00:00Z"}), auth), http.StatusOK)
	privacy := s.put(t, base+"/privacy", researchJSON(t, map[string]any{"sensitivity": "restricted", "legal_hold": true, "legal_hold_reason": "Preserve for the active review."}), auth)
	researchStatus(t, privacy, http.StatusOK)

	var review sourcedomain.PurgeReview
	researchStatus(t, s.get(t, base+"/retention-review", auth), http.StatusOK)
	res := s.get(t, base+"/retention-review", auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &review)
	if review.State != sourcedomain.RetentionHeld || review.Eligible || len(review.Blockers) == 0 || review.Blockers[0] != "legal_hold" {
		t.Fatalf("legal hold did not block purge review: %+v", review)
	}

	researchStatus(t, s.put(t, base+"/privacy", researchJSON(t, map[string]any{"sensitivity": "restricted", "legal_hold": false, "legal_hold_reason": ""}), auth), http.StatusOK)
	res = s.get(t, base+"/retention-review", auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &review)
	if review.State != sourcedomain.RetentionDue || !review.Eligible || len(review.Blockers) != 0 {
		t.Fatalf("eligible purge review was not exposed: %+v", review)
	}

	res = s.post(t, base+"/purge", researchJSON(t, map[string]any{"confirm": true, "reason": "Retention expired; no downstream research dependency remains."}), auth)
	researchStatus(t, res, http.StatusOK)
	var purged sourcecmd.PurgeResult
	decode(t, res, &purged)
	if !purged.Purged {
		t.Fatalf("explicit purge did not complete: %+v", purged)
	}
	var detail sourcequery.Detail
	res = s.get(t, base, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &detail)
	if detail.Source.PurgedAt == nil || detail.Source.PurgedBy.IsZero() || detail.Source.PurgeReason == "" || detail.Source.Sensitivity != sourcedomain.SensitivityRestricted {
		t.Fatalf("purge/privacy metadata was not retained: %+v", detail.Source)
	}
	res = s.get(t, base+"/captures/"+source.LatestCapture.ID.String(), auth)
	researchStatus(t, res, http.StatusConflict)
	res = s.post(t, base+"/captures", researchJSON(t, map[string]string{"content": "new bytes", "media_type": "text/plain"}), auth)
	researchStatus(t, res, http.StatusConflict)
}

func TestResearchRetentionCleanupOnlyRemovesVerifiedUnreferencedBytes(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, ownerAuth, _ := firm(t, s, orgdomain.RoleMember)
	source := addResearchSource(t, s, ws, ownerAuth, "A source whose bytes can be swept.")
	shared := addResearchSource(t, s, ws, ownerAuth, "A source whose bytes can be swept.")
	base := "/v1/workspaces/" + ws.String() + "/sources/" + source.ID.String()
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	researchStatus(t, s.put(t, base+"/retention", researchJSON(t, map[string]any{"retention_until": past}), ownerAuth), http.StatusOK)
	researchStatus(t, s.post(t, base+"/purge", researchJSON(t, map[string]any{"confirm": true, "reason": "Retention expired and no downstream research dependency remains."}), ownerAuth), http.StatusOK)
	var lifecycle cleanupdomain.StatusPage
	res := s.get(t, "/v1/workspaces/"+ws.String()+"/retention-cleanup/status", ownerAuth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &lifecycle)
	if lifecycle.StateCounts[cleanupdomain.StateProtected] != 1 || lifecycle.Items[0].Reason != "Referenced by a live source." {
		t.Fatalf("live duplicate was not explained as protected: %+v", lifecycle)
	}

	var inventory cleanupdomain.Inventory
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/retention-cleanup", ownerAuth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &inventory)
	if inventory.CandidateCount != 0 || len(inventory.Items) != 0 {
		t.Fatalf("a live duplicate source did not protect the shared blob: %+v", inventory)
	}
	sharedBase := "/v1/workspaces/" + ws.String() + "/sources/" + shared.ID.String()
	researchStatus(t, s.put(t, sharedBase+"/retention", researchJSON(t, map[string]any{"retention_until": past}), ownerAuth), http.StatusOK)
	researchStatus(t, s.post(t, sharedBase+"/purge", researchJSON(t, map[string]any{"confirm": true, "reason": "Retention expired and no downstream research dependency remains."}), ownerAuth), http.StatusOK)
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/retention-cleanup", ownerAuth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &inventory)
	if inventory.CandidateCount != 1 || len(inventory.Items) != 1 || inventory.Items[0].Kind != cleanupdomain.KindSourceCapture {
		t.Fatalf("unexpected cleanup inventory: %+v", inventory)
	}
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/retention-cleanup/status", ownerAuth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &lifecycle)
	if lifecycle.StateCounts[cleanupdomain.StateEligible] != 1 || lifecycle.Items[0].State != cleanupdomain.StateEligible {
		t.Fatalf("eligible lifecycle was not explained: %+v", lifecycle)
	}
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/retention-cleanup/status?state=eligible&ref="+inventory.Items[0].Ref, ownerAuth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &lifecycle)
	if lifecycle.Count != 1 || lifecycle.Items[0].Ref != inventory.Items[0].Ref {
		t.Fatalf("exact lifecycle filter failed: %+v", lifecycle)
	}
	var cleanupReview cleanupdomain.Review
	res = s.put(t, "/v1/workspaces/"+ws.String()+"/retention-cleanup/review", researchJSON(t, map[string]any{"selected_refs": []string{inventory.Items[0].Ref}}), ownerAuth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &cleanupReview)
	if cleanupReview.Status != cleanupdomain.ReviewOpen || cleanupReview.ID.IsZero() || len(cleanupReview.SelectedRefs) != 1 || cleanupReview.SelectedRefs[0] != inventory.Items[0].Ref {
		t.Fatalf("cleanup review was not saved: %+v", cleanupReview)
	}
	var reviewHistory cleanupdomain.ReviewPage
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/retention-cleanup/reviews", ownerAuth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &reviewHistory)
	if reviewHistory.Count != 1 || reviewHistory.Items[0].ID != cleanupReview.ID || reviewHistory.Items[0].Status != cleanupdomain.ReviewOpen || reviewHistory.Items[0].ItemCount != 1 || reviewHistory.Items[0].ItemBytes != inventory.CandidateBytes {
		t.Fatalf("open cleanup review history was not visible: %+v", reviewHistory)
	}
	if res := s.post(t, "/v1/workspaces/"+ws.String()+"/retention-cleanup", `{"confirm":false}`, ownerAuth); res.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("unconfirmed cleanup status=%d want=%d", res.StatusCode, http.StatusPreconditionRequired)
	}
	var swept cleanupdomain.SweepResult
	res = s.post(t, "/v1/workspaces/"+ws.String()+"/retention-cleanup", `{"confirm":true,"selected_refs":["`+inventory.Items[0].Ref+`"],"review_id":"`+cleanupReview.ID.String()+`"}`, ownerAuth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &swept)
	if len(swept.Deleted) != 1 || swept.DeletedBytes != inventory.CandidateBytes {
		t.Fatalf("cleanup did not remove the verified candidate: %+v", swept)
	}
	if swept.Run.Status != cleanupdomain.SweepCompleted || swept.Run.DeletedCount != 1 || swept.Run.FinishedAt == nil {
		t.Fatalf("cleanup did not record a completed run: %+v", swept.Run)
	}
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/retention-cleanup", ownerAuth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &inventory)
	if inventory.CandidateCount != 0 || len(inventory.Items) != 0 {
		t.Fatalf("already swept bytes remained in inventory: %+v", inventory)
	}
	if len(inventory.RecentSweeps) != 1 || inventory.RecentSweeps[0].Status != cleanupdomain.SweepCompleted {
		t.Fatalf("cleanup history was not visible: %+v", inventory.RecentSweeps)
	}
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/retention-cleanup/status", ownerAuth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &lifecycle)
	if lifecycle.StateCounts[cleanupdomain.StateSwept] != 1 || lifecycle.Items[0].LastSweepOutcome != cleanupdomain.OutcomeDeleted {
		t.Fatalf("swept lifecycle was not durable: %+v", lifecycle)
	}
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/retention-cleanup/review", ownerAuth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &cleanupReview)
	if cleanupReview.Status != cleanupdomain.ReviewCompleted || cleanupReview.SweepID == nil || *cleanupReview.SweepID != swept.Run.ID {
		t.Fatalf("cleanup review was not completed with its sweep: %+v", cleanupReview)
	}
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/retention-cleanup/reviews", ownerAuth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &reviewHistory)
	if reviewHistory.Count != 1 || reviewHistory.Items[0].Status != cleanupdomain.ReviewCompleted || reviewHistory.Items[0].SweepID == nil || *reviewHistory.Items[0].SweepID != swept.Run.ID {
		t.Fatalf("completed cleanup review history was not durable: %+v", reviewHistory)
	}
	draftSource := addResearchSource(t, s, ws, ownerAuth, "A cleanup review that will be intentionally discarded.")
	draftBase := "/v1/workspaces/" + ws.String() + "/sources/" + draftSource.ID.String()
	researchStatus(t, s.put(t, draftBase+"/retention", researchJSON(t, map[string]any{"retention_until": past}), ownerAuth), http.StatusOK)
	researchStatus(t, s.post(t, draftBase+"/purge", researchJSON(t, map[string]any{"confirm": true, "reason": "Retention expired and no downstream research dependency remains."}), ownerAuth), http.StatusOK)
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/retention-cleanup", ownerAuth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &inventory)
	if inventory.CandidateCount != 1 {
		t.Fatalf("unexpected second cleanup inventory: %+v", inventory)
	}
	res = s.put(t, "/v1/workspaces/"+ws.String()+"/retention-cleanup/review", researchJSON(t, map[string]any{"selected_refs": []string{inventory.Items[0].Ref}}), ownerAuth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &cleanupReview)
	res = s.post(t, "/v1/workspaces/"+ws.String()+"/retention-cleanup/review/discard", researchJSON(t, map[string]any{"review_id": cleanupReview.ID.String(), "reason": "The investigation no longer needs this retention action."}), ownerAuth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &cleanupReview)
	if cleanupReview.Status != cleanupdomain.ReviewDiscarded || cleanupReview.DiscardReason == "" {
		t.Fatalf("cleanup review was not explicitly discarded: %+v", cleanupReview)
	}
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/retention-cleanup/reviews", ownerAuth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &reviewHistory)
	if reviewHistory.Count != 2 || reviewHistory.Items[0].Status != cleanupdomain.ReviewDiscarded || reviewHistory.Items[1].Status != cleanupdomain.ReviewCompleted {
		t.Fatalf("discarded review history was not preserved: %+v", reviewHistory)
	}
}

func TestResearchSourcePurgeRefusesWhenCitationsDependOnIt(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	source := addResearchSource(t, s, ws, auth, "A cited source.")
	base := "/v1/workspaces/" + ws.String() + "/sources/" + source.ID.String()
	recordResearchObservation(t, s, base, auth, source.LatestCapture.ID, "cited source")
	researchStatus(t, s.put(t, base+"/retention", researchJSON(t, map[string]any{"retention_until": "2026-01-01T00:00:00Z"}), auth), http.StatusOK)
	var review sourcedomain.PurgeReview
	res := s.get(t, base+"/retention-review", auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &review)
	if review.Eligible || review.Dependencies.ManualObservations != 1 || len(review.Blockers) != 1 || review.Blockers[0] != "observations_present" {
		t.Fatalf("citation dependency was not visible: %+v", review)
	}
	res = s.post(t, base+"/purge", researchJSON(t, map[string]any{"confirm": true, "reason": "Attempt while citation dependency exists."}), auth)
	researchStatus(t, res, http.StatusConflict)
	var refused sourcecmd.PurgeResult
	decode(t, res, &refused)
	if refused.Purged || refused.Review.Eligible {
		t.Fatalf("unsafe purge was not refused: %+v", refused)
	}
	var captured sourcequery.Captured
	res = s.get(t, base+"/captures/"+source.LatestCapture.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &captured)
	if captured.Content != "A cited source." {
		t.Fatalf("blocked purge changed readable content: %+v", captured)
	}
}

func TestResearchRetentionQueueFiltersCurrentLifecycleAndDependencies(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	due := addResearchSource(t, s, ws, auth, "Due source.")
	blocked := addResearchSource(t, s, ws, auth, "Blocked source.")
	recordResearchObservation(t, s, "/v1/workspaces/"+ws.String()+"/sources/"+blocked.ID.String(), auth, blocked.LatestCapture.ID, "Blocked source.")
	held := addResearchSource(t, s, ws, auth, "Held source.")
	scheduled := addResearchSource(t, s, ws, auth, "Scheduled source.")
	for _, source := range []sourcedomain.Summary{due, blocked, held, scheduled} {
		date := "2026-01-01T00:00:00Z"
		if source.ID == scheduled.ID {
			date = "2027-01-01T00:00:00Z"
		}
		researchStatus(t, s.put(t, "/v1/workspaces/"+ws.String()+"/sources/"+source.ID.String()+"/retention", researchJSON(t, map[string]any{"retention_until": date}), auth), http.StatusOK)
	}
	researchStatus(t, s.put(t, "/v1/workspaces/"+ws.String()+"/sources/"+held.ID.String()+"/privacy", researchJSON(t, map[string]any{"sensitivity": "restricted", "legal_hold": true, "legal_hold_reason": "Preserve during review."}), auth), http.StatusOK)

	path := "/v1/workspaces/" + ws.String() + "/retention-review"
	res := s.get(t, path, auth)
	researchStatus(t, res, http.StatusOK)
	var page sourcequery.RetentionPage
	decode(t, res, &page)
	if len(page.Items) != 4 {
		t.Fatalf("retention queue returned %d items: %+v", len(page.Items), page)
	}
	states := map[id.ID]string{}
	for _, item := range page.Items {
		states[item.Source.ID] = item.Review.State
	}
	if states[due.ID] != sourcedomain.RetentionDue || states[blocked.ID] != sourcedomain.RetentionBlocked || states[held.ID] != sourcedomain.RetentionHeld || states[scheduled.ID] != sourcedomain.RetentionScheduled {
		t.Fatalf("retention queue states=%v", states)
	}
	if page.Items[0].Review.Dependencies.Captures != 1 {
		t.Fatalf("queue did not expose capture dependency count: %+v", page.Items[0])
	}

	res = s.get(t, path+"?state=blocked", auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &page)
	if len(page.Items) != 1 || page.Items[0].Source.ID != blocked.ID || page.Items[0].Review.Dependencies.ManualObservations != 1 {
		t.Fatalf("blocked retention filter=%+v", page)
	}
	res = s.get(t, path+"?state=not-a-state", auth)
	researchStatus(t, res, http.StatusBadRequest)
}

func TestResearchURLReferenceFetchCreatesAnImmutableCapture(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<article>Retained public notice.</article>"))
	}))
	defer server.Close()
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)

	res := s.post(t, "/v1/workspaces/"+ws.String()+"/sources", researchJSON(t, map[string]any{
		"title": "Public notice", "origin": "reference", "url": server.URL,
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var source sourcedomain.Summary
	decode(t, res, &source)
	if source.LatestCapture != nil {
		t.Fatal("reference created a capture before explicit fetch")
	}
	res = s.post(t, "/v1/workspaces/"+ws.String()+"/sources/"+source.ID.String()+"/fetch", `{}`, auth)
	researchStatus(t, res, http.StatusCreated)
	var captured sourcedomain.Capture
	decode(t, res, &captured)
	if captured.Version != 1 || captured.MediaType != "text/html" || captured.Bytes == 0 {
		t.Fatalf("unexpected fetched capture: %+v", captured)
	}
	var held sourcequery.Captured
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/sources/"+source.ID.String()+"/captures/"+captured.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &held)
	if held.Content != "<article>Retained public notice.</article>" {
		t.Fatalf("fetched content was not retained exactly: %q", held.Content)
	}
}

func TestResearchSourceWatchCapturesOnlyChangedBytes(t *testing.T) {
	body := "<article>Initial monitored notice.</article>"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	base := "/v1/workspaces/" + ws.String() + "/sources"
	res := s.post(t, base, researchJSON(t, map[string]any{"title": "Monitored notice", "origin": "reference", "url": server.URL}), auth)
	researchStatus(t, res, http.StatusCreated)
	var source sourcedomain.Summary
	decode(t, res, &source)

	res = s.get(t, base+"/"+source.ID.String()+"/watch", auth)
	researchStatus(t, res, http.StatusOK)
	var watch sourcedomain.Watch
	decode(t, res, &watch)
	if watch.Enabled || watch.LastStatus != sourcedomain.WatchStatusNever || watch.IntervalSeconds != sourcedomain.DefaultWatchIntervalSeconds {
		t.Fatalf("unexpected default watch: %+v", watch)
	}
	res = s.put(t, base+"/"+source.ID.String()+"/watch", researchJSON(t, map[string]any{"enabled": true, "interval_seconds": 900}), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &watch)
	if !watch.Enabled || watch.NextRunAt == nil || watch.IntervalSeconds != 900 {
		t.Fatalf("watch was not enabled: %+v", watch)
	}
	if _, err := s.pool.DB(t.Context()).Exec(t.Context(), `update source.watch set next_run_at=$3 where workspace_id=$1 and source_id=$2`, ws, source.ID, time.Now().UTC().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}

	res = s.post(t, "/v1/workspaces/"+ws.String()+"/source-watches/run-due", `{}`, auth)
	researchStatus(t, res, http.StatusOK)
	var due dueSourceWatchResponse
	decode(t, res, &due)
	if !due.Claimed || due.Run == nil || !due.Run.Changed || due.Run.Capture == nil || due.Run.Capture.Version != 1 || due.Run.Watch.LastStatus != sourcedomain.WatchStatusChanged {
		t.Fatalf("due watch worker did not retain a changed capture: %+v", due)
	}
	if due.Run.Watch.LeaseOwner != "" || due.Run.Watch.LeaseUntil != nil {
		t.Fatalf("due watch lease was not released: %+v", due.Run.Watch)
	}
	alertsPath := "/v1/workspaces/" + ws.String() + "/source-alerts"
	res = s.get(t, alertsPath, auth)
	researchStatus(t, res, http.StatusOK)
	var alerts sourcequery.AlertPage
	decode(t, res, &alerts)
	if len(alerts.Items) != 1 || alerts.Items[0].Kind != sourcedomain.AlertKindCaptureChanged || alerts.Items[0].CaptureID == nil || *alerts.Items[0].CaptureID != due.Run.Capture.ID || alerts.Items[0].SourceTitle != "Monitored notice" || alerts.Items[0].SeenAt != nil {
		t.Fatalf("changed watch alert was not durable and unread: %+v", alerts)
	}
	res = s.post(t, alertsPath+"/"+alerts.Items[0].ID.String()+"/seen", `{}`, auth)
	researchStatus(t, res, http.StatusOK)
	res = s.get(t, alertsPath, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &alerts)
	if len(alerts.Items) != 1 || alerts.Items[0].SeenAt == nil {
		t.Fatalf("alert seen state was not account-scoped and durable: %+v", alerts)
	}
	run := *due.Run
	res = s.post(t, base+"/"+source.ID.String()+"/watch/run", `{}`, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &run)
	if run.Changed || run.Capture != nil || run.Watch.LastStatus != sourcedomain.WatchStatusUnchanged {
		t.Fatalf("unchanged watch run created a capture: %+v", run)
	}

	res = s.post(t, base+"/"+source.ID.String()+"/watch/run", `{}`, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &run)
	if run.Changed || run.Capture != nil || run.Watch.LastStatus != sourcedomain.WatchStatusUnchanged {
		t.Fatalf("repeated unchanged watch run created a capture: %+v", run)
	}

	body = "<article>Updated monitored notice.</article>"
	res = s.post(t, base+"/"+source.ID.String()+"/watch/run", `{}`, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &run)
	if !run.Changed || run.Capture == nil || run.Capture.Version != 2 || run.Watch.LastStatus != sourcedomain.WatchStatusChanged {
		t.Fatalf("changed watch run did not create version two: %+v", run)
	}
	var detail sourcequery.Detail
	res = s.get(t, base+"/"+source.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &detail)
	if len(detail.Captures) != 2 || detail.Source.LatestCapture == nil || detail.Source.LatestCapture.ID != run.Capture.ID {
		t.Fatalf("watch capture history was not immutable: %+v", detail)
	}
}

func TestResearchQuestionGapAlertsRefreshAndRetireWithCurrentReview(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	first := addResearchSource(t, s, ws, auth, "The first notice names East Quay café.")
	second := addResearchSource(t, s, ws, auth, "The second notice names East Quay café at 18:00.")
	left := recordResearchObservation(t, s, "/v1/workspaces/"+ws.String()+"/sources/"+first.ID.String(), auth, first.LatestCapture.ID, "East Quay café")
	right := recordResearchObservation(t, s, "/v1/workspaces/"+ws.String()+"/sources/"+second.ID.String(), auth, second.LatestCapture.ID, "East Quay café")
	base := "/v1/workspaces/" + ws.String()
	researchStatus(t, s.put(t, base+"/evidence/relations", researchJSON(t, map[string]any{
		"left_observation_id": left.ID, "right_observation_id": right.ID,
		"kind": "unresolved", "rationale": "The timing still needs another source.",
	}), auth), http.StatusOK)
	res := s.post(t, base+"/questions", researchJSON(t, map[string]any{
		"question": "Which notice timing is reliable?", "context": "Compare the two retained notices.",
		"state": "open", "observation_ids": []id.ID{left.ID, right.ID},
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var question map[string]any
	decode(t, res, &question)
	questionID, ok := question["question_id"].(string)
	if !ok || questionID == "" {
		t.Fatalf("question fixture: %+v", question)
	}

	alertsPath := base + "/source-alerts"
	res = s.post(t, alertsPath+"/refresh-gaps", `{}`, auth)
	researchStatus(t, res, http.StatusOK)
	var refreshed sourceGapAlertsResponse
	decode(t, res, &refreshed)
	if refreshed.ActiveGapCount != 1 {
		t.Fatalf("expected one active question gap: %+v", refreshed)
	}
	var alerts sourcequery.AlertPage
	res = s.get(t, alertsPath, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &alerts)
	if len(alerts.Items) != 1 || alerts.Items[0].Kind != sourcedomain.AlertKindQuestionGap || alerts.Items[0].QuestionID == nil || alerts.Items[0].QuestionID.String() != questionID || alerts.Items[0].SourceID != nil || alerts.Items[0].SeenAt != nil {
		t.Fatalf("question gap alert projection: %+v", alerts)
	}

	res = s.post(t, alertsPath+"/refresh-gaps", `{}`, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &refreshed)
	if refreshed.ActiveGapCount != 1 {
		t.Fatalf("refresh duplicated or lost the current gap: %+v", refreshed)
	}
	res = s.put(t, base+"/evidence/relations", researchJSON(t, map[string]any{
		"left_observation_id": left.ID, "right_observation_id": right.ID,
		"kind": "supports", "rationale": "The retained notices support the same conclusion.",
	}), auth)
	researchStatus(t, res, http.StatusOK)
	res = s.post(t, alertsPath+"/refresh-gaps", `{}`, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &refreshed)
	if refreshed.ActiveGapCount != 0 {
		t.Fatalf("resolved question gap remained active: %+v", refreshed)
	}
	res = s.get(t, alertsPath, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &alerts)
	if len(alerts.Items) != 0 {
		t.Fatalf("retired question gap remained in the active inbox: %+v", alerts)
	}
}

func TestResearchRecordAndClusterGapAlertsRefreshAndRetireWithCurrentReview(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	first := addResearchSource(t, s, ws, auth, "The first notice names East Quay café.")
	second := addResearchSource(t, s, ws, auth, "The second notice names East Quay café at 18:00.")
	left := recordResearchObservation(t, s, "/v1/workspaces/"+ws.String()+"/sources/"+first.ID.String(), auth, first.LatestCapture.ID, "East Quay café")
	right := recordResearchObservation(t, s, "/v1/workspaces/"+ws.String()+"/sources/"+second.ID.String(), auth, second.LatestCapture.ID, "East Quay café")
	base := "/v1/workspaces/" + ws.String()
	res := s.put(t, base+"/evidence/relations", researchJSON(t, map[string]any{
		"left_observation_id": left.ID, "right_observation_id": right.ID,
		"kind": "unresolved", "rationale": "The timing still needs another source.",
	}), auth)
	researchStatus(t, res, http.StatusOK)
	researchStatus(t, s.post(t, base+"/records", researchJSON(t, map[string]any{
		"kind": "account", "name": "East Quay account", "description": "A provisional authored record.", "observation_ids": []id.ID{left.ID, right.ID},
	}), auth), http.StatusCreated)
	researchStatus(t, s.post(t, base+"/evidence/clusters", researchJSON(t, map[string]any{
		"kind": "claim", "title": "East Quay timing claim", "description": "A provisional evidence grouping.", "observation_ids": []id.ID{left.ID, right.ID},
	}), auth), http.StatusCreated)

	alertsPath := base + "/source-alerts"
	res = s.post(t, alertsPath+"/refresh-gaps", `{}`, auth)
	researchStatus(t, res, http.StatusOK)
	var refreshed sourceGapAlertsResponse
	decode(t, res, &refreshed)
	if refreshed.ActiveGapCount != 2 {
		t.Fatalf("expected record and cluster gaps: %+v", refreshed)
	}
	var alerts sourcequery.AlertPage
	res = s.get(t, alertsPath, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &alerts)
	if len(alerts.Items) != 2 {
		t.Fatalf("expected two derived gap alerts: %+v", alerts)
	}
	kinds := map[string]bool{}
	for _, alert := range alerts.Items {
		kinds[alert.Kind] = true
		if alert.Kind == sourcedomain.AlertKindRecordGap && (alert.RecordID == nil || alert.ClusterID != nil || alert.SourceID != nil) {
			t.Fatalf("record gap target shape: %+v", alert)
		}
		if alert.Kind == sourcedomain.AlertKindClusterGap && (alert.ClusterID == nil || alert.RecordID != nil || alert.SourceID != nil) {
			t.Fatalf("cluster gap target shape: %+v", alert)
		}
	}
	if !kinds[sourcedomain.AlertKindRecordGap] || !kinds[sourcedomain.AlertKindClusterGap] {
		t.Fatalf("derived gap kinds: %+v", kinds)
	}

	res = s.put(t, base+"/evidence/relations", researchJSON(t, map[string]any{
		"left_observation_id": left.ID, "right_observation_id": right.ID,
		"kind": "supports", "rationale": "The retained notices support the same conclusion.",
	}), auth)
	researchStatus(t, res, http.StatusOK)
	res = s.post(t, alertsPath+"/refresh-gaps", `{}`, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &refreshed)
	if refreshed.ActiveGapCount != 0 {
		t.Fatalf("resolved record and cluster gaps remained active: %+v", refreshed)
	}
	res = s.get(t, alertsPath, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &alerts)
	if len(alerts.Items) != 0 {
		t.Fatalf("retired derived gap remained in the active inbox: %+v", alerts)
	}
}

func TestResearchDueWatchWorkerReturnsNoWorkWhenNothingIsDue(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	res := s.post(t, "/v1/workspaces/"+ws.String()+"/source-watches/run-due", `{}`, auth)
	researchStatus(t, res, http.StatusOK)
	var due dueSourceWatchResponse
	decode(t, res, &due)
	if due.Claimed || due.Run != nil {
		t.Fatalf("worker claimed unexpected work: %+v", due)
	}
}

func TestResearchBinarySourceImportRetainsBytesWithoutTextContent(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	pdf := "%PDF-1.7\nretained bytes"
	res := s.post(t, "/v1/workspaces/"+ws.String()+"/sources", researchJSON(t, map[string]any{
		"title": "Retained PDF", "origin": "import", "filename": "notice.pdf", "media_type": "application/pdf", "content_base64": base64.StdEncoding.EncodeToString([]byte(pdf)),
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var source sourcedomain.Summary
	decode(t, res, &source)
	if source.LatestCapture == nil || source.LatestCapture.MediaType != "application/pdf" {
		t.Fatalf("binary source was not captured: %+v", source)
	}
	var held sourcequery.Captured
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/sources/"+source.ID.String()+"/captures/"+source.LatestCapture.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &held)
	if held.Content != "" || held.ContentBase64 != base64.StdEncoding.EncodeToString([]byte(pdf)) {
		t.Fatalf("binary content crossed the text boundary: %+v", held)
	}
	base := "/v1/workspaces/" + ws.String() + "/sources/" + source.ID.String()
	assistanceResponse := s.post(t, base+"/captures/"+source.LatestCapture.ID.String()+"/assistance", `{}`, auth)
	researchStatus(t, assistanceResponse, http.StatusCreated)
	var unsupported assistquery.Detail
	decode(t, assistanceResponse, &unsupported)
	if unsupported.Operation.Status != assistdomain.OperationUnsupported || unsupported.Operation.Error == "" || len(unsupported.Proposals) != 0 {
		t.Fatalf("binary assistance outcome was not retained: %+v", unsupported)
	}
	retryResponse := s.post(t, base+"/captures/"+source.LatestCapture.ID.String()+"/assistance", researchJSON(t, map[string]any{"retry_operation_id": unsupported.Operation.ID}), auth)
	researchStatus(t, retryResponse, http.StatusCreated)
	var retried assistquery.Detail
	decode(t, retryResponse, &retried)
	if retried.Operation.Status != assistdomain.OperationUnsupported || retried.Operation.RetryOf == nil || *retried.Operation.RetryOf != unsupported.Operation.ID {
		t.Fatalf("unsupported assistance retry lost lineage: %+v", retried.Operation)
	}
	researchStatus(t, s.post(t, base+"/observations", researchJSON(t, map[string]any{"capture_id": source.LatestCapture.ID, "statement": "A binary claim", "quote": "PDF bytes"}), auth), http.StatusBadRequest)
}

func TestResearchLargeBinaryCaptureRoundTripsBeyondTextLimit(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	pdf := []byte("%PDF-1.7\n" + strings.Repeat("retained binary material\n", 16000))
	res := s.post(t, "/v1/workspaces/"+ws.String()+"/sources", researchJSON(t, map[string]any{
		"title": "Large retained PDF", "origin": "import", "filename": "large-notice.pdf", "media_type": "application/pdf", "content_base64": base64.StdEncoding.EncodeToString(pdf),
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var source sourcedomain.Summary
	decode(t, res, &source)
	var held sourcequery.Captured
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/sources/"+source.ID.String()+"/captures/"+source.LatestCapture.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &held)
	if held.Bytes != int64(len(pdf)) || held.Content != "" || held.ContentBase64 != base64.StdEncoding.EncodeToString(pdf) {
		t.Fatalf("large binary capture did not round-trip: bytes=%d content=%q encoded=%d", held.Bytes, held.Content, len(held.ContentBase64))
	}
}

func TestResearchURLReferenceFetchRetainsPDFBytesAsBinary(t *testing.T) {
	pdf := []byte("%PDF-1.7\nFetched PDF bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(pdf)
	}))
	defer server.Close()
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	res := s.post(t, "/v1/workspaces/"+ws.String()+"/sources", researchJSON(t, map[string]any{
		"title": "Fetched PDF", "origin": "reference", "url": server.URL,
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var source sourcedomain.Summary
	decode(t, res, &source)
	res = s.post(t, "/v1/workspaces/"+ws.String()+"/sources/"+source.ID.String()+"/fetch", `{}`, auth)
	researchStatus(t, res, http.StatusCreated)
	var captured sourcedomain.Capture
	decode(t, res, &captured)
	if captured.MediaType != "application/pdf" || captured.Bytes != int64(len(pdf)) {
		t.Fatalf("fetched PDF was not retained as binary: %+v", captured)
	}
	var held sourcequery.Captured
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/sources/"+source.ID.String()+"/captures/"+captured.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &held)
	if held.Content != "" || held.ContentBase64 != base64.StdEncoding.EncodeToString(pdf) {
		t.Fatalf("fetched PDF crossed the text boundary: %+v", held)
	}
}

func TestResearchPDFExtractionCreatesAProvenanceLinkedTextArtifact(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	pdf := researchPDF("The retained notice names East Quay.")
	res := s.post(t, "/v1/workspaces/"+ws.String()+"/sources", researchJSON(t, map[string]any{
		"title": "Extractable PDF", "origin": "import", "filename": "notice.pdf", "media_type": "application/pdf", "content_base64": base64.StdEncoding.EncodeToString(pdf),
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var source sourcedomain.Summary
	decode(t, res, &source)
	base := "/v1/workspaces/" + ws.String() + "/sources/" + source.ID.String() + "/captures/" + source.LatestCapture.ID.String()
	res = s.post(t, base+"/extract", `{}`, auth)
	researchStatus(t, res, http.StatusCreated)
	var created extractiondomain.Extraction
	decode(t, res, &created)
	if created.Status != extractiondomain.Succeeded || created.Method != extractiondomain.MethodPDFV1 || created.CaptureID != source.LatestCapture.ID || created.OutputBytes == 0 || created.OutputHash == "" {
		t.Fatalf("unexpected extraction: %+v", created)
	}
	res = s.get(t, base+"/extractions/"+created.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	var detail extractionquery.Detail
	decode(t, res, &detail)
	if !strings.Contains(detail.Text, "The retained notice names East Quay.") || detail.OutputBytes != int64(len(detail.Text)) {
		t.Fatalf("derived text was not read and verified: %+v", detail)
	}
	res = s.get(t, base+"/extractions", auth)
	researchStatus(t, res, http.StatusOK)
	var page extractionquery.Page
	decode(t, res, &page)
	if len(page.Items) != 1 || page.Items[0].ID != created.ID {
		t.Fatalf("extraction history: %+v", page)
	}
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/search?q=East%20Quay", auth)
	researchStatus(t, res, http.StatusOK)
	var search sourcequery.SearchPage
	decode(t, res, &search)
	found := false
	for _, item := range search.Items {
		if item.SourceID == source.ID && item.CaptureID == source.LatestCapture.ID && item.ExtractionID != nil && *item.ExtractionID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("derived extraction was not indexed with provenance: %+v", search)
	}
}

func TestResearchImageOCRRecordsAnExplicitUnsupportedAttemptWhenRuntimeIsUnconfigured(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	png := []byte("\x89PNG\r\n\x1a\nretained image bytes")
	res := s.post(t, "/v1/workspaces/"+ws.String()+"/sources", researchJSON(t, map[string]any{
		"title": "Retained image", "origin": "import", "filename": "notice.png", "media_type": "image/png", "content_base64": base64.StdEncoding.EncodeToString(png),
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var source sourcedomain.Summary
	decode(t, res, &source)
	base := "/v1/workspaces/" + ws.String() + "/sources/" + source.ID.String() + "/captures/" + source.LatestCapture.ID.String()
	res = s.post(t, base+"/extract", `{}`, auth)
	researchStatus(t, res, http.StatusCreated)
	var attempt extractiondomain.Extraction
	decode(t, res, &attempt)
	if attempt.Method != extractiondomain.MethodOCRV1 || attempt.Status != extractiondomain.Unsupported || attempt.Message != "image OCR is not configured in this runtime" || attempt.OutputHash != "" || attempt.OutputBytes != 0 {
		t.Fatalf("unexpected image OCR attempt: %+v", attempt)
	}
	res = s.get(t, base+"/extractions", auth)
	researchStatus(t, res, http.StatusOK)
	var page extractionquery.Page
	decode(t, res, &page)
	if len(page.Items) != 1 || page.Items[0].ID != attempt.ID || page.Items[0].Method != extractiondomain.MethodOCRV1 {
		t.Fatalf("image OCR attempt was not retained: %+v", page)
	}
}

func TestResearchConfiguredImageOCRProducesCitableDerivedText(t *testing.T) {
	ocr := extractioncmd.NewProcessImageOCR(extractioncmd.ProcessImageOCRConfig{
		Binary: "/bin/sh",
		Args:   []string{"-c", `test -s "$1" && printf 'The image notice names East Quay.'`, "ocr-test", extractioncmd.OCRInputPlaceholder},
	})
	s := tracedSystemWithOCR(t, ocr)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	png := []byte("\x89PNG\r\n\x1a\nretained image bytes")
	res := s.post(t, "/v1/workspaces/"+ws.String()+"/sources", researchJSON(t, map[string]any{
		"title": "OCR notice", "origin": "import", "filename": "notice.png", "media_type": "image/png", "content_base64": base64.StdEncoding.EncodeToString(png),
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var source sourcedomain.Summary
	decode(t, res, &source)
	base := "/v1/workspaces/" + ws.String() + "/sources/" + source.ID.String() + "/captures/" + source.LatestCapture.ID.String()
	res = s.post(t, base+"/extract", `{}`, auth)
	researchStatus(t, res, http.StatusCreated)
	var extraction extractiondomain.Extraction
	decode(t, res, &extraction)
	if extraction.Method != extractiondomain.MethodOCRV1 || extraction.Status != extractiondomain.Succeeded || extraction.OutputBytes == 0 || extraction.OutputHash == "" {
		t.Fatalf("unexpected configured OCR extraction: %+v", extraction)
	}
	res = s.get(t, base+"/extractions/"+extraction.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	var derived extractionquery.Detail
	decode(t, res, &derived)
	quote := "The image notice names East Quay."
	if derived.Text != quote {
		t.Fatalf("unexpected OCR text: %q", derived.Text)
	}
	res = s.post(t, "/v1/workspaces/"+ws.String()+"/sources/"+source.ID.String()+"/observations", researchJSON(t, map[string]any{
		"capture_id": source.LatestCapture.ID, "extraction_id": extraction.ID, "statement": "The image names East Quay.", "quote": quote,
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var observation obsdomain.Manual
	decode(t, res, &observation)
	if observation.ExtractionID == nil || *observation.ExtractionID != extraction.ID || observation.Quote != quote || observation.QuoteStart != 0 {
		t.Fatalf("OCR citation lost provenance or range: %+v", observation)
	}
	res = s.post(t, base+"/assistance", researchJSON(t, map[string]any{"extraction_id": extraction.ID}), auth)
	researchStatus(t, res, http.StatusCreated)
	var assistance assistquery.Detail
	decode(t, res, &assistance)
	if assistance.Operation.ExtractionID == nil || *assistance.Operation.ExtractionID != extraction.ID || len(assistance.Proposals) != 1 || assistance.Proposals[0].ExtractionID == nil || *assistance.Proposals[0].ExtractionID != extraction.ID {
		t.Fatalf("OCR assistance lost extraction provenance: %+v", assistance)
	}
}

func TestResearchPDFExtractionCanReceiveAnExactCitation(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	pdf := researchPDF("The retained notice names East Quay.")
	res := s.post(t, "/v1/workspaces/"+ws.String()+"/sources", researchJSON(t, map[string]any{
		"title": "Citable PDF", "origin": "import", "filename": "notice.pdf", "media_type": "application/pdf", "content_base64": base64.StdEncoding.EncodeToString(pdf),
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var source sourcedomain.Summary
	decode(t, res, &source)
	base := "/v1/workspaces/" + ws.String() + "/sources/" + source.ID.String() + "/captures/" + source.LatestCapture.ID.String()
	res = s.post(t, base+"/extract", `{}`, auth)
	researchStatus(t, res, http.StatusCreated)
	var extraction extractiondomain.Extraction
	decode(t, res, &extraction)
	quote := "The retained notice names East Quay."
	res = s.post(t, "/v1/workspaces/"+ws.String()+"/sources/"+source.ID.String()+"/observations", researchJSON(t, map[string]any{
		"capture_id": source.LatestCapture.ID, "extraction_id": extraction.ID, "statement": "The derived PDF text names East Quay.", "quote": quote,
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var observation obsdomain.Manual
	decode(t, res, &observation)
	if observation.ExtractionID == nil || *observation.ExtractionID != extraction.ID || observation.CaptureID != source.LatestCapture.ID || observation.Quote != quote {
		t.Fatalf("unexpected derived citation: %+v", observation)
	}
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/sources/"+source.ID.String()+"/observations/"+observation.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	var reread obsdomain.Manual
	decode(t, res, &reread)
	if reread.ExtractionID == nil || *reread.ExtractionID != extraction.ID || reread.QuoteEnd <= reread.QuoteStart {
		t.Fatalf("derived citation lost provenance: %+v", reread)
	}
}

func TestResearchAssistanceGeneratesReviewablePassagesFromOneRetainedCapture(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	content := "The notice places the meeting at East Quay. A second source check is still needed."
	source := addResearchSource(t, s, ws, auth, content)
	base := "/v1/workspaces/" + ws.String() + "/sources/" + source.ID.String() + "/captures/" + source.LatestCapture.ID.String() + "/assistance"
	res := s.get(t, base, auth)
	researchStatus(t, res, http.StatusOK)
	var latest assistquery.LatestDetail
	decode(t, res, &latest)
	if latest.Operation != nil || latest.Proposals == nil {
		t.Fatalf("unexpected assistance before generation: %+v", latest)
	}
	res = s.get(t, base+"/history", auth)
	researchStatus(t, res, http.StatusOK)
	var history assistquery.HistoryPage
	decode(t, res, &history)
	if history.Items == nil || len(history.Items) != 0 {
		t.Fatalf("unexpected assistance history before generation: %+v", history)
	}
	res = s.post(t, base, `{}`, auth)
	researchStatus(t, res, http.StatusCreated)
	var detail assistquery.Detail
	decode(t, res, &detail)
	if detail.Operation.SourceID != source.ID || detail.Operation.CaptureID != source.LatestCapture.ID || detail.Operation.Provider != "local" || detail.Operation.Method != "sentence-passages-v1" || detail.Operation.TemplateVersion != "sentence-passages-v1" || detail.Operation.InputBytes == 0 || detail.Operation.OutputBytes == 0 || detail.Operation.TimedOut || len(detail.Proposals) != 2 {
		t.Fatalf("unexpected assistance output: %+v", detail)
	}
	res = s.get(t, base, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &latest)
	if latest.Operation == nil || latest.Operation.ID != detail.Operation.ID || len(latest.Proposals) != len(detail.Proposals) {
		t.Fatalf("latest assistance did not resume generated operation: %+v", latest)
	}
	res = s.get(t, base+"/history", auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &history)
	if len(history.Items) != 1 || history.Items[0].Operation.ID != detail.Operation.ID {
		t.Fatalf("assistance history did not expose generated operation: %+v", history)
	}
	first := detail.Proposals[0]
	if first.State.String() != "proposed" || first.GeneratedQuote != "The notice places the meeting at East Quay." || first.GeneratedStatement != first.GeneratedQuote {
		t.Fatalf("unexpected first proposal: %+v", first)
	}
	res = s.put(t, "/v1/workspaces/"+ws.String()+"/assistance/"+detail.Operation.ID.String()+"/proposals/"+first.ID.String(), researchJSON(t, map[string]any{
		"decision": "accept", "statement": "The notice identifies East Quay as the meeting location.", "quote": first.GeneratedQuote,
	}), auth)
	researchStatus(t, res, http.StatusOK)
	var accepted map[string]any
	decode(t, res, &accepted)
	if accepted["state"] != "accepted" || accepted["generated_statement"] != first.GeneratedStatement || accepted["reviewed_statement"] != "The notice identifies East Quay as the meeting location." {
		t.Fatalf("generated proposal was not preserved during review: %+v", accepted)
	}
	second := detail.Proposals[1]
	res = s.put(t, "/v1/workspaces/"+ws.String()+"/assistance/"+detail.Operation.ID.String()+"/proposals/"+second.ID.String(), `{"decision":"reject","note":"Not sufficiently specific."}`, auth)
	researchStatus(t, res, http.StatusOK)
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/assistance/"+detail.Operation.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &detail)
	if detail.Proposals[0].State.String() != "accepted" || detail.Proposals[1].State.String() != "rejected" {
		t.Fatalf("review state was not retained: %+v", detail.Proposals)
	}
	res = s.get(t, base, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &latest)
	if latest.Operation == nil || latest.Proposals[0].State.String() != "accepted" || latest.Proposals[1].State.String() != "rejected" {
		t.Fatalf("latest assistance did not retain review state: %+v", latest.Proposals)
	}
	res = s.post(t, base, `{}`, auth)
	researchStatus(t, res, http.StatusCreated)
	var secondDetail assistquery.Detail
	decode(t, res, &secondDetail)
	res = s.get(t, base+"/history", auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &history)
	if len(history.Items) != 2 || history.Items[0].Operation.ID != secondDetail.Operation.ID || history.Items[1].Operation.ID != detail.Operation.ID {
		t.Fatalf("assistance history is not newest-first: %+v", history)
	}
	if n := testx.Count(t, s.pool, "select count(*) from assistance.proposal_review"); n != 2 {
		t.Fatalf("review history has %d rows, want 2", n)
	}
}

func TestResearchAssistanceCanGenerateAndReviewAgainstPDFExtraction(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	pdf := researchPDF("The notice places the meeting at East Quay. A second check is still needed.")
	res := s.post(t, "/v1/workspaces/"+ws.String()+"/sources", researchJSON(t, map[string]any{
		"title": "Assisted PDF", "origin": "import", "filename": "notice.pdf", "media_type": "application/pdf", "content_base64": base64.StdEncoding.EncodeToString(pdf),
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var source sourcedomain.Summary
	decode(t, res, &source)
	base := "/v1/workspaces/" + ws.String() + "/sources/" + source.ID.String() + "/captures/" + source.LatestCapture.ID.String()
	res = s.post(t, base+"/extract", `{}`, auth)
	researchStatus(t, res, http.StatusCreated)
	var extraction extractiondomain.Extraction
	decode(t, res, &extraction)
	res = s.post(t, base+"/assistance", researchJSON(t, map[string]any{"extraction_id": extraction.ID}), auth)
	researchStatus(t, res, http.StatusCreated)
	var detail assistquery.Detail
	decode(t, res, &detail)
	if detail.Operation.ExtractionID == nil || *detail.Operation.ExtractionID != extraction.ID || len(detail.Proposals) == 0 || detail.Proposals[0].ExtractionID == nil || *detail.Proposals[0].ExtractionID != extraction.ID {
		t.Fatalf("assistance lost extraction provenance: %+v", detail)
	}
	first := detail.Proposals[0]
	res = s.put(t, "/v1/workspaces/"+ws.String()+"/assistance/"+detail.Operation.ID.String()+"/proposals/"+first.ID.String(), researchJSON(t, map[string]any{
		"decision": "accept", "statement": "The derived notice names East Quay.", "quote": first.GeneratedQuote,
	}), auth)
	researchStatus(t, res, http.StatusOK)
	var accepted assistdomain.Proposal
	decode(t, res, &accepted)
	if accepted.State != assistdomain.ProposalAccepted || accepted.ExtractionID == nil || *accepted.ExtractionID != extraction.ID {
		t.Fatalf("derived assistance review lost provenance: %+v", accepted)
	}
}

func researchPDF(text string) []byte {
	stream := "BT\n/F1 18 Tf\n72 720 Td\n(" + strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(text) + ") Tj\nET\n"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var document bytes.Buffer
	document.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = document.Len()
		fmt.Fprintf(&document, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := document.Len()
	fmt.Fprintf(&document, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&document, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&document, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return document.Bytes()
}

func TestResearchTimelineEventKeepsReportedTimeSeparateFromCapture(t *testing.T) {
	s := tracedSystem(t)
	_, ws, owner, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	content := "The notice reports a disruption at East Quay. It was published after the reported incident."
	source := addResearchSource(t, s, ws, auth, content)
	base := "/v1/workspaces/" + ws.String() + "/sources/" + source.ID.String()
	observation := recordResearchObservation(t, s, base, auth, source.LatestCapture.ID, "disruption at East Quay")
	events := "/v1/workspaces/" + ws.String() + "/events"
	records := "/v1/workspaces/" + ws.String() + "/records"
	res := s.post(t, records, `{"kind":"person","name":"Harborline author","description":"The named author in the reports.","observation_ids":[]}`, auth)
	researchStatus(t, res, http.StatusCreated)
	var participant researchRecordResponse
	decode(t, res, &participant)
	res = s.post(t, records, `{"kind":"place","name":"East Quay","description":"The named location in the reports.","observation_ids":[]}`, auth)
	researchStatus(t, res, http.StatusCreated)
	var place researchRecordResponse
	decode(t, res, &place)
	participantID, err := id.Parse(participant.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	placeID, err := id.Parse(place.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	res = s.post(t, events, researchJSON(t, map[string]any{
		"title": "East Quay disruption", "description": "The reports may describe one incident.",
		"reported_time": "around 18:00", "time_precision": "approximate", "sort_date": "2026-09-17",
		"location": "East Quay", "observation_ids": []id.ID{observation.ID}, "participant_record_ids": []id.ID{participantID}, "participant_links": []eventdomain.ParticipantLink{{RecordID: participantID, Role: eventdomain.ParticipantActor}}, "location_record_id": placeID,
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var created eventdomain.Event
	decode(t, res, &created)
	if created.Author != owner || created.UpdatedBy != owner || created.TimePrecision != eventdomain.TimeApproximate || created.ReportedTime != "around 18:00" || created.ObservationIDs[0] != observation.ID || len(created.ParticipantRecordIDs) != 1 || created.ParticipantRecordIDs[0] != participantID || len(created.ParticipantLinks) != 1 || created.ParticipantLinks[0].Role != eventdomain.ParticipantActor || created.LocationRecordID == nil || *created.LocationRecordID != placeID {
		t.Fatalf("unexpected timeline event: %+v", created)
	}
	res = s.get(t, events+"/"+created.ID.String()+"/revisions", auth)
	researchStatus(t, res, http.StatusOK)
	var initialHistory struct {
		Items []struct {
			RevisionID         string `json:"revision_id"`
			Revision           int    `json:"revision"`
			Title              string `json:"title"`
			ReportedTime       string `json:"reported_time"`
			ParticipantRecords []struct {
				Name string `json:"name"`
			} `json:"participant_records"`
			LocationRecord *struct {
				Name string `json:"name"`
			} `json:"location_record"`
		} `json:"items"`
	}
	decode(t, res, &initialHistory)
	if len(initialHistory.Items) != 1 || initialHistory.Items[0].Revision != 1 || initialHistory.Items[0].Title != "East Quay disruption" || initialHistory.Items[0].ReportedTime != "around 18:00" || len(initialHistory.Items[0].ParticipantRecords) != 1 || initialHistory.Items[0].ParticipantRecords[0].Name != "Harborline author" || initialHistory.Items[0].LocationRecord == nil || initialHistory.Items[0].LocationRecord.Name != "East Quay" {
		t.Fatalf("unexpected initial event history: %+v", initialHistory)
	}
	res = s.put(t, events+"/"+created.ID.String(), researchJSON(t, map[string]any{
		"title": "East Quay disruption", "description": "The account remains provisional.",
		"reported_time": "", "time_precision": "unknown", "sort_date": "", "location": "East Quay", "observation_ids": []id.ID{observation.ID}, "participant_record_ids": []id.ID{participantID}, "participant_links": []eventdomain.ParticipantLink{{RecordID: participantID, Role: eventdomain.ParticipantActor}}, "location_record_id": placeID,
	}), auth)
	researchStatus(t, res, http.StatusOK)
	var updated eventdomain.Event
	decode(t, res, &updated)
	if updated.Author != owner || updated.UpdatedBy != owner || updated.TimePrecision != eventdomain.TimeUnknown || updated.Description != "The account remains provisional." || updated.ObservationIDs[0] != observation.ID || len(updated.ParticipantRecordIDs) != 1 || updated.LocationRecordID == nil || *updated.LocationRecordID != placeID {
		t.Fatalf("unexpected edited event: %+v", updated)
	}
	res = s.get(t, events+"/"+created.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &updated)
	if updated.TimePrecision != eventdomain.TimeUnknown || updated.ReportedTime != "" {
		t.Fatalf("event did not persist its uncertain time: %+v", updated)
	}
	res = s.get(t, events+"/"+created.ID.String()+"/revisions", auth)
	researchStatus(t, res, http.StatusOK)
	var history struct {
		Items []struct {
			Revision      int    `json:"revision"`
			Title         string `json:"title"`
			ReportedTime  string `json:"reported_time"`
			TimePrecision string `json:"time_precision"`
		} `json:"items"`
	}
	decode(t, res, &history)
	if len(history.Items) != 2 || history.Items[0].Revision != 1 || history.Items[0].ReportedTime != "around 18:00" || history.Items[1].Revision != 2 || history.Items[1].TimePrecision != "unknown" {
		t.Fatalf("event history did not preserve both revisions: %+v", history)
	}
	res = s.put(t, records+"/"+participant.RecordID, `{"kind":"person","name":"Harborline author revised","description":"A later record edit.","observation_ids":[]}`, auth)
	researchStatus(t, res, http.StatusOK)
	res = s.put(t, records+"/"+place.RecordID, `{"kind":"place","name":"East Quay revised","description":"A later place edit.","observation_ids":[]}`, auth)
	researchStatus(t, res, http.StatusOK)
	res = s.get(t, events+"/"+created.ID.String()+"/revisions", auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &initialHistory)
	if len(initialHistory.Items) != 2 {
		t.Fatalf("history changed after linked record edits: %+v", initialHistory)
	}
	res = s.get(t, events+"/"+created.ID.String()+"/revisions/"+initialHistory.Items[0].RevisionID, auth)
	researchStatus(t, res, http.StatusOK)
	var firstRevision struct {
		Title              string `json:"title"`
		ReportedTime       string `json:"reported_time"`
		ParticipantRecords []struct {
			Name string `json:"name"`
		} `json:"participant_records"`
		LocationRecord *struct {
			Name string `json:"name"`
		} `json:"location_record"`
	}
	decode(t, res, &firstRevision)
	if firstRevision.Title != "East Quay disruption" || firstRevision.ReportedTime != "around 18:00" || len(firstRevision.ParticipantRecords) != 1 || firstRevision.ParticipantRecords[0].Name != "Harborline author" || firstRevision.LocationRecord == nil || firstRevision.LocationRecord.Name != "East Quay" {
		t.Fatalf("event revision was reconstructed from mutable records: %+v", firstRevision)
	}
	accounts := events + "/" + created.ID.String() + "/accounts"
	res = s.post(t, accounts, researchJSON(t, map[string]any{
		"title": "Harborline notice account", "description": "The notice places the disruption at 18:20.",
		"reported_time": "18:20", "time_precision": "exact", "sort_date": "2026-09-17", "location": "East Quay", "observation_ids": []id.ID{observation.ID}, "participant_record_ids": []id.ID{participantID}, "participant_links": []eventdomain.ParticipantLink{{RecordID: participantID, Role: eventdomain.ParticipantAffected}},
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var account eventdomain.Account
	decode(t, res, &account)
	if account.EventID != created.ID || account.TimePrecision != eventdomain.TimeExact || account.ReportedTime != "18:20" || len(account.ObservationIDs) != 1 {
		t.Fatalf("unexpected competing event account: %+v", account)
	}
	res = s.get(t, accounts, auth)
	researchStatus(t, res, http.StatusOK)
	var accountPage eventdomain.AccountPage
	decode(t, res, &accountPage)
	if len(accountPage.Items) != 1 || accountPage.Reconciliation != nil {
		t.Fatalf("unexpected competing-account page: %+v", accountPage)
	}
	res = s.put(t, accounts+"/reconciliation", researchJSON(t, map[string]any{
		"decision": "prefer_account", "selected_account_id": account.ID, "rationale": "The cited notice gives a more precise reported time; retain the authored reconstruction and source account separately.",
	}), auth)
	researchStatus(t, res, http.StatusOK)
	var reconciliation eventdomain.Reconciliation
	decode(t, res, &reconciliation)
	if reconciliation.Decision != eventdomain.DecisionPreferAccount || reconciliation.SelectedAccountID == nil || *reconciliation.SelectedAccountID != account.ID {
		t.Fatalf("unexpected event reconciliation: %+v", reconciliation)
	}
	res = s.post(t, events, researchJSON(t, map[string]any{
		"title": "East Quay timing account", "description": "A second authored reconstruction retains a different timing interpretation.",
		"reported_time": "18:20", "time_precision": "exact", "sort_date": "2026-09-17", "location": "East Quay", "observation_ids": []id.ID{observation.ID}, "participant_record_ids": []id.ID{participantID},
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var second eventdomain.Event
	decode(t, res, &second)
	clusters := "/v1/workspaces/" + ws.String() + "/event-clusters"
	res = s.post(t, clusters, researchJSON(t, map[string]any{
		"title": "Possible same East Quay occurrence", "description": "Group authored event reconstructions for analyst review without asserting causality.", "event_ids": []id.ID{created.ID, second.ID},
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var cluster eventdomain.Cluster
	decode(t, res, &cluster)
	if cluster.State != eventdomain.ClusterProposed || len(cluster.EventIDs) != 2 || cluster.EventIDs[0] != created.ID || cluster.EventIDs[1] != second.ID {
		t.Fatalf("unexpected event cluster: %+v", cluster)
	}
	res = s.get(t, clusters, auth)
	researchStatus(t, res, http.StatusOK)
	var clusterPage eventquery.ClusterPage
	decode(t, res, &clusterPage)
	if len(clusterPage.Items) != 1 || clusterPage.Items[0].ID != cluster.ID {
		t.Fatalf("unexpected event cluster page: %+v", clusterPage)
	}
	res = s.put(t, clusters+"/"+cluster.ID.String()+"/review", researchJSON(t, map[string]any{
		"state": "accepted", "note": "The retained observations support one bounded same-occurrence hypothesis.",
	}), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &cluster)
	if cluster.State != eventdomain.ClusterAccepted || cluster.ReviewedBy == nil || *cluster.ReviewedBy != owner || cluster.ReviewNote == "" {
		t.Fatalf("unexpected reviewed event cluster: %+v", cluster)
	}
	relationships := "/v1/workspaces/" + ws.String() + "/event-relationships"
	res = s.post(t, relationships, researchJSON(t, map[string]any{
		"from_event_id": created.ID, "to_event_id": second.ID, "kind": "possibly_causes", "rationale": "The earlier disruption may explain the later account, but the evidence remains contested.", "supporting_observation_ids": []id.ID{observation.ID},
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var relationship eventdomain.Relationship
	decode(t, res, &relationship)
	if relationship.State != eventdomain.RelationshipProposed || relationship.Kind != eventdomain.RelationshipPossiblyCauses || relationship.FromEventID != created.ID || relationship.ToEventID != second.ID || len(relationship.SupportingObservationIDs) != 1 || relationship.SupportingObservationIDs[0] != observation.ID {
		t.Fatalf("unexpected event relationship: %+v", relationship)
	}
	res = s.get(t, relationships, auth)
	researchStatus(t, res, http.StatusOK)
	var relationshipPage eventquery.RelationshipPage
	decode(t, res, &relationshipPage)
	if len(relationshipPage.Items) != 1 || relationshipPage.Items[0].ID != relationship.ID {
		t.Fatalf("unexpected event relationship page: %+v", relationshipPage)
	}
	res = s.put(t, relationships+"/"+relationship.ID.String()+"/review", researchJSON(t, map[string]any{
		"state": "accepted", "note": "The sequence is accepted for this investigation.",
	}), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &relationship)
	if relationship.State != eventdomain.RelationshipAccepted || relationship.ReviewedBy == nil || *relationship.ReviewedBy != owner {
		t.Fatalf("unexpected reviewed event relationship: %+v", relationship)
	}
}

func TestResearchSourceAndCaptureIDsCannotCrossWorkspaceOrSource(t *testing.T) {
	s := tracedSystem(t)
	org, ws, _, other, auth, otherAuth := firm(t, s, orgdomain.RoleMember)
	source := addResearchSource(t, s, ws, auth, "First notice mentions East Quay.")
	base := "/v1/workspaces/" + ws.String() + "/sources/" + source.ID.String()
	res := s.post(t, "/v1/orgs/"+org.String()+"/workspaces", `{"name":"Separate investigation"}`, auth)
	researchStatus(t, res, http.StatusCreated)
	var opened workspaceResponse
	decode(t, res, &opened)
	second, err := id.Parse(opened.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	secondBase := "/v1/workspaces/" + second.String() + "/sources/" + source.ID.String()
	for _, path := range []string{base, base + "/captures/" + source.LatestCapture.ID.String(), base + "/observations"} {
		researchStatus(t, s.get(t, path, otherAuth), http.StatusNotFound)
	}
	for _, path := range []string{secondBase, secondBase + "/captures/" + source.LatestCapture.ID.String(), secondBase + "/observations"} {
		researchStatus(t, s.get(t, path, auth), http.StatusNotFound)
	}
	another := addResearchSource(t, s, ws, auth, "Second notice mentions East Quay.")
	res = s.post(t, base+"/observations", researchJSON(t, map[string]any{"capture_id": another.LatestCapture.ID, "statement": "Claim", "quote": "East Quay"}), auth)
	researchStatus(t, res, http.StatusNotFound)
	// Read access is enough to inspect evidence, but does not permit adding it.
	s.grant(t, org, other, ws, orgdomain.LevelRead)
	researchStatus(t, s.get(t, base, otherAuth), http.StatusOK)
	res = s.post(t, base+"/observations", researchJSON(t, map[string]any{"capture_id": source.LatestCapture.ID, "statement": "Claim", "quote": "East Quay"}), otherAuth)
	researchStatus(t, res, http.StatusNotFound)
}

func TestResearchIntakeRefusesInvalidContentAndUnmatchedCitations(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	list := "/v1/workspaces/" + ws.String() + "/sources"
	invalid := []string{
		`{"title":"Surrogate","origin":"paste","content":"\ud800"}`,
		string(append([]byte(`{"title":"UTF8","origin":"paste","content":"`), 255, '"', '}')),
		`{"title":"Bad URL","origin":"reference","url":"javascript:alert(1)"}`,
		`{"title":"Credentials","origin":"reference","url":"https://name:secret@example.test"}`,
		`{"title":"Bad JSON","origin":"import","filename":"a.json","media_type":"application/json","content":"broken"}`,
		`{"title":"Empty","origin":"paste","content":""}`,
		`{"title":"Two values","origin":"paste","content":"text"} {}`,
		researchJSON(t, map[string]any{"title": "Too large", "origin": "paste", "content": strings.Repeat("a", sourcedomain.MaxCaptureBytes+1)}),
	}
	for _, body := range invalid {
		researchStatus(t, s.post(t, list, body, auth), http.StatusBadRequest)
	}
	source := addResearchSource(t, s, ws, auth, "📍 East Quay")
	base := list + "/" + source.ID.String()
	for _, in := range []map[string]any{
		{"capture_id": source.LatestCapture.ID, "statement": "Claim", "quote": "not in this source"},
		{"capture_id": source.LatestCapture.ID, "statement": "Claim", "quote": "East Quay", "quote_start": 3},
		{"capture_id": source.LatestCapture.ID, "statement": "Claim", "quote": "East Quay", "quote_start": -1},
	} {
		researchStatus(t, s.post(t, base+"/observations", researchJSON(t, in), auth), http.StatusBadRequest)
	}
	var rows obsquery.ManualPage
	decode(t, s.get(t, base+"/observations", auth), &rows)
	if len(rows.Items) != 0 {
		t.Fatal("invalid citation persisted")
	}
	reference := s.post(t, list, `{"title":"Reference only","origin":"reference","url":"https://example.test/notice"}`, auth)
	researchStatus(t, reference, http.StatusCreated)
	var held sourcedomain.Summary
	decode(t, reference, &held)
	if held.LatestCapture != nil {
		t.Fatal("URL reference pretends it captured content")
	}
	for _, suffix := range []string{"?before=not-a-uuid", "?limit=0", "?limit=101"} {
		researchStatus(t, s.get(t, list+suffix, auth), http.StatusBadRequest)
	}
}

func TestResearchPaginationAndClosedWorkspaceKeepExistingEvidenceReadable(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	list := "/v1/workspaces/" + ws.String() + "/sources"
	searchResponse := s.post(t, list, researchJSON(t, map[string]any{"title": "Harbor bulletin", "origin": "reference", "url": "https://harbor.example/notice"}), auth)
	researchStatus(t, searchResponse, http.StatusCreated)
	var harbor sourcedomain.Summary
	decode(t, searchResponse, &harbor)
	sources := []sourcedomain.Summary{}
	for range 3 {
		sources = append(sources, addResearchSource(t, s, ws, auth, "East Quay notice"))
	}
	var searched sourcequery.Page
	decode(t, s.get(t, list+"?q=HARBOR", auth), &searched)
	if len(searched.Items) != 1 || searched.Items[0].ID != harbor.ID {
		t.Fatalf("source search=%+v", searched)
	}
	researchStatus(t, s.get(t, list+"?q="+strings.Repeat("x", 201), auth), http.StatusBadRequest)
	var first, second sourcequery.Page
	decode(t, s.get(t, list+"?q=East%20Quay&limit=2", auth), &first)
	if len(first.Items) != 2 || first.NextCursor == nil {
		t.Fatalf("first page=%+v", first)
	}
	decode(t, s.get(t, list+"?q=East%20Quay&limit=2&before="+first.NextCursor.String(), auth), &second)
	if len(second.Items) != 1 || second.NextCursor != nil || second.Items[0].ID == first.Items[0].ID || second.Items[0].ID == first.Items[1].ID {
		t.Fatalf("second page=%+v", second)
	}
	base := list + "/" + sources[0].ID.String()
	for range 3 {
		recordResearchObservation(t, s, base, auth, sources[0].LatestCapture.ID, "East Quay")
	}
	var obs1, obs2 obsquery.ManualPage
	decode(t, s.get(t, base+"/observations?limit=2", auth), &obs1)
	if len(obs1.Items) != 2 || obs1.NextCursor == nil {
		t.Fatalf("observation first page=%+v", obs1)
	}
	decode(t, s.get(t, base+"/observations?limit=2&before="+obs1.NextCursor.String(), auth), &obs2)
	if len(obs2.Items) != 1 || obs2.NextCursor != nil || obs2.Items[0].ID == obs1.Items[0].ID || obs2.Items[0].ID == obs1.Items[1].ID {
		t.Fatalf("observation second page=%+v", obs2)
	}
	researchStatus(t, s.post(t, "/v1/workspaces/"+ws.String()+"/close", "", auth), http.StatusNoContent)
	for _, path := range []string{list, base, base + "/observations", base + "/captures/" + sources[0].LatestCapture.ID.String()} {
		researchStatus(t, s.get(t, path, auth), http.StatusOK)
	}
	researchStatus(t, s.post(t, list, `{"title":"Closed","origin":"paste","content":"text"}`, auth), http.StatusConflict)
	researchStatus(t, s.post(t, base+"/captures", `{"content":"text"}`, auth), http.StatusConflict)
	researchStatus(t, s.post(t, base+"/observations", researchJSON(t, map[string]any{"capture_id": sources[0].LatestCapture.ID, "statement": "Claim", "quote": "East Quay"}), auth), http.StatusConflict)
}

func TestResearchTextSearchReturnsExactCaptureProvenanceAcrossSources(t *testing.T) {
	s := tracedSystem(t)
	_, ws, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	first := addResearchSource(t, s, ws, auth, "The first notice places the activity at East Quay.")
	second := addResearchSource(t, s, ws, auth, "The second notice independently names East Quay.")
	path := "/v1/workspaces/" + ws.String() + "/search?q=East%20Quay"
	var found sourcequery.SearchPage
	res := s.get(t, path, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &found)
	if len(found.Items) != 2 || found.NextCursor != nil {
		t.Fatalf("search page=%+v", found)
	}
	seen := map[id.ID]bool{}
	for _, item := range found.Items {
		seen[item.SourceID] = true
		if item.CaptureID.IsZero() || item.CaptureVersion != 1 || item.Match != "East Quay" || item.MatchStart < 0 || item.MatchEnd <= item.MatchStart || item.Excerpt == "" {
			t.Fatalf("search result lost exact provenance: %+v", item)
		}
	}
	if !seen[first.ID] || !seen[second.ID] {
		t.Fatalf("search sources=%v want %s and %s", seen, first.ID, second.ID)
	}
}
