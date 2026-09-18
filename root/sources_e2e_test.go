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
	researchStatus(t, s.post(t, base+"/captures/"+source.LatestCapture.ID.String()+"/assistance", `{}`, auth), http.StatusBadRequest)
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
	if detail.Operation.SourceID != source.ID || detail.Operation.CaptureID != source.LatestCapture.ID || detail.Operation.Provider != "local" || detail.Operation.Method != "sentence-passages-v1" || len(detail.Proposals) != 2 {
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
	res := s.post(t, events, researchJSON(t, map[string]any{
		"title": "East Quay disruption", "description": "The reports may describe one incident.",
		"reported_time": "around 18:00", "time_precision": "approximate", "sort_date": "2026-09-17",
		"location": "East Quay", "observation_ids": []id.ID{observation.ID},
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var created eventdomain.Event
	decode(t, res, &created)
	if created.Author != owner || created.UpdatedBy != owner || created.TimePrecision != eventdomain.TimeApproximate || created.ReportedTime != "around 18:00" || created.ObservationIDs[0] != observation.ID {
		t.Fatalf("unexpected timeline event: %+v", created)
	}
	res = s.put(t, events+"/"+created.ID.String(), researchJSON(t, map[string]any{
		"title": "East Quay disruption", "description": "The account remains provisional.",
		"reported_time": "", "time_precision": "unknown", "sort_date": "", "location": "East Quay", "observation_ids": []id.ID{observation.ID},
	}), auth)
	researchStatus(t, res, http.StatusOK)
	var updated eventdomain.Event
	decode(t, res, &updated)
	if updated.Author != owner || updated.UpdatedBy != owner || updated.TimePrecision != eventdomain.TimeUnknown || updated.Description != "The account remains provisional." || updated.ObservationIDs[0] != observation.ID {
		t.Fatalf("unexpected edited event: %+v", updated)
	}
	res = s.get(t, events+"/"+created.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &updated)
	if updated.TimePrecision != eventdomain.TimeUnknown || updated.ReportedTime != "" {
		t.Fatalf("event did not persist its uncertain time: %+v", updated)
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
