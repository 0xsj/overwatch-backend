package root

import (
	"net/http"
	"strings"
	"time"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	recorddomain "github.com/0xsj/overwatch-backend/internal/researchentity/domain"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type researchRecordResponse struct {
	RecordID       string                         `json:"record_id"`
	WorkspaceID    string                         `json:"workspace_id"`
	Kind           string                         `json:"kind"`
	Name           string                         `json:"name"`
	Description    string                         `json:"description,omitempty"`
	ObservationIDs []string                       `json:"observation_ids"`
	PlaceGeometry  *researchPlaceGeometryResponse `json:"place_geometry,omitempty"`
	Author         string                         `json:"author"`
	UpdatedBy      string                         `json:"updated_by"`
	CreatedAt      string                         `json:"created_at"`
	UpdatedAt      string                         `json:"updated_at"`
	ArchivedAt     string                         `json:"archived_at,omitempty"`
	ArchivedBy     string                         `json:"archived_by,omitempty"`
}

type researchPlaceGeometryResponse struct {
	Latitude       float64  `json:"latitude"`
	Longitude      float64  `json:"longitude"`
	Precision      string   `json:"precision"`
	ObservationIDs []string `json:"observation_ids"`
}

type researchRecordSummaryResponse struct {
	RecordCount                   int            `json:"record_count"`
	KindCounts                    map[string]int `json:"kind_counts"`
	CitedRecordCount              int            `json:"cited_record_count"`
	UncitedRecordCount            int            `json:"uncited_record_count"`
	CitationCount                 int            `json:"citation_count"`
	OpenResolutionRecordCount     int            `json:"open_resolution_record_count"`
	AcceptedResolutionRecordCount int            `json:"accepted_resolution_record_count"`
}

type researchRecordRevisionResponse struct {
	RevisionID     string                         `json:"revision_id"`
	WorkspaceID    string                         `json:"workspace_id"`
	RecordID       string                         `json:"record_id"`
	Revision       int                            `json:"revision"`
	Kind           string                         `json:"kind"`
	Name           string                         `json:"name"`
	Description    string                         `json:"description,omitempty"`
	ObservationIDs []string                       `json:"observation_ids"`
	PlaceGeometry  *researchPlaceGeometryResponse `json:"place_geometry,omitempty"`
	ArchivedAt     string                         `json:"archived_at,omitempty"`
	ArchivedBy     string                         `json:"archived_by,omitempty"`
	ChangedBy      string                         `json:"changed_by"`
	ChangedAt      string                         `json:"changed_at"`
}

func asResearchRecord(record recorddomain.Record) researchRecordResponse {
	observations := make([]string, 0, len(record.ObservationIDs))
	for _, one := range record.ObservationIDs {
		observations = append(observations, one.String())
	}
	var geometry *researchPlaceGeometryResponse
	if record.PlaceGeometry != nil {
		geometryObservationIDs := make([]string, 0, len(record.PlaceGeometry.ObservationIDs))
		for _, observation := range record.PlaceGeometry.ObservationIDs {
			geometryObservationIDs = append(geometryObservationIDs, observation.String())
		}
		geometry = &researchPlaceGeometryResponse{Latitude: record.PlaceGeometry.Latitude, Longitude: record.PlaceGeometry.Longitude, Precision: record.PlaceGeometry.Precision.String(), ObservationIDs: geometryObservationIDs}
	}
	response := researchRecordResponse{
		RecordID: record.ID.String(), WorkspaceID: record.WorkspaceID.String(), Kind: record.Kind.String(),
		Name: record.Name, Description: record.Description, ObservationIDs: observations, PlaceGeometry: geometry,
		Author: record.Author.String(), UpdatedBy: record.UpdatedBy.String(),
		CreatedAt: record.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if record.Archived() {
		response.ArchivedAt = record.ArchivedAt.UTC().Format(time.RFC3339Nano)
		response.ArchivedBy = record.ArchivedBy.String()
	}
	return response
}

func asResearchRecordSummary(summary recorddomain.BrowseSummary) researchRecordSummaryResponse {
	kinds := map[string]int{}
	for _, kind := range []recorddomain.Kind{recorddomain.Person, recorddomain.Account, recorddomain.Organisation, recorddomain.Place} {
		kinds[kind.String()] = summary.KindCounts[kind]
	}
	return researchRecordSummaryResponse{
		RecordCount: summary.RecordCount, KindCounts: kinds,
		CitedRecordCount: summary.CitedRecordCount, UncitedRecordCount: summary.UncitedRecordCount,
		CitationCount: summary.CitationCount, OpenResolutionRecordCount: summary.OpenResolutionRecordCount,
		AcceptedResolutionRecordCount: summary.AcceptedResolutionRecordCount,
	}
}

func asResearchRecordRevision(revision recorddomain.Revision) researchRecordRevisionResponse {
	observations := make([]string, 0, len(revision.ObservationIDs))
	for _, one := range revision.ObservationIDs {
		observations = append(observations, one.String())
	}
	var geometry *researchPlaceGeometryResponse
	if revision.PlaceGeometry != nil {
		geometryObservations := make([]string, 0, len(revision.PlaceGeometry.ObservationIDs))
		for _, one := range revision.PlaceGeometry.ObservationIDs {
			geometryObservations = append(geometryObservations, one.String())
		}
		geometry = &researchPlaceGeometryResponse{Latitude: revision.PlaceGeometry.Latitude, Longitude: revision.PlaceGeometry.Longitude, Precision: revision.PlaceGeometry.Precision.String(), ObservationIDs: geometryObservations}
	}
	response := researchRecordRevisionResponse{
		RevisionID: revision.ID.String(), WorkspaceID: revision.WorkspaceID.String(), RecordID: revision.RecordID.String(), Revision: revision.Revision,
		Kind: revision.Kind.String(), Name: revision.Name, Description: revision.Description, ObservationIDs: observations, PlaceGeometry: geometry,
		ChangedBy: revision.ChangedBy.String(), ChangedAt: revision.ChangedAt.UTC().Format(time.RFC3339Nano),
	}
	if !revision.ArchivedAt.IsZero() {
		response.ArchivedAt = revision.ArchivedAt.UTC().Format(time.RFC3339Nano)
		response.ArchivedBy = revision.ArchivedBy.String()
	}
	return response
}

type researchRecordRequest struct {
	Kind           string                        `json:"kind"`
	Name           string                        `json:"name"`
	Description    string                        `json:"description"`
	ObservationIDs []id.ID                       `json:"observation_ids"`
	PlaceGeometry  *researchPlaceGeometryRequest `json:"place_geometry"`
}

type researchPlaceGeometryRequest struct {
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	Precision      string  `json:"precision"`
	ObservationIDs []id.ID `json:"observation_ids"`
}

func placeGeometryRequest(input *researchPlaceGeometryRequest) *recorddomain.PlaceGeometry {
	if input == nil {
		return nil
	}
	return &recorddomain.PlaceGeometry{Latitude: input.Latitude, Longitude: input.Longitude, Precision: recorddomain.PlacePrecision(input.Precision), ObservationIDs: input.ObservationIDs}
}

func (m *me) listResearchRecords(w http.ResponseWriter, r *http.Request) {
	caller, workspace, org, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	maxSensitivity, err := m.sourceSensitivityScope(r.Context(), caller, workspace, org)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	kind := recorddomain.Kind(strings.TrimSpace(r.URL.Query().Get("kind")))
	citation := recorddomain.CitationFilter(strings.TrimSpace(r.URL.Query().Get("citation")))
	resolution := recorddomain.ResolutionFilter(strings.TrimSpace(r.URL.Query().Get("resolution")))
	archived := recorddomain.ArchiveActive
	if raw := strings.TrimSpace(r.URL.Query().Get("archived")); raw != "" {
		archived = recorddomain.ArchiveFilter(raw)
	}
	found, err := m.research.records.List(r.Context(), workspace, before, search, kind, citation, resolution, archived, size, maxSensitivity)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	items := make([]researchRecordResponse, 0, len(found.Items))
	for _, one := range found.Items {
		items = append(items, asResearchRecord(one))
	}
	httpx.WriteJSON(w, r, http.StatusOK, struct {
		Items      []researchRecordResponse `json:"items"`
		NextCursor *id.ID                   `json:"next_cursor"`
	}{Items: items, NextCursor: found.NextCursor})
}

func (m *me) summarizeResearchRecords(w http.ResponseWriter, r *http.Request) {
	caller, workspace, org, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	maxSensitivity, err := m.sourceSensitivityScope(r.Context(), caller, workspace, org)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	found, err := m.research.records.Summary(r.Context(), workspace, maxSensitivity)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asResearchRecordSummary(found))
}

func (m *me) readResearchRecord(w http.ResponseWriter, r *http.Request) {
	caller, workspace, org, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	maxSensitivity, err := m.sourceSensitivityScope(r.Context(), caller, workspace, org)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	want, err := id.Parse(r.PathValue("record"))
	if err != nil {
		httpx.Fail(m.log, w, r, recorddomain.ErrNotFound)
		return
	}
	found, err := m.research.records.ByID(r.Context(), workspace, want, maxSensitivity)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asResearchRecord(found))
}

func (m *me) listResearchRecordRevisions(w http.ResponseWriter, r *http.Request) {
	caller, workspace, org, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	maxSensitivity, err := m.sourceSensitivityScope(r.Context(), caller, workspace, org)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	want, err := id.Parse(r.PathValue("record"))
	if err != nil {
		httpx.Fail(m.log, w, r, recorddomain.ErrNotFound)
		return
	}
	if _, err := m.research.records.ByID(r.Context(), workspace, want, maxSensitivity); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	found, err := m.research.records.Revisions(r.Context(), workspace, want, maxSensitivity)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	items := make([]researchRecordRevisionResponse, 0, len(found))
	for _, one := range found {
		items = append(items, asResearchRecordRevision(one))
	}
	httpx.WriteJSON(w, r, http.StatusOK, struct {
		Items []researchRecordRevisionResponse `json:"items"`
	}{Items: items})
}

func (m *me) createResearchRecord(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in researchRecordRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	fresh, err := m.research.recordCmd.CreateWithPlaceGeometry(r.Context(), workspace, caller, in.Kind, in.Name, in.Description, in.ObservationIDs, placeGeometryRequest(in.PlaceGeometry))
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, asResearchRecord(fresh))
}

func (m *me) editResearchRecord(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("record"))
	if err != nil {
		httpx.Fail(m.log, w, r, recorddomain.ErrNotFound)
		return
	}
	var in researchRecordRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	updated, err := m.research.recordCmd.EditWithPlaceGeometry(r.Context(), workspace, want, caller, in.Kind, in.Name, in.Description, in.ObservationIDs, placeGeometryRequest(in.PlaceGeometry))
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asResearchRecord(updated))
}

func (m *me) archiveResearchRecord(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("record"))
	if err != nil {
		httpx.Fail(m.log, w, r, recorddomain.ErrNotFound)
		return
	}
	updated, err := m.research.recordCmd.Archive(r.Context(), workspace, want, caller)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asResearchRecord(updated))
}

func (m *me) restoreResearchRecord(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("record"))
	if err != nil {
		httpx.Fail(m.log, w, r, recorddomain.ErrNotFound)
		return
	}
	updated, err := m.research.recordCmd.Restore(r.Context(), workspace, want, caller)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asResearchRecord(updated))
}
