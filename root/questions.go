package root

import (
	"net/http"
	"time"

	leadquery "github.com/0xsj/overwatch-backend/internal/lead/app/query"
	leaddomain "github.com/0xsj/overwatch-backend/internal/lead/domain"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type questionResponse struct {
	QuestionID     string   `json:"question_id"`
	WorkspaceID    string   `json:"workspace_id"`
	Question       string   `json:"question"`
	Context        string   `json:"context,omitempty"`
	State          string   `json:"state"`
	Resolution     string   `json:"resolution,omitempty"`
	Author         string   `json:"author"`
	UpdatedBy      string   `json:"updated_by"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
	ObservationIDs []string `json:"observation_ids"`
}

func asQuestion(q leaddomain.Question) questionResponse {
	observations := make([]string, 0, len(q.ObservationIDs))
	for _, observation := range q.ObservationIDs {
		observations = append(observations, observation.String())
	}
	return questionResponse{
		QuestionID: q.ID.String(), WorkspaceID: q.WorkspaceID.String(),
		Question: q.Prompt, Context: q.Context, State: q.State.String(),
		Resolution: q.Resolution, Author: q.Author.String(), UpdatedBy: q.UpdatedBy.String(),
		CreatedAt:      q.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:      q.UpdatedAt.UTC().Format(time.RFC3339Nano),
		ObservationIDs: observations,
	}
}

type questionRequest struct {
	Question       string  `json:"question"`
	Context        string  `json:"context"`
	State          string  `json:"state"`
	Resolution     string  `json:"resolution"`
	ObservationIDs []id.ID `json:"observation_ids"`
}

func (m *me) listQuestions(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	state := r.URL.Query().Get("state")
	var found leadquery.Page
	var err error
	if state == "" {
		found, err = m.research.questions.List(r.Context(), workspace, before, size)
	} else {
		found, err = m.research.questions.Filtered(r.Context(), workspace, before, state, size)
	}
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := make([]questionResponse, 0, len(found.Items))
	for _, question := range found.Items {
		out = append(out, asQuestion(question))
	}
	httpx.WriteJSON(w, r, http.StatusOK, struct {
		Items      []questionResponse `json:"items"`
		NextCursor *id.ID             `json:"next_cursor"`
	}{Items: out, NextCursor: found.NextCursor})
}

func (m *me) readQuestion(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("question"))
	if err != nil {
		httpx.Fail(m.log, w, r, leaddomain.ErrNotFound)
		return
	}
	found, err := m.research.questions.ByID(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asQuestion(found))
}

func (m *me) createQuestion(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in questionRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	if in.State == "" {
		in.State = leaddomain.Open.String()
	}
	created, err := m.research.questionCmd.Create(r.Context(), workspace, caller,
		in.Question, in.Context, in.State, in.Resolution, in.ObservationIDs)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, asQuestion(created))
}

func (m *me) editQuestion(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("question"))
	if err != nil {
		httpx.Fail(m.log, w, r, leaddomain.ErrNotFound)
		return
	}
	var in questionRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	updated, err := m.research.questionCmd.Edit(r.Context(), workspace, want, caller,
		in.Question, in.Context, in.State, in.Resolution, in.ObservationIDs)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asQuestion(updated))
}
