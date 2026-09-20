package root

import (
	"net/http"
	"testing"

	assistquery "github.com/0xsj/overwatch-backend/internal/assistance/app/query"
	assistdomain "github.com/0xsj/overwatch-backend/internal/assistance/domain"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	reviewquery "github.com/0xsj/overwatch-backend/internal/review/app/query"
	reviewdomain "github.com/0xsj/overwatch-backend/internal/review/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestResearchEvidenceReviewListsCitationsAndUpdatesOneUnorderedPair(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	first := addResearchSource(t, s, workspace, auth, "The first notice names East Quay café.")
	second := addResearchSource(t, s, workspace, auth, "The second notice names East Quay café at 18:00.")
	firstBase := "/v1/workspaces/" + workspace.String() + "/sources/" + first.ID.String()
	secondBase := "/v1/workspaces/" + workspace.String() + "/sources/" + second.ID.String()
	left := recordResearchObservation(t, s, firstBase, auth, first.LatestCapture.ID, "East Quay café")
	right := recordResearchObservation(t, s, secondBase, auth, second.LatestCapture.ID, "East Quay café")

	var evidence reviewquery.EvidencePage
	res := s.get(t, "/v1/workspaces/"+workspace.String()+"/evidence", auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &evidence)
	if len(evidence.Items) != 2 || evidence.Items[0].SourceTitle == "" || evidence.Items[1].SourceTitle == "" {
		t.Fatalf("evidence projection: %+v", evidence)
	}
	var direct reviewdomain.Evidence
	res = s.get(t, "/v1/workspaces/"+workspace.String()+"/evidence/"+left.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &direct)
	if direct.ID != left.ID || direct.SourceID != first.ID || direct.CaptureID != first.LatestCapture.ID || direct.Quote == "" {
		t.Fatalf("direct evidence read: %+v", direct)
	}

	relationPath := "/v1/workspaces/" + workspace.String() + "/evidence/relations"
	body := researchJSON(t, map[string]any{
		"left_observation_id": right.ID, "right_observation_id": left.ID,
		"kind": "supports", "rationale": "Both notices place the café at the same location.",
	})
	res = s.put(t, relationPath, body, auth)
	researchStatus(t, res, http.StatusOK)
	var firstDecision reviewdomain.Relation
	decode(t, res, &firstDecision)
	if firstDecision.ID.IsZero() || firstDecision.LeftObservationID != left.ID || firstDecision.RightObservationID != right.ID {
		t.Fatalf("relation was not canonicalised: %+v", firstDecision)
	}

	res = s.put(t, relationPath, researchJSON(t, map[string]any{
		"left_observation_id": left.ID, "right_observation_id": right.ID,
		"kind": "unresolved", "rationale": "The location repeats, but the timing still needs another source.",
	}), auth)
	researchStatus(t, res, http.StatusOK)
	var updated reviewdomain.Relation
	decode(t, res, &updated)
	if updated.ID != firstDecision.ID || updated.Kind != reviewdomain.Unresolved || updated.Rationale == "" {
		t.Fatalf("reverse update created the wrong current decision: %+v", updated)
	}

	var decisions reviewquery.RelationPage
	res = s.get(t, relationPath, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &decisions)
	if len(decisions.Items) != 1 || decisions.Items[0].ID != firstDecision.ID || decisions.Items[0].Kind != reviewdomain.Unresolved {
		t.Fatalf("stored decisions: %+v", decisions)
	}
	var board reviewquery.BoardPage
	res = s.get(t, "/v1/workspaces/"+workspace.String()+"/evidence/board?state=unresolved&q=East+Quay", auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &board)
	if len(board.Items) != 2 || board.Items[0].ReviewState != reviewdomain.BoardUnresolved || board.Items[0].Unresolved != 1 || board.Items[1].ClusterCount != 0 {
		t.Fatalf("claims board projection: %+v", board)
	}
	researchStatus(t, s.post(t, "/v1/workspaces/"+workspace.String()+"/close", "", auth), http.StatusNoContent)
	researchStatus(t, s.get(t, "/v1/workspaces/"+workspace.String()+"/evidence", auth), http.StatusOK)
	researchStatus(t, s.get(t, relationPath, auth), http.StatusOK)
	researchStatus(t, s.put(t, relationPath, body, auth), http.StatusConflict)
}

func TestResearchEvidenceBoardFiltersBySourceDateRecordEventAndUnresolved(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	first := addResearchSource(t, s, workspace, auth, "The first notice names East Quay.")
	second := addResearchSource(t, s, workspace, auth, "The second notice names Harbor Line.")
	left := recordResearchObservation(t, s, "/v1/workspaces/"+workspace.String()+"/sources/"+first.ID.String(), auth, first.LatestCapture.ID, "East Quay")
	right := recordResearchObservation(t, s, "/v1/workspaces/"+workspace.String()+"/sources/"+second.ID.String(), auth, second.LatestCapture.ID, "Harbor Line")
	base := "/v1/workspaces/" + workspace.String()

	var record map[string]any
	res := s.post(t, base+"/records", researchJSON(t, map[string]any{
		"kind": "place", "name": "East Quay", "description": "A place filter fixture.", "observation_ids": []string{left.ID.String()},
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	decode(t, res, &record)
	recordID, ok := record["record_id"].(string)
	if !ok || recordID == "" {
		t.Fatalf("record fixture: %+v", record)
	}

	var event map[string]any
	res = s.post(t, base+"/events", researchJSON(t, map[string]any{
		"title": "Harbor Line event", "description": "An event filter fixture.", "reported_time": "18:00", "time_precision": "exact", "sort_date": "2026-09-19", "location": "Harbor Line", "observation_ids": []string{right.ID.String()}, "participant_record_ids": []string{},
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	decode(t, res, &event)
	eventID, ok := event["event_id"].(string)
	if !ok || eventID == "" {
		t.Fatalf("event fixture: %+v", event)
	}

	researchStatus(t, s.put(t, base+"/evidence/relations", researchJSON(t, map[string]any{
		"left_observation_id": left.ID, "right_observation_id": right.ID, "kind": "unresolved", "rationale": "The reports still need an independent check.",
	}), auth), http.StatusOK)

	date := left.RecordedAt.UTC().Format("2006-01-02")
	queries := []struct {
		name  string
		query string
		want  int
		id    id.ID
	}{
		{"source", "source=" + first.ID.String(), 1, left.ID},
		{"record", "record=" + recordID, 1, left.ID},
		{"event", "event=" + eventID, 1, right.ID},
		{"date", "date_from=" + date + "&date_to=" + date, 2, id.ID{}},
		{"unresolved", "unresolved=true", 2, id.ID{}},
	}
	for _, one := range queries {
		var page reviewquery.BoardPage
		res = s.get(t, base+"/evidence/board?"+one.query, auth)
		researchStatus(t, res, http.StatusOK)
		decode(t, res, &page)
		if len(page.Items) != one.want {
			t.Fatalf("%s filter returned %d rows: %+v", one.name, len(page.Items), page)
		}
		if !one.id.IsZero() && page.Items[0].ID != one.id {
			t.Fatalf("%s filter returned wrong observation: %+v", one.name, page.Items[0])
		}
	}
}

func TestResearchEvidenceReviewCannotUseAnUnknownObservation(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	source := addResearchSource(t, s, workspace, auth, "A notice.")
	observation := recordResearchObservation(t, s, "/v1/workspaces/"+workspace.String()+"/sources/"+source.ID.String(), auth, source.LatestCapture.ID, "notice")

	res := s.put(t, "/v1/workspaces/"+workspace.String()+"/evidence/relations", researchJSON(t, map[string]any{
		"left_observation_id": observation.ID, "right_observation_id": id.ID{1},
		"kind": "supports", "rationale": "unknown observation",
	}), auth)
	researchStatus(t, res, http.StatusNotFound)
}

func TestResearchEvidenceSourceLinksDetectDirectedCycles(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	first := addResearchSource(t, s, workspace, auth, "The first notice names East Quay.")
	second := addResearchSource(t, s, workspace, auth, "The second notice repeats the East Quay wording.")
	upstream := recordResearchObservation(t, s, "/v1/workspaces/"+workspace.String()+"/sources/"+first.ID.String(), auth, first.LatestCapture.ID, "East Quay")
	downstream := recordResearchObservation(t, s, "/v1/workspaces/"+workspace.String()+"/sources/"+second.ID.String(), auth, second.LatestCapture.ID, "East Quay")
	path := "/v1/workspaces/" + workspace.String() + "/evidence/source-links"

	res := s.put(t, path, researchJSON(t, map[string]any{
		"downstream_observation_id": downstream.ID,
		"upstream_observation_id":   upstream.ID,
		"rationale":                 "The later notice appears to repeat the earlier report's distinctive wording.",
	}), auth)
	researchStatus(t, res, http.StatusOK)
	var saved reviewdomain.SourceLink
	decode(t, res, &saved)
	if saved.ID.IsZero() || saved.DownstreamObservationID != downstream.ID || saved.UpstreamObservationID != upstream.ID {
		t.Fatalf("saved source link: %+v", saved)
	}

	var page reviewquery.SourceLinkPage
	res = s.get(t, path, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &page)
	if len(page.Items) != 1 || page.Items[0].CycleDetected || page.Items[0].DownstreamSourceID != second.ID || page.Items[0].UpstreamSourceID != first.ID || page.Items[0].DownstreamSourceTitle == "" || page.Items[0].UpstreamSourceTitle == "" {
		t.Fatalf("acyclic source-link projection: %+v", page)
	}

	res = s.put(t, path, researchJSON(t, map[string]any{
		"downstream_observation_id": upstream.ID,
		"upstream_observation_id":   downstream.ID,
		"rationale":                 "The reverse direction is retained as a separate analyst suspicion for review.",
	}), auth)
	researchStatus(t, res, http.StatusOK)

	res = s.get(t, path, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &page)
	if len(page.Items) != 2 {
		t.Fatalf("source-link cycle edges: %+v", page)
	}
	for _, item := range page.Items {
		if !item.CycleDetected {
			t.Fatalf("expected cycle warning for %+v", item)
		}
	}

	researchStatus(t, s.post(t, "/v1/workspaces/"+workspace.String()+"/close", "", auth), http.StatusNoContent)
	researchStatus(t, s.get(t, path, auth), http.StatusOK)
	researchStatus(t, s.put(t, path, researchJSON(t, map[string]any{
		"downstream_observation_id": downstream.ID,
		"upstream_observation_id":   upstream.ID,
		"rationale":                 "closed workspace write",
	}), auth), http.StatusConflict)
}

func TestResearchEvidenceClustersAreWorkspaceScopedAndEditable(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	first := addResearchSource(t, s, workspace, auth, "The first notice names East Quay.")
	second := addResearchSource(t, s, workspace, auth, "The second notice names East Quay again.")
	left := recordResearchObservation(t, s, "/v1/workspaces/"+workspace.String()+"/sources/"+first.ID.String(), auth, first.LatestCapture.ID, "East Quay")
	right := recordResearchObservation(t, s, "/v1/workspaces/"+workspace.String()+"/sources/"+second.ID.String(), auth, second.LatestCapture.ID, "East Quay")
	researchStatus(t, s.put(t, "/v1/workspaces/"+workspace.String()+"/evidence/relations", researchJSON(t, map[string]any{
		"left_observation_id": left.ID, "right_observation_id": right.ID,
		"kind": "supports", "rationale": "Both notices describe the same location.",
	}), auth), http.StatusOK)
	path := "/v1/workspaces/" + workspace.String() + "/evidence/clusters"

	res := s.post(t, path, researchJSON(t, map[string]any{
		"kind": "claim", "title": "East Quay location claim", "description": "Two retained notices describe the same location; keep the grouping qualified.", "observation_ids": []id.ID{left.ID, right.ID},
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var created reviewdomain.Cluster
	decode(t, res, &created)
	if created.ID.IsZero() || created.Kind != reviewdomain.ClaimCluster || created.Title != "East Quay location claim" || len(created.ObservationIDs) != 2 || created.ObservationIDs[0] != left.ID {
		t.Fatalf("created cluster: %+v", created)
	}
	var complete reviewquery.ClusterCoveragePage
	res = s.get(t, "/v1/workspaces/"+workspace.String()+"/evidence/clusters/coverage", auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &complete)
	if len(complete.Items) != 1 || complete.Items[0].DistinctSourceCount != 2 || complete.Items[0].SupportingCount != 1 || complete.Items[0].PossibleInternalPairs != 1 || complete.Items[0].UnreviewedInternalPairs != 0 || complete.Items[0].Status != reviewdomain.ClusterCovered {
		t.Fatalf("complete cluster coverage: %+v", complete)
	}

	res = s.put(t, path+"/"+created.ID.String(), researchJSON(t, map[string]any{
		"kind": "account", "title": "East Quay account grouping", "description": "The grouping is now being reviewed as an account hypothesis.", "observation_ids": []id.ID{right.ID},
	}), auth)
	researchStatus(t, res, http.StatusOK)
	var updated reviewdomain.Cluster
	decode(t, res, &updated)
	if updated.ID != created.ID || updated.Kind != reviewdomain.AccountCluster || len(updated.ObservationIDs) != 1 || updated.ObservationIDs[0] != right.ID {
		t.Fatalf("updated cluster: %+v", updated)
	}

	var page reviewquery.ClusterPage
	res = s.get(t, path, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &page)
	if len(page.Items) != 1 || page.Items[0].ID != created.ID || len(page.Items[0].ObservationIDs) != 1 {
		t.Fatalf("cluster list: %+v", page)
	}
	var coverage reviewquery.ClusterCoveragePage
	res = s.get(t, "/v1/workspaces/"+workspace.String()+"/evidence/clusters/coverage", auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &coverage)
	if len(coverage.Items) != 1 || coverage.Items[0].ClusterID != created.ID || coverage.Items[0].ObservationCount != 1 || coverage.Items[0].Status != reviewdomain.ClusterNeedsCorroboration {
		t.Fatalf("cluster coverage: %+v", coverage)
	}
	res = s.post(t, path, researchJSON(t, map[string]any{
		"kind": "claim", "title": "Unknown evidence", "description": "Should fail workspace membership.", "observation_ids": []id.ID{left.ID, id.ID{7}},
	}), auth)
	researchStatus(t, res, http.StatusNotFound)
}

func TestResearchEvidenceClusterCoverageFlagsRepeatedSingleSourceReporting(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	source := addResearchSource(t, s, workspace, auth, "The notice repeats the East Quay account.")
	base := "/v1/workspaces/" + workspace.String() + "/sources/" + source.ID.String()
	first := recordResearchObservation(t, s, base, auth, source.LatestCapture.ID, "East Quay")
	second := recordResearchObservation(t, s, base, auth, source.LatestCapture.ID, "East Quay account")
	researchStatus(t, s.put(t, "/v1/workspaces/"+workspace.String()+"/evidence/relations", researchJSON(t, map[string]any{
		"left_observation_id": first.ID, "right_observation_id": second.ID,
		"kind": "repeats", "rationale": "The second citation repeats the same retained notice account.",
	}), auth), http.StatusOK)

	path := "/v1/workspaces/" + workspace.String() + "/evidence/clusters"
	res := s.post(t, path, researchJSON(t, map[string]any{
		"kind": "claim", "title": "Single-source East Quay report", "description": "Keep repeated reporting separate from corroboration.", "observation_ids": []id.ID{first.ID, second.ID},
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var cluster reviewdomain.Cluster
	decode(t, res, &cluster)

	var page reviewquery.ClusterCoveragePage
	res = s.get(t, "/v1/workspaces/"+workspace.String()+"/evidence/clusters/coverage", auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &page)
	if len(page.Items) != 1 || page.Items[0].DistinctSourceCount != 1 || page.Items[0].RepeatingCount != 1 || page.Items[0].SupportingCount != 0 || page.Items[0].Status != reviewdomain.ClusterNeedsCorroboration {
		t.Fatalf("single-source repeated coverage: %+v", page)
	}
}

func TestResearchEvidenceSynthesisIsSavedAndResumable(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	first := addResearchSource(t, s, workspace, auth, "The notice names @HarborLine.")
	second := addResearchSource(t, s, workspace, auth, "The notice lists user@example.com.")
	left := recordResearchObservation(t, s, "/v1/workspaces/"+workspace.String()+"/sources/"+first.ID.String(), auth, first.LatestCapture.ID, "@HarborLine")
	right := recordResearchObservation(t, s, "/v1/workspaces/"+workspace.String()+"/sources/"+second.ID.String(), auth, second.LatestCapture.ID, "user@example.com")
	path := "/v1/workspaces/" + workspace.String() + "/evidence/syntheses"
	var empty assistquery.SynthesisPage
	res := s.get(t, path, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &empty)
	if len(empty.Items) != 0 {
		t.Fatalf("unexpected synthesis history: %+v", empty)
	}

	res = s.post(t, path, researchJSON(t, map[string]any{"observation_ids": []id.ID{left.ID, right.ID}}), auth)
	researchStatus(t, res, http.StatusCreated)
	var saved map[string]any
	decode(t, res, &saved)
	if saved["provider"] != "local" || saved["method"] != "selected-observations-v1" || saved["output"] == "" {
		t.Fatalf("saved synthesis: %+v", saved)
	}
	synthesisID, ok := saved["synthesis_id"].(string)
	if !ok || synthesisID == "" {
		t.Fatalf("missing synthesis id: %+v", saved)
	}
	if candidates, ok := saved["candidates"].([]any); !ok || len(candidates) != 2 {
		t.Fatalf("candidate citations were not retained: %+v", saved["candidates"])
	}

	var history assistquery.SynthesisPage
	res = s.get(t, path, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &history)
	if len(history.Items) != 1 || history.Items[0].ID.String() != synthesisID || len(history.Items[0].ObservationIDs) != 2 {
		t.Fatalf("synthesis history: %+v", history)
	}
	var direct map[string]any
	res = s.get(t, path+"/"+synthesisID, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &direct)
	if direct["synthesis_id"] != synthesisID || direct["workspace_id"] != workspace.String() {
		t.Fatalf("direct synthesis read: %+v", direct)
	}
}

func TestResearchEvidenceAssistedComparisonPersistsStructuredCitations(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	first := addResearchSource(t, s, workspace, auth, "The first notice says East Quay was closed at 18:00.")
	second := addResearchSource(t, s, workspace, auth, "The second notice says East Quay was open at 18:00.")
	left := recordResearchObservation(t, s, "/v1/workspaces/"+workspace.String()+"/sources/"+first.ID.String(), auth, first.LatestCapture.ID, "East Quay was closed at 18:00")
	right := recordResearchObservation(t, s, "/v1/workspaces/"+workspace.String()+"/sources/"+second.ID.String(), auth, second.LatestCapture.ID, "East Quay was open at 18:00")
	base := "/v1/workspaces/" + workspace.String() + "/evidence"
	researchStatus(t, s.put(t, base+"/relations", researchJSON(t, map[string]any{
		"left_observation_id": left.ID, "right_observation_id": right.ID, "kind": "contradicts", "rationale": "The opening status differs.",
	}), auth), http.StatusOK)

	path := base + "/comparisons"
	var empty assistquery.ComparisonPage
	res := s.get(t, path, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &empty)
	if len(empty.Items) != 0 {
		t.Fatalf("unexpected comparison history: %+v", empty)
	}

	res = s.post(t, path, researchJSON(t, map[string]any{"observation_ids": []id.ID{left.ID, right.ID}}), auth)
	researchStatus(t, res, http.StatusCreated)
	var saved assistdomain.Comparison
	decode(t, res, &saved)
	if saved.ID.IsZero() || saved.Provider != "local" || saved.Method != "selected-observations-comparison-v1" || saved.TemplateVersion != "comparison-v1" || saved.Status != assistdomain.ComparisonCompleted || len(saved.Findings) == 0 {
		t.Fatalf("saved comparison: %+v", saved)
	}
	foundContradiction := false
	for _, finding := range saved.Findings {
		if finding.Kind == assistdomain.Contradiction {
			foundContradiction = true
		}
		if len(finding.ObservationIDs) == 0 {
			t.Fatalf("comparison finding lost exact citations: %+v", finding)
		}
	}
	if !foundContradiction {
		t.Fatalf("comparison did not surface the saved contradiction: %+v", saved.Findings)
	}

	var history assistquery.ComparisonPage
	res = s.get(t, path, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &history)
	if len(history.Items) != 1 || history.Items[0].ID != saved.ID || len(history.Items[0].ObservationIDs) != 2 {
		t.Fatalf("comparison history: %+v", history)
	}
	var direct assistdomain.Comparison
	res = s.get(t, path+"/"+saved.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &direct)
	if direct.ID != saved.ID || direct.WorkspaceID != workspace {
		t.Fatalf("direct comparison read: %+v", direct)
	}
}

func TestResearchEvidenceQuestionSuggestionsPersistGapInputsAndDrafts(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	first := addResearchSource(t, s, workspace, auth, "The first notice says East Quay was closed at 18:00.")
	second := addResearchSource(t, s, workspace, auth, "The second notice says East Quay was open at 18:00.")
	left := recordResearchObservation(t, s, "/v1/workspaces/"+workspace.String()+"/sources/"+first.ID.String(), auth, first.LatestCapture.ID, "East Quay was closed at 18:00")
	right := recordResearchObservation(t, s, "/v1/workspaces/"+workspace.String()+"/sources/"+second.ID.String(), auth, second.LatestCapture.ID, "East Quay was open at 18:00")
	base := "/v1/workspaces/" + workspace.String() + "/evidence/question-suggestions"
	var empty assistquery.QuestionSuggestionPage
	res := s.get(t, base, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &empty)
	if len(empty.Items) != 0 {
		t.Fatalf("unexpected question suggestion history: %+v", empty)
	}

	res = s.post(t, base, researchJSON(t, map[string]any{"gaps": []map[string]any{{
		"kind": "contradiction", "label": "East Quay opening status", "detail": "The two notices disagree about whether the venue was open.", "observation_ids": []id.ID{left.ID, right.ID},
	}}}), auth)
	researchStatus(t, res, http.StatusCreated)
	var saved assistdomain.QuestionSuggestions
	decode(t, res, &saved)
	if saved.ID.IsZero() || saved.Provider != "local" || saved.Method != "unresolved-evidence-gaps-v1" || saved.TemplateVersion != "question-suggestions-v1" || saved.Status != assistdomain.QuestionSuggestionsCompleted || len(saved.Gaps) != 1 || len(saved.Suggestions) != 1 {
		t.Fatalf("saved question suggestions: %+v", saved)
	}
	if len(saved.Gaps[0].ObservationIDs) != 2 || len(saved.Suggestions[0].ObservationIDs) != 2 || saved.Suggestions[0].Prompt == "" {
		t.Fatalf("question suggestion citations: %+v", saved)
	}

	var history assistquery.QuestionSuggestionPage
	res = s.get(t, base, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &history)
	if len(history.Items) != 1 || history.Items[0].ID != saved.ID {
		t.Fatalf("question suggestion history: %+v", history)
	}
	var direct assistdomain.QuestionSuggestions
	res = s.get(t, base+"/"+saved.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &direct)
	if direct.ID != saved.ID || direct.WorkspaceID != workspace || direct.Suggestions[0].ObservationIDs[0] != left.ID {
		t.Fatalf("direct question suggestion read: %+v", direct)
	}
}
