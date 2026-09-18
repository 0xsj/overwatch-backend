package root

import (
	"net/http"
	"time"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	recorddomain "github.com/0xsj/overwatch-backend/internal/researchentity/domain"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type researchRecordResponse struct {
	RecordID       string   `json:"record_id"`
	WorkspaceID    string   `json:"workspace_id"`
	Kind           string   `json:"kind"`
	Name           string   `json:"name"`
	Description    string   `json:"description,omitempty"`
	ObservationIDs []string `json:"observation_ids"`
	Author         string   `json:"author"`
	UpdatedBy      string   `json:"updated_by"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

func asResearchRecord(record recorddomain.Record) researchRecordResponse {
	observations := make([]string, 0, len(record.ObservationIDs))
	for _, one := range record.ObservationIDs {
		observations = append(observations, one.String())
	}
	return researchRecordResponse{
		RecordID: record.ID.String(), WorkspaceID: record.WorkspaceID.String(), Kind: record.Kind.String(),
		Name: record.Name, Description: record.Description, ObservationIDs: observations,
		Author: record.Author.String(), UpdatedBy: record.UpdatedBy.String(),
		CreatedAt: record.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: record.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

type researchRecordRequest struct {
	Kind           string  `json:"kind"`
	Name           string  `json:"name"`
	Description    string  `json:"description"`
	ObservationIDs []id.ID `json:"observation_ids"`
}

func (m *me) listResearchRecords(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.records.List(r.Context(), workspace, before, size)
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

func (m *me) readResearchRecord(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("record"))
	if err != nil {
		httpx.Fail(m.log, w, r, recorddomain.ErrNotFound)
		return
	}
	found, err := m.research.records.ByID(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asResearchRecord(found))
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
	fresh, err := m.research.recordCmd.Create(r.Context(), workspace, caller, in.Kind, in.Name, in.Description, in.ObservationIDs)
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
	updated, err := m.research.recordCmd.Edit(r.Context(), workspace, want, caller, in.Kind, in.Name, in.Description, in.ObservationIDs)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asResearchRecord(updated))
}
