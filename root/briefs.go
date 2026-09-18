package root

import (
	"errors"
	"net/http"
	"time"

	briefdomain "github.com/0xsj/overwatch-backend/internal/brief/domain"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type workingBriefRequest struct {
	Title          string  `json:"title"`
	Question       string  `json:"question"`
	CurrentAccount string  `json:"current_account"`
	Alternatives   string  `json:"alternatives"`
	Limitations    string  `json:"limitations"`
	NextSteps      string  `json:"next_steps"`
	ObservationIDs []id.ID `json:"observation_ids"`
	QuestionIDs    []id.ID `json:"question_ids"`
	ConnectionIDs  []id.ID `json:"connection_ids"`
}

type workingBriefResponse struct {
	BriefID        string   `json:"brief_id"`
	WorkspaceID    string   `json:"workspace_id"`
	Title          string   `json:"title"`
	Question       string   `json:"question"`
	CurrentAccount string   `json:"current_account,omitempty"`
	Alternatives   string   `json:"alternatives,omitempty"`
	Limitations    string   `json:"limitations,omitempty"`
	NextSteps      string   `json:"next_steps,omitempty"`
	ObservationIDs []string `json:"observation_ids"`
	QuestionIDs    []string `json:"question_ids"`
	ConnectionIDs  []string `json:"connection_ids"`
	Author         string   `json:"author"`
	UpdatedBy      string   `json:"updated_by"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

type snapshotQuestionResponse struct {
	QuestionID     string   `json:"question_id"`
	Question       string   `json:"question"`
	State          string   `json:"state"`
	Resolution     string   `json:"resolution,omitempty"`
	ObservationIDs []string `json:"observation_ids"`
}

type snapshotConnectionResponse struct {
	ConnectionID             string   `json:"connection_id"`
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
}

type briefSnapshotResponse struct {
	SnapshotID      string                       `json:"snapshot_id"`
	WorkspaceID     string                       `json:"workspace_id"`
	BriefID         string                       `json:"brief_id"`
	Title           string                       `json:"title"`
	Question        string                       `json:"question"`
	CurrentAccount  string                       `json:"current_account,omitempty"`
	Alternatives    string                       `json:"alternatives,omitempty"`
	Limitations     string                       `json:"limitations,omitempty"`
	NextSteps       string                       `json:"next_steps,omitempty"`
	ObservationIDs  []string                     `json:"observation_ids"`
	Questions       []snapshotQuestionResponse   `json:"questions"`
	Connections     []snapshotConnectionResponse `json:"connections"`
	Author          string                       `json:"author"`
	UpdatedBy       string                       `json:"updated_by"`
	FrozenBy        string                       `json:"frozen_by"`
	SourceUpdatedAt string                       `json:"source_updated_at"`
	FrozenAt        string                       `json:"frozen_at"`
}

func asWorkingBrief(b briefdomain.Brief) workingBriefResponse {
	observations := make([]string, 0, len(b.ObservationIDs))
	for _, one := range b.ObservationIDs {
		observations = append(observations, one.String())
	}
	questions := make([]string, 0, len(b.QuestionIDs))
	for _, one := range b.QuestionIDs {
		questions = append(questions, one.String())
	}
	connections := make([]string, 0, len(b.ConnectionIDs))
	for _, one := range b.ConnectionIDs {
		connections = append(connections, one.String())
	}
	return workingBriefResponse{
		BriefID: b.ID.String(), WorkspaceID: b.WorkspaceID.String(), Title: b.Title, Question: b.Question,
		CurrentAccount: b.CurrentAccount, Alternatives: b.Alternatives, Limitations: b.Limitations, NextSteps: b.NextSteps,
		ObservationIDs: observations, QuestionIDs: questions, ConnectionIDs: connections, Author: b.Author.String(), UpdatedBy: b.UpdatedBy.String(),
		CreatedAt: b.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: b.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func asBriefSnapshot(s briefdomain.Snapshot) briefSnapshotResponse {
	observations := make([]string, 0, len(s.ObservationIDs))
	for _, one := range s.ObservationIDs {
		observations = append(observations, one.String())
	}
	questions := make([]snapshotQuestionResponse, 0, len(s.Questions))
	for _, one := range s.Questions {
		observations := make([]string, 0, len(one.ObservationIDs))
		for _, observation := range one.ObservationIDs {
			observations = append(observations, observation.String())
		}
		questions = append(questions, snapshotQuestionResponse{QuestionID: one.ID.String(), Question: one.Prompt, State: one.State, Resolution: one.Resolution, ObservationIDs: observations})
	}
	connections := make([]snapshotConnectionResponse, 0, len(s.Connections))
	for _, one := range s.Connections {
		fromRecordObservations := make([]string, 0, len(one.FromRecordObservationIDs))
		for _, observation := range one.FromRecordObservationIDs {
			fromRecordObservations = append(fromRecordObservations, observation.String())
		}
		toRecordObservations := make([]string, 0, len(one.ToRecordObservationIDs))
		for _, observation := range one.ToRecordObservationIDs {
			toRecordObservations = append(toRecordObservations, observation.String())
		}
		supporting := make([]string, 0, len(one.SupportingObservationIDs))
		for _, observation := range one.SupportingObservationIDs {
			supporting = append(supporting, observation.String())
		}
		opposing := make([]string, 0, len(one.OpposingObservationIDs))
		for _, observation := range one.OpposingObservationIDs {
			opposing = append(opposing, observation.String())
		}
		connections = append(connections, snapshotConnectionResponse{
			ConnectionID: one.ID.String(), FromRecordID: one.FromRecordID.String(), FromRecordKind: one.FromRecordKind, FromRecordName: one.FromRecordName,
			FromRecordDescription: one.FromRecordDescription, FromRecordObservationIDs: fromRecordObservations,
			ToRecordID: one.ToRecordID.String(), ToRecordKind: one.ToRecordKind, ToRecordName: one.ToRecordName,
			ToRecordDescription: one.ToRecordDescription, ToRecordObservationIDs: toRecordObservations,
			Kind: one.Kind, State: one.State, Rationale: one.Rationale, SupportingObservationIDs: supporting, OpposingObservationIDs: opposing,
		})
	}
	return briefSnapshotResponse{
		SnapshotID: s.ID.String(), WorkspaceID: s.WorkspaceID.String(), BriefID: s.BriefID.String(), Title: s.Title, Question: s.Question,
		CurrentAccount: s.CurrentAccount, Alternatives: s.Alternatives, Limitations: s.Limitations, NextSteps: s.NextSteps,
		ObservationIDs: observations, Questions: questions, Connections: connections, Author: s.Author.String(), UpdatedBy: s.UpdatedBy.String(), FrozenBy: s.FrozenBy.String(),
		SourceUpdatedAt: s.SourceUpdatedAt.UTC().Format(time.RFC3339Nano), FrozenAt: s.FrozenAt.UTC().Format(time.RFC3339Nano),
	}
}

func (m *me) readWorkingBrief(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	found, err := m.research.brief.ByWorkspace(r.Context(), workspace)
	if errors.Is(err, briefdomain.ErrNotFound) {
		httpx.WriteJSON(w, r, http.StatusOK, nil)
		return
	}
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asWorkingBrief(found))
}

func (m *me) saveWorkingBrief(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in workingBriefRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	saved, err := m.research.briefCmd.Save(r.Context(), workspace, caller, in.Title, in.Question, in.CurrentAccount, in.Alternatives, in.Limitations, in.NextSteps, in.ObservationIDs, in.QuestionIDs, in.ConnectionIDs)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asWorkingBrief(saved))
}

func (m *me) listBriefSnapshots(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.brief.Snapshots(r.Context(), workspace, before, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	items := make([]briefSnapshotResponse, 0, len(found.Items))
	for _, one := range found.Items {
		items = append(items, asBriefSnapshot(one))
	}
	httpx.WriteJSON(w, r, http.StatusOK, struct {
		Items      []briefSnapshotResponse `json:"items"`
		NextCursor *id.ID                  `json:"next_cursor"`
	}{Items: items, NextCursor: found.NextCursor})
}

func (m *me) createBriefSnapshot(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	snapshot, err := m.research.briefCmd.Freeze(r.Context(), workspace, caller)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, asBriefSnapshot(snapshot))
}

func (m *me) readBriefSnapshot(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("snapshot"))
	if err != nil {
		httpx.Fail(m.log, w, r, briefdomain.ErrNotFound)
		return
	}
	found, err := m.research.brief.SnapshotByID(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asBriefSnapshot(found))
}
