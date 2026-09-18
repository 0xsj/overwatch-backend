package root

import (
	"net/http"
	"time"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	connectiondomain "github.com/0xsj/overwatch-backend/internal/researchconnection/domain"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type researchConnectionResponse struct {
	ConnectionID             string   `json:"connection_id"`
	WorkspaceID              string   `json:"workspace_id"`
	FromRecordID             string   `json:"from_record_id"`
	ToRecordID               string   `json:"to_record_id"`
	Kind                     string   `json:"kind"`
	State                    string   `json:"state"`
	Rationale                string   `json:"rationale"`
	SupportingObservationIDs []string `json:"supporting_observation_ids"`
	OpposingObservationIDs   []string `json:"opposing_observation_ids"`
	Author                   string   `json:"author"`
	UpdatedBy                string   `json:"updated_by"`
	CreatedAt                string   `json:"created_at"`
	UpdatedAt                string   `json:"updated_at"`
}

type researchConnectionRevisionResponse struct {
	RevisionID               string   `json:"revision_id"`
	WorkspaceID              string   `json:"workspace_id"`
	ConnectionID             string   `json:"connection_id"`
	Revision                 int      `json:"revision"`
	FromRecordID             string   `json:"from_record_id"`
	FromRecordKind           string   `json:"from_record_kind"`
	FromRecordName           string   `json:"from_record_name"`
	FromRecordDescription    string   `json:"from_record_description,omitempty"`
	FromRecordObservationIDs []string `json:"from_record_observation_ids"`
	ToRecordID               string   `json:"to_record_id"`
	ToRecordKind             string   `json:"to_record_kind"`
	ToRecordName             string   `json:"to_record_name"`
	ToRecordDescription      string   `json:"to_record_description,omitempty"`
	ToRecordObservationIDs   []string `json:"to_record_observation_ids"`
	Kind                     string   `json:"kind"`
	State                    string   `json:"state"`
	Rationale                string   `json:"rationale"`
	SupportingObservationIDs []string `json:"supporting_observation_ids"`
	OpposingObservationIDs   []string `json:"opposing_observation_ids"`
	ChangedBy                string   `json:"changed_by"`
	ChangedAt                string   `json:"changed_at"`
}

func asResearchConnection(connection connectiondomain.Connection) researchConnectionResponse {
	ids := func(input []id.ID) []string {
		out := make([]string, 0, len(input))
		for _, one := range input {
			out = append(out, one.String())
		}
		return out
	}
	return researchConnectionResponse{
		ConnectionID: connection.ID.String(), WorkspaceID: connection.WorkspaceID.String(),
		FromRecordID: connection.FromRecordID.String(), ToRecordID: connection.ToRecordID.String(),
		Kind: connection.Kind.String(), State: connection.State.String(), Rationale: connection.Rationale,
		SupportingObservationIDs: ids(connection.SupportingObservationIDs), OpposingObservationIDs: ids(connection.OpposingObservationIDs),
		Author: connection.Author.String(), UpdatedBy: connection.UpdatedBy.String(),
		CreatedAt: connection.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: connection.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func asResearchConnectionRevision(revision connectiondomain.Revision) researchConnectionRevisionResponse {
	ids := func(input []id.ID) []string {
		out := make([]string, 0, len(input))
		for _, one := range input {
			out = append(out, one.String())
		}
		return out
	}
	return researchConnectionRevisionResponse{
		RevisionID: revision.ID.String(), WorkspaceID: revision.WorkspaceID.String(), ConnectionID: revision.ConnectionID.String(), Revision: revision.Revision,
		FromRecordID: revision.FromRecordID.String(), FromRecordKind: revision.FromRecordKind, FromRecordName: revision.FromRecordName,
		FromRecordDescription: revision.FromRecordDescription, FromRecordObservationIDs: ids(revision.FromRecordObservationIDs),
		ToRecordID: revision.ToRecordID.String(), ToRecordKind: revision.ToRecordKind, ToRecordName: revision.ToRecordName,
		ToRecordDescription: revision.ToRecordDescription, ToRecordObservationIDs: ids(revision.ToRecordObservationIDs),
		Kind: revision.Kind.String(), State: revision.State.String(), Rationale: revision.Rationale,
		SupportingObservationIDs: ids(revision.SupportingObservationIDs), OpposingObservationIDs: ids(revision.OpposingObservationIDs),
		ChangedBy: revision.ChangedBy.String(), ChangedAt: revision.ChangedAt.UTC().Format(time.RFC3339Nano),
	}
}

type researchConnectionRequest struct {
	FromRecordID             id.ID   `json:"from_record_id"`
	ToRecordID               id.ID   `json:"to_record_id"`
	Kind                     string  `json:"kind"`
	State                    string  `json:"state"`
	Rationale                string  `json:"rationale"`
	SupportingObservationIDs []id.ID `json:"supporting_observation_ids"`
	OpposingObservationIDs   []id.ID `json:"opposing_observation_ids"`
}

func (m *me) listResearchConnections(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.connections.List(r.Context(), workspace, before, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	items := make([]researchConnectionResponse, 0, len(found.Items))
	for _, one := range found.Items {
		items = append(items, asResearchConnection(one))
	}
	httpx.WriteJSON(w, r, http.StatusOK, struct {
		Items      []researchConnectionResponse `json:"items"`
		NextCursor *id.ID                       `json:"next_cursor"`
	}{Items: items, NextCursor: found.NextCursor})
}

func (m *me) readResearchConnection(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("connection"))
	if err != nil {
		httpx.Fail(m.log, w, r, connectiondomain.ErrNotFound)
		return
	}
	found, err := m.research.connections.ByID(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asResearchConnection(found))
}

func (m *me) listResearchConnectionRevisions(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("connection"))
	if err != nil {
		httpx.Fail(m.log, w, r, connectiondomain.ErrNotFound)
		return
	}
	if _, err := m.research.connections.ByID(r.Context(), workspace, want); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	found, err := m.research.connections.Revisions(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	items := make([]researchConnectionRevisionResponse, 0, len(found))
	for _, one := range found {
		items = append(items, asResearchConnectionRevision(one))
	}
	httpx.WriteJSON(w, r, http.StatusOK, struct {
		Items []researchConnectionRevisionResponse `json:"items"`
	}{Items: items})
}

func (m *me) readResearchConnectionRevision(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	connection, err := id.Parse(r.PathValue("connection"))
	if err != nil {
		httpx.Fail(m.log, w, r, connectiondomain.ErrNotFound)
		return
	}
	revision, err := id.Parse(r.PathValue("revision"))
	if err != nil {
		httpx.Fail(m.log, w, r, connectiondomain.ErrNotFound)
		return
	}
	found, err := m.research.connections.RevisionByID(r.Context(), workspace, connection, revision)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asResearchConnectionRevision(found))
}

func (m *me) createResearchConnection(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in researchConnectionRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	fresh, err := m.research.connectionCmd.Create(r.Context(), workspace, caller, in.FromRecordID, in.ToRecordID, in.Kind, in.State, in.Rationale, in.SupportingObservationIDs, in.OpposingObservationIDs)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, asResearchConnection(fresh))
}

func (m *me) editResearchConnection(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("connection"))
	if err != nil {
		httpx.Fail(m.log, w, r, connectiondomain.ErrNotFound)
		return
	}
	var in researchConnectionRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	updated, err := m.research.connectionCmd.Edit(r.Context(), workspace, want, caller, in.Kind, in.State, in.Rationale, in.SupportingObservationIDs, in.OpposingObservationIDs)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asResearchConnection(updated))
}
