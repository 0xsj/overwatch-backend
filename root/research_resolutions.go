package root

import (
	"net/http"
	"time"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	resolutiondomain "github.com/0xsj/overwatch-backend/internal/researchresolution/domain"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type researchResolutionResponse struct {
	ResolutionID                  string   `json:"resolution_id"`
	WorkspaceID                   string   `json:"workspace_id"`
	AliasRecordID                 string   `json:"alias_record_id"`
	CanonicalRecordID             string   `json:"canonical_record_id"`
	State                         string   `json:"state"`
	Rationale                     string   `json:"rationale"`
	ProposedBy                    string   `json:"proposed_by"`
	ProposedAt                    string   `json:"proposed_at"`
	ReviewedBy                    string   `json:"reviewed_by,omitempty"`
	ReviewedAt                    string   `json:"reviewed_at,omitempty"`
	ReversedBy                    string   `json:"reversed_by,omitempty"`
	ReversedAt                    string   `json:"reversed_at,omitempty"`
	CanonicalObservationIDsBefore []string `json:"canonical_observation_ids_before"`
	AddedObservationIDs           []string `json:"added_observation_ids"`
	CanonicalObservationIDsAfter  []string `json:"canonical_observation_ids_after"`
}

func asResearchResolution(resolution resolutiondomain.Resolution) researchResolutionResponse {
	ids := func(input []id.ID) []string {
		out := make([]string, 0, len(input))
		for _, one := range input {
			out = append(out, one.String())
		}
		return out
	}
	idString := func(value id.ID) string {
		if value.IsZero() {
			return ""
		}
		return value.String()
	}
	format := func(value time.Time) string {
		if value.IsZero() {
			return ""
		}
		return value.UTC().Format(time.RFC3339Nano)
	}
	formatPtr := func(value *time.Time) string {
		if value == nil {
			return ""
		}
		return format(*value)
	}
	return researchResolutionResponse{
		ResolutionID: idString(resolution.ID), WorkspaceID: idString(resolution.WorkspaceID), AliasRecordID: idString(resolution.AliasRecordID), CanonicalRecordID: idString(resolution.CanonicalRecordID), State: resolution.State.String(), Rationale: resolution.Rationale,
		ProposedBy: idString(resolution.ProposedBy), ProposedAt: format(resolution.ProposedAt), ReviewedBy: idString(resolution.ReviewedBy), ReviewedAt: formatPtr(resolution.ReviewedAt), ReversedBy: idString(resolution.ReversedBy), ReversedAt: formatPtr(resolution.ReversedAt),
		CanonicalObservationIDsBefore: ids(resolution.CanonicalObservationIDsBefore), AddedObservationIDs: ids(resolution.AddedObservationIDs), CanonicalObservationIDsAfter: ids(resolution.CanonicalObservationIDsAfter()),
	}
}

type researchResolutionRequest struct {
	CanonicalRecordID id.ID  `json:"canonical_record_id"`
	Rationale         string `json:"rationale"`
}

type researchResolutionDecisionRequest struct {
	Decision string `json:"decision"`
}

func (m *me) listResearchResolutions(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.resolutions.List(r.Context(), workspace, before, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	items := make([]researchResolutionResponse, 0, len(found.Items))
	for _, one := range found.Items {
		items = append(items, asResearchResolution(one))
	}
	httpx.WriteJSON(w, r, http.StatusOK, struct {
		Items      []researchResolutionResponse `json:"items"`
		NextCursor *id.ID                       `json:"next_cursor"`
	}{Items: items, NextCursor: found.NextCursor})
}

func (m *me) listRecordResolutions(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	record, err := id.Parse(r.PathValue("record"))
	if err != nil {
		httpx.Fail(m.log, w, r, resolutiondomain.ErrNotFound)
		return
	}
	found, err := m.research.resolutions.ActiveByAlias(r.Context(), workspace, record)
	if err != nil {
		if err == resolutiondomain.ErrNotFound {
			httpx.WriteJSON(w, r, http.StatusOK, struct {
				Items []researchResolutionResponse `json:"items"`
			}{Items: []researchResolutionResponse{}})
			return
		}
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, struct {
		Items []researchResolutionResponse `json:"items"`
	}{Items: []researchResolutionResponse{asResearchResolution(found)}})
}

func (m *me) readResearchResolution(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("resolution"))
	if err != nil {
		httpx.Fail(m.log, w, r, resolutiondomain.ErrNotFound)
		return
	}
	found, err := m.research.resolutions.ByID(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asResearchResolution(found))
}

func (m *me) createRecordResolution(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	alias, err := id.Parse(r.PathValue("record"))
	if err != nil {
		httpx.Fail(m.log, w, r, resolutiondomain.ErrNotFound)
		return
	}
	var in researchResolutionRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	fresh, err := m.research.resolutionCmd.Propose(r.Context(), workspace, alias, in.CanonicalRecordID, caller, in.Rationale)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, asResearchResolution(fresh))
}

func (m *me) reviewResearchResolution(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("resolution"))
	if err != nil {
		httpx.Fail(m.log, w, r, resolutiondomain.ErrNotFound)
		return
	}
	var in researchResolutionDecisionRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	found, err := m.research.resolutionCmd.Review(r.Context(), workspace, want, caller, in.Decision)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asResearchResolution(found))
}

func (m *me) reverseResearchResolution(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("resolution"))
	if err != nil {
		httpx.Fail(m.log, w, r, resolutiondomain.ErrNotFound)
		return
	}
	found, err := m.research.resolutionCmd.Reverse(r.Context(), workspace, want, caller)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asResearchResolution(found))
}
