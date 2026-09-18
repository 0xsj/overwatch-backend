package root

import (
	"net/http"
	"strings"

	assistdomain "github.com/0xsj/overwatch-backend/internal/assistance/domain"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	reviewdomain "github.com/0xsj/overwatch-backend/internal/review/domain"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type synthesisRequest struct {
	ObservationIDs []id.ID `json:"observation_ids"`
}

type relationRequest struct {
	LeftObservationID  id.ID  `json:"left_observation_id"`
	RightObservationID id.ID  `json:"right_observation_id"`
	Kind               string `json:"kind"`
	Rationale          string `json:"rationale"`
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
