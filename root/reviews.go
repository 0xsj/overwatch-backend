package root

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	assistdomain "github.com/0xsj/overwatch-backend/internal/assistance/domain"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	reviewdomain "github.com/0xsj/overwatch-backend/internal/review/domain"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type synthesisRequest struct {
	ObservationIDs []id.ID `json:"observation_ids"`
}

type comparisonRequest struct {
	ObservationIDs []id.ID `json:"observation_ids"`
}

type questionSuggestionsRequest struct {
	Gaps []assistdomain.QuestionSuggestionGap `json:"gaps"`
}

type relationRequest struct {
	LeftObservationID  id.ID  `json:"left_observation_id"`
	RightObservationID id.ID  `json:"right_observation_id"`
	Kind               string `json:"kind"`
	Rationale          string `json:"rationale"`
}

type sourceLinkRequest struct {
	DownstreamObservationID id.ID  `json:"downstream_observation_id"`
	UpstreamObservationID   id.ID  `json:"upstream_observation_id"`
	Rationale               string `json:"rationale"`
}

type clusterRequest struct {
	Kind           string  `json:"kind"`
	Title          string  `json:"title"`
	Description    string  `json:"description"`
	ObservationIDs []id.ID `json:"observation_ids"`
}

type evidenceClusterResponse struct {
	ClusterID      string   `json:"cluster_id"`
	WorkspaceID    string   `json:"workspace_id"`
	Kind           string   `json:"kind"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	ObservationIDs []string `json:"observation_ids"`
	Author         string   `json:"author"`
	UpdatedBy      string   `json:"updated_by"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

func (m *me) listEvidenceBoard(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	filters := reviewdomain.BoardFilters{Query: strings.TrimSpace(r.URL.Query().Get("q"))}
	for raw, target := range map[string]*id.ID{"source": &filters.SourceID, "record": &filters.RecordID, "event": &filters.EventID} {
		value := strings.TrimSpace(r.URL.Query().Get(raw))
		if value == "" {
			continue
		}
		parsed, err := id.Parse(value)
		if err != nil || parsed.IsZero() {
			httpx.Fail(m.log, w, r, reviewdomain.ErrInvalid)
			return
		}
		*target = parsed
	}
	state, err := reviewdomain.ParseBoardState(r.URL.Query().Get("state"))
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	filters.State = state
	for _, one := range []struct {
		name   string
		end    bool
		target **time.Time
	}{{"date_from", false, &filters.RecordedFrom}, {"date_to", true, &filters.RecordedTo}} {
		value := strings.TrimSpace(r.URL.Query().Get(one.name))
		if value == "" {
			continue
		}
		parsed, parseErr := parseEvidenceBoardDate(value, one.end)
		if parseErr != nil {
			httpx.Fail(m.log, w, r, reviewdomain.ErrInvalid)
			return
		}
		*one.target = parsed
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("unresolved")); raw != "" {
		parsed, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			httpx.Fail(m.log, w, r, reviewdomain.ErrInvalid)
			return
		}
		filters.UnresolvedOnly = parsed
	}
	found, err := m.research.board.List(r.Context(), workspace, before, filters, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func parseEvidenceBoardDate(raw string, exclusiveEnd bool) (*time.Time, error) {
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		return nil, err
	}
	if exclusiveEnd {
		parsed = parsed.AddDate(0, 0, 1)
	}
	return &parsed, nil
}

func asEvidenceCluster(cluster reviewdomain.Cluster) evidenceClusterResponse {
	observations := make([]string, 0, len(cluster.ObservationIDs))
	for _, observation := range cluster.ObservationIDs {
		observations = append(observations, observation.String())
	}
	return evidenceClusterResponse{
		ClusterID: cluster.ID.String(), WorkspaceID: cluster.WorkspaceID.String(), Kind: cluster.Kind.String(),
		Title: cluster.Title, Description: cluster.Description, ObservationIDs: observations,
		Author: cluster.Author.String(), UpdatedBy: cluster.UpdatedBy.String(),
		CreatedAt: cluster.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: cluster.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (m *me) listEvidenceClusters(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.clusters.List(r.Context(), workspace, before, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	items := make([]evidenceClusterResponse, 0, len(found.Items))
	for _, cluster := range found.Items {
		items = append(items, asEvidenceCluster(cluster))
	}
	httpx.WriteJSON(w, r, http.StatusOK, struct {
		Items      []evidenceClusterResponse `json:"items"`
		NextCursor *id.ID                    `json:"next_cursor"`
	}{Items: items, NextCursor: found.NextCursor})
}

func (m *me) listEvidenceClusterCoverage(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.clusterCoverage.List(r.Context(), workspace, before, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) readEvidenceCluster(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(strings.TrimSpace(r.PathValue("cluster")))
	if err != nil || want.IsZero() {
		httpx.Fail(m.log, w, r, reviewdomain.ErrNotFound)
		return
	}
	found, err := m.research.clusters.ByID(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asEvidenceCluster(found))
}

func (m *me) createEvidenceCluster(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in clusterRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	fresh, err := m.research.clusterCmd.Create(r.Context(), workspace, caller, in.Kind, in.Title, in.Description, in.ObservationIDs)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, asEvidenceCluster(fresh))
}

func (m *me) editEvidenceCluster(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(strings.TrimSpace(r.PathValue("cluster")))
	if err != nil || want.IsZero() {
		httpx.Fail(m.log, w, r, reviewdomain.ErrNotFound)
		return
	}
	var in clusterRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	updated, err := m.research.clusterCmd.Edit(r.Context(), workspace, want, caller, in.Kind, in.Title, in.Description, in.ObservationIDs)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asEvidenceCluster(updated))
}

func (m *me) listEvidence(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.relations.Evidence(r.Context(), workspace, before, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) readEvidence(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("observation"))
	if err != nil || want.IsZero() {
		httpx.Fail(m.log, w, r, reviewdomain.ErrNotFound)
		return
	}
	found, err := m.research.relations.EvidenceByID(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) listEvidenceRelations(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.relations.Relations(r.Context(), workspace, before, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) setEvidenceRelation(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in relationRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	fresh, err := m.research.relationCmd.Set(r.Context(), workspace, caller,
		in.LeftObservationID, in.RightObservationID, in.Kind, in.Rationale)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, fresh)
}

func (m *me) listEvidenceSourceLinks(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.sourceLinks.List(r.Context(), workspace, before, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) setEvidenceSourceLink(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in sourceLinkRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	fresh, err := m.research.sourceLinkCmd.Set(r.Context(), workspace, caller, in.DownstreamObservationID, in.UpstreamObservationID, in.Rationale)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, fresh)
}

func (m *me) listEvidenceSyntheses(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.syntheses.List(r.Context(), workspace, before, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) readEvidenceSynthesis(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(strings.TrimSpace(r.PathValue("synthesis")))
	if err != nil || want.IsZero() {
		httpx.Fail(m.log, w, r, assistdomain.ErrNotFound)
		return
	}
	found, err := m.research.syntheses.ByID(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) createEvidenceSynthesis(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in synthesisRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	fresh, err := m.research.synthesisCmd.Generate(r.Context(), workspace, in.ObservationIDs, caller)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, fresh)
}

func (m *me) listEvidenceComparisons(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.comparisons.List(r.Context(), workspace, before, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) readEvidenceComparison(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(strings.TrimSpace(r.PathValue("comparison")))
	if err != nil || want.IsZero() {
		httpx.Fail(m.log, w, r, assistdomain.ErrNotFound)
		return
	}
	found, err := m.research.comparisons.ByID(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) createEvidenceComparison(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in comparisonRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	fresh, err := m.research.comparisonCmd.Generate(r.Context(), workspace, in.ObservationIDs, caller)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, fresh)
}

func (m *me) listEvidenceQuestionSuggestions(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.questionSuggestions.List(r.Context(), workspace, before, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) readEvidenceQuestionSuggestions(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(strings.TrimSpace(r.PathValue("suggestion")))
	if err != nil || want.IsZero() {
		httpx.Fail(m.log, w, r, assistdomain.ErrNotFound)
		return
	}
	found, err := m.research.questionSuggestions.ByID(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) createEvidenceQuestionSuggestions(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in questionSuggestionsRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	fresh, err := m.research.questionSuggestionCmd.Generate(r.Context(), workspace, in.Gaps, caller)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, fresh)
}
