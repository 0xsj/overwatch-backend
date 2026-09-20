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

type researchResolutionSetResponse struct {
	ResolutionSetID               string   `json:"resolution_set_id"`
	WorkspaceID                   string   `json:"workspace_id"`
	AliasRecordIDs                []string `json:"alias_record_ids"`
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

type researchResolutionImpactResponse struct {
	Connections []researchResolutionConnectionImpactResponse `json:"connections"`
	Events      []researchResolutionEventImpactResponse      `json:"events"`
	Briefs      []researchResolutionBriefImpactResponse      `json:"briefs"`
	Snapshots   []researchResolutionSnapshotImpactResponse   `json:"snapshots"`
}

type researchResolutionConnectionImpactResponse struct {
	ConnectionID   string `json:"connection_id"`
	FromRecordID   string `json:"from_record_id"`
	FromRecordName string `json:"from_record_name"`
	ToRecordID     string `json:"to_record_id"`
	ToRecordName   string `json:"to_record_name"`
	Kind           string `json:"kind"`
	State          string `json:"state"`
}

type researchResolutionEventImpactResponse struct {
	EventID  string `json:"event_id"`
	Title    string `json:"title"`
	SortDate string `json:"sort_date,omitempty"`
}

type researchResolutionBriefImpactResponse struct {
	BriefID   string `json:"brief_id"`
	Title     string `json:"title"`
	UpdatedAt string `json:"updated_at"`
}

type researchResolutionSnapshotImpactResponse struct {
	SnapshotID string `json:"snapshot_id"`
	BriefID    string `json:"brief_id"`
	Title      string `json:"title"`
	FrozenAt   string `json:"frozen_at"`
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

func asResearchResolutionSet(resolution resolutiondomain.ResolutionSet) researchResolutionSetResponse {
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
	return researchResolutionSetResponse{
		ResolutionSetID: resolution.ID.String(), WorkspaceID: idString(resolution.WorkspaceID), AliasRecordIDs: ids(resolution.AliasRecordIDs), CanonicalRecordID: idString(resolution.CanonicalRecordID), State: resolution.State.String(), Rationale: resolution.Rationale,
		ProposedBy: idString(resolution.ProposedBy), ProposedAt: format(resolution.ProposedAt), ReviewedBy: idString(resolution.ReviewedBy), ReviewedAt: formatPtr(resolution.ReviewedAt), ReversedBy: idString(resolution.ReversedBy), ReversedAt: formatPtr(resolution.ReversedAt),
		CanonicalObservationIDsBefore: ids(resolution.CanonicalObservationIDsBefore), AddedObservationIDs: ids(resolution.AddedObservationIDs), CanonicalObservationIDsAfter: ids(resolution.CanonicalObservationIDsAfter()),
	}
}

func asResearchResolutionImpact(impact resolutiondomain.Impact) researchResolutionImpactResponse {
	format := func(value time.Time) string {
		if value.IsZero() {
			return ""
		}
		return value.UTC().Format(time.RFC3339Nano)
	}
	out := researchResolutionImpactResponse{
		Connections: make([]researchResolutionConnectionImpactResponse, 0, len(impact.Connections)),
		Events:      make([]researchResolutionEventImpactResponse, 0, len(impact.Events)),
		Briefs:      make([]researchResolutionBriefImpactResponse, 0, len(impact.Briefs)),
		Snapshots:   make([]researchResolutionSnapshotImpactResponse, 0, len(impact.Snapshots)),
	}
	for _, connection := range impact.Connections {
		out.Connections = append(out.Connections, researchResolutionConnectionImpactResponse{ConnectionID: connection.ID.String(), FromRecordID: connection.FromRecordID.String(), FromRecordName: connection.FromRecordName, ToRecordID: connection.ToRecordID.String(), ToRecordName: connection.ToRecordName, Kind: connection.Kind, State: connection.State})
	}
	for _, event := range impact.Events {
		out.Events = append(out.Events, researchResolutionEventImpactResponse{EventID: event.ID.String(), Title: event.Title, SortDate: event.SortDate})
	}
	for _, brief := range impact.Briefs {
		out.Briefs = append(out.Briefs, researchResolutionBriefImpactResponse{BriefID: brief.ID.String(), Title: brief.Title, UpdatedAt: format(brief.UpdatedAt)})
	}
	for _, snapshot := range impact.Snapshots {
		out.Snapshots = append(out.Snapshots, researchResolutionSnapshotImpactResponse{SnapshotID: snapshot.ID.String(), BriefID: snapshot.BriefID.String(), Title: snapshot.Title, FrozenAt: format(snapshot.FrozenAt)})
	}
	return out
}

type researchResolutionRequest struct {
	CanonicalRecordID id.ID  `json:"canonical_record_id"`
	Rationale         string `json:"rationale"`
}

type researchResolutionSetRequest struct {
	CanonicalRecordID id.ID   `json:"canonical_record_id"`
	AliasRecordIDs    []id.ID `json:"alias_record_ids"`
	Rationale         string  `json:"rationale"`
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

func (m *me) readResearchResolutionImpact(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("resolution"))
	if err != nil {
		httpx.Fail(m.log, w, r, resolutiondomain.ErrNotFound)
		return
	}
	found, err := m.research.resolutions.Impact(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asResearchResolutionImpact(found))
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

func (m *me) listResearchResolutionSets(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.resolutions.ListSets(r.Context(), workspace, before, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	items := make([]researchResolutionSetResponse, 0, len(found.Items))
	for _, one := range found.Items {
		items = append(items, asResearchResolutionSet(one))
	}
	httpx.WriteJSON(w, r, http.StatusOK, struct {
		Items      []researchResolutionSetResponse `json:"items"`
		NextCursor *id.ID                          `json:"next_cursor"`
	}{Items: items, NextCursor: found.NextCursor})
}

func (m *me) readResearchResolutionSet(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("resolution_set"))
	if err != nil {
		httpx.Fail(m.log, w, r, resolutiondomain.ErrNotFound)
		return
	}
	found, err := m.research.resolutions.SetByID(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asResearchResolutionSet(found))
}

func (m *me) readResearchResolutionSetImpact(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("resolution_set"))
	if err != nil {
		httpx.Fail(m.log, w, r, resolutiondomain.ErrNotFound)
		return
	}
	found, err := m.research.resolutions.SetImpact(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asResearchResolutionImpact(found))
}

func (m *me) createResearchResolutionSet(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in researchResolutionSetRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	fresh, err := m.research.resolutionCmd.ProposeSet(r.Context(), workspace, in.CanonicalRecordID, in.AliasRecordIDs, caller, in.Rationale)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, asResearchResolutionSet(fresh))
}

func (m *me) reviewResearchResolutionSet(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("resolution_set"))
	if err != nil {
		httpx.Fail(m.log, w, r, resolutiondomain.ErrNotFound)
		return
	}
	var in researchResolutionDecisionRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	found, err := m.research.resolutionCmd.ReviewSet(r.Context(), workspace, want, caller, in.Decision)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asResearchResolutionSet(found))
}

func (m *me) reverseResearchResolutionSet(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("resolution_set"))
	if err != nil {
		httpx.Fail(m.log, w, r, resolutiondomain.ErrNotFound)
		return
	}
	found, err := m.research.resolutionCmd.ReverseSet(r.Context(), workspace, want, caller)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asResearchResolutionSet(found))
}
