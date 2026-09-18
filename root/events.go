package root

import (
	"net/http"

	eventdomain "github.com/0xsj/overwatch-backend/internal/event/domain"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type timelineEventRequest struct {
	Title          string  `json:"title"`
	Description    string  `json:"description"`
	ReportedTime   string  `json:"reported_time"`
	TimePrecision  string  `json:"time_precision"`
	SortDate       string  `json:"sort_date"`
	Location       string  `json:"location"`
	ObservationIDs []id.ID `json:"observation_ids"`
}

func (m *me) listTimelineEvents(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.events.List(r.Context(), workspace, before, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) readTimelineEvent(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("event"))
	if err != nil {
		httpx.Fail(m.log, w, r, eventdomain.ErrNotFound)
		return
	}
	found, err := m.research.events.ByID(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) createTimelineEvent(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in timelineEventRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	fresh, err := m.research.eventCmd.Create(r.Context(), workspace, caller, in.Title, in.Description, in.ReportedTime, in.TimePrecision, in.SortDate, in.Location, in.ObservationIDs)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, fresh)
}

func (m *me) editTimelineEvent(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("event"))
	if err != nil {
		httpx.Fail(m.log, w, r, eventdomain.ErrNotFound)
		return
	}
	var in timelineEventRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	updated, err := m.research.eventCmd.Edit(r.Context(), workspace, want, caller, in.Title, in.Description, in.ReportedTime, in.TimePrecision, in.SortDate, in.Location, in.ObservationIDs)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, updated)
}
