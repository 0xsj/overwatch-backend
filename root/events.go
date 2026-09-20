package root

import (
	"net/http"
	"time"

	eventdomain "github.com/0xsj/overwatch-backend/internal/event/domain"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type timelineEventRequest struct {
	Title                string                        `json:"title"`
	Description          string                        `json:"description"`
	ReportedTime         string                        `json:"reported_time"`
	TimePrecision        string                        `json:"time_precision"`
	SortDate             string                        `json:"sort_date"`
	Location             string                        `json:"location"`
	ObservationIDs       []id.ID                       `json:"observation_ids"`
	ParticipantRecordIDs []id.ID                       `json:"participant_record_ids"`
	ParticipantLinks     []eventdomain.ParticipantLink `json:"participant_links"`
	LocationRecordID     *id.ID                        `json:"location_record_id"`
}

type timelineEventReconciliationRequest struct {
	Decision          eventdomain.ReconciliationDecision `json:"decision"`
	SelectedAccountID *id.ID                             `json:"selected_account_id"`
	Rationale         string                             `json:"rationale"`
}

type timelineEventClusterRequest struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	EventIDs    []id.ID `json:"event_ids"`
}

type timelineEventClusterReviewRequest struct {
	State eventdomain.ClusterState `json:"state"`
	Note  string                   `json:"note"`
}

type timelineEventRelationshipRequest struct {
	FromEventID              id.ID                        `json:"from_event_id"`
	ToEventID                id.ID                        `json:"to_event_id"`
	Kind                     eventdomain.RelationshipKind `json:"kind"`
	Rationale                string                       `json:"rationale"`
	SupportingObservationIDs []id.ID                      `json:"supporting_observation_ids"`
	OpposingObservationIDs   []id.ID                      `json:"opposing_observation_ids"`
}

type timelineEventRelationshipReviewRequest struct {
	State eventdomain.RelationshipState `json:"state"`
	Note  string                        `json:"note"`
}

func participantLinksFromIDs(input []id.ID) []eventdomain.ParticipantLink {
	links := make([]eventdomain.ParticipantLink, 0, len(input))
	for _, recordID := range input {
		links = append(links, eventdomain.ParticipantLink{RecordID: recordID, Role: eventdomain.ParticipantAssociated})
	}
	return links
}

type timelineEventRecordResponse struct {
	RecordID       string                         `json:"record_id"`
	Kind           string                         `json:"kind"`
	Name           string                         `json:"name"`
	Description    string                         `json:"description,omitempty"`
	ObservationIDs []string                       `json:"observation_ids"`
	PlaceGeometry  *timelinePlaceGeometryResponse `json:"place_geometry,omitempty"`
	Role           string                         `json:"role,omitempty"`
}

type timelinePlaceGeometryResponse struct {
	Latitude       float64  `json:"latitude"`
	Longitude      float64  `json:"longitude"`
	Precision      string   `json:"precision"`
	ObservationIDs []string `json:"observation_ids"`
}

type timelineEventRevisionResponse struct {
	RevisionID           string                        `json:"revision_id"`
	WorkspaceID          string                        `json:"workspace_id"`
	EventID              string                        `json:"event_id"`
	Revision             int                           `json:"revision"`
	Title                string                        `json:"title"`
	Description          string                        `json:"description,omitempty"`
	ReportedTime         string                        `json:"reported_time,omitempty"`
	TimePrecision        string                        `json:"time_precision"`
	SortDate             string                        `json:"sort_date,omitempty"`
	Location             string                        `json:"location,omitempty"`
	ObservationIDs       []string                      `json:"observation_ids"`
	ParticipantRecordIDs []string                      `json:"participant_record_ids"`
	ParticipantLinks     []eventdomain.ParticipantLink `json:"participant_links"`
	ParticipantRecords   []timelineEventRecordResponse `json:"participant_records"`
	LocationRecordID     string                        `json:"location_record_id,omitempty"`
	LocationRecord       *timelineEventRecordResponse  `json:"location_record,omitempty"`
	ChangedBy            string                        `json:"changed_by"`
	ChangedAt            string                        `json:"changed_at"`
}

func asTimelineEventRevision(revision eventdomain.Revision) timelineEventRevisionResponse {
	ids := func(input []id.ID) []string {
		out := make([]string, 0, len(input))
		for _, one := range input {
			out = append(out, one.String())
		}
		return out
	}
	record := func(input eventdomain.RecordSnapshot) timelineEventRecordResponse {
		var geometry *timelinePlaceGeometryResponse
		if input.PlaceGeometry != nil {
			geometry = &timelinePlaceGeometryResponse{Latitude: input.PlaceGeometry.Latitude, Longitude: input.PlaceGeometry.Longitude, Precision: input.PlaceGeometry.Precision, ObservationIDs: ids(input.PlaceGeometry.ObservationIDs)}
		}
		return timelineEventRecordResponse{RecordID: input.ID.String(), Kind: input.Kind, Name: input.Name, Description: input.Description, ObservationIDs: ids(input.ObservationIDs), PlaceGeometry: geometry, Role: input.Role.String()}
	}
	participants := make([]timelineEventRecordResponse, 0, len(revision.ParticipantRecords))
	for _, one := range revision.ParticipantRecords {
		participants = append(participants, record(one))
	}
	out := timelineEventRevisionResponse{
		RevisionID: revision.ID.String(), WorkspaceID: revision.WorkspaceID.String(), EventID: revision.EventID.String(), Revision: revision.Revision,
		Title: revision.Title, Description: revision.Description, ReportedTime: revision.ReportedTime, TimePrecision: revision.TimePrecision.String(), SortDate: revision.SortDate, Location: revision.Location,
		ObservationIDs: ids(revision.ObservationIDs), ParticipantRecordIDs: ids(revision.ParticipantRecordIDs), ParticipantLinks: revision.ParticipantLinks, ParticipantRecords: participants,
		ChangedBy: revision.ChangedBy.String(), ChangedAt: revision.ChangedAt.UTC().Format(time.RFC3339Nano),
	}
	if revision.LocationRecordID != nil {
		out.LocationRecordID = revision.LocationRecordID.String()
	}
	if revision.LocationRecord != nil {
		copy := record(*revision.LocationRecord)
		out.LocationRecord = &copy
	}
	return out
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

func (m *me) listTimelineEventRevisions(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	event, err := id.Parse(r.PathValue("event"))
	if err != nil {
		httpx.Fail(m.log, w, r, eventdomain.ErrNotFound)
		return
	}
	if _, err := m.research.events.ByID(r.Context(), workspace, event); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	found, err := m.research.events.Revisions(r.Context(), workspace, event)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	items := make([]timelineEventRevisionResponse, 0, len(found))
	for _, one := range found {
		items = append(items, asTimelineEventRevision(one))
	}
	httpx.WriteJSON(w, r, http.StatusOK, struct {
		Items []timelineEventRevisionResponse `json:"items"`
	}{Items: items})
}

func (m *me) readTimelineEventRevision(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	event, err := id.Parse(r.PathValue("event"))
	if err != nil {
		httpx.Fail(m.log, w, r, eventdomain.ErrNotFound)
		return
	}
	revision, err := id.Parse(r.PathValue("revision"))
	if err != nil {
		httpx.Fail(m.log, w, r, eventdomain.ErrNotFound)
		return
	}
	found, err := m.research.events.RevisionByID(r.Context(), workspace, event, revision)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asTimelineEventRevision(found))
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
	participants := in.ParticipantLinks
	if len(participants) == 0 && len(in.ParticipantRecordIDs) > 0 {
		participants = participantLinksFromIDs(in.ParticipantRecordIDs)
	}
	fresh, err := m.research.eventCmd.CreateWithParticipantLinks(r.Context(), workspace, caller, in.Title, in.Description, in.ReportedTime, in.TimePrecision, in.SortDate, in.Location, in.ObservationIDs, participants, in.LocationRecordID)
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
	participants := in.ParticipantLinks
	if len(participants) == 0 && len(in.ParticipantRecordIDs) > 0 {
		participants = participantLinksFromIDs(in.ParticipantRecordIDs)
	}
	updated, err := m.research.eventCmd.EditWithParticipantLinks(r.Context(), workspace, want, caller, in.Title, in.Description, in.ReportedTime, in.TimePrecision, in.SortDate, in.Location, in.ObservationIDs, participants, in.LocationRecordID)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, updated)
}

func (m *me) listTimelineEventAccounts(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	event, err := id.Parse(r.PathValue("event"))
	if err != nil {
		httpx.Fail(m.log, w, r, eventdomain.ErrNotFound)
		return
	}
	found, err := m.research.eventAccounts.List(r.Context(), workspace, event)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) createTimelineEventAccount(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	event, err := id.Parse(r.PathValue("event"))
	if err != nil {
		httpx.Fail(m.log, w, r, eventdomain.ErrNotFound)
		return
	}
	var in timelineEventRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	fresh, err := m.research.eventAccountCmd.Create(r.Context(), workspace, event, caller, in.Title, in.Description, in.ReportedTime, in.TimePrecision, in.SortDate, in.Location, in.ObservationIDs, in.ParticipantRecordIDs, in.LocationRecordID)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, fresh)
}

func (m *me) reconcileTimelineEventAccounts(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	event, err := id.Parse(r.PathValue("event"))
	if err != nil {
		httpx.Fail(m.log, w, r, eventdomain.ErrNotFound)
		return
	}
	var in timelineEventReconciliationRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	found, err := m.research.eventAccountCmd.Reconcile(r.Context(), workspace, event, caller, in.Decision, in.SelectedAccountID, in.Rationale)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) listTimelineEventClusters(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.eventClusters.List(r.Context(), workspace, before, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) readTimelineEventCluster(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	cluster, err := id.Parse(r.PathValue("cluster"))
	if err != nil {
		httpx.Fail(m.log, w, r, eventdomain.ErrNotFound)
		return
	}
	found, err := m.research.eventClusters.ByID(r.Context(), workspace, cluster)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) createTimelineEventCluster(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in timelineEventClusterRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	fresh, err := m.research.eventClusterCmd.Create(r.Context(), workspace, caller, in.Title, in.Description, in.EventIDs)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, fresh)
}

func (m *me) editTimelineEventCluster(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	cluster, err := id.Parse(r.PathValue("cluster"))
	if err != nil {
		httpx.Fail(m.log, w, r, eventdomain.ErrNotFound)
		return
	}
	var in timelineEventClusterRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	found, err := m.research.eventClusterCmd.Edit(r.Context(), workspace, cluster, caller, in.Title, in.Description, in.EventIDs)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) reviewTimelineEventCluster(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	cluster, err := id.Parse(r.PathValue("cluster"))
	if err != nil {
		httpx.Fail(m.log, w, r, eventdomain.ErrNotFound)
		return
	}
	var in timelineEventClusterReviewRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	found, err := m.research.eventClusterCmd.Review(r.Context(), workspace, cluster, caller, in.State, in.Note)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) listTimelineEventRelationships(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.eventRelationships.List(r.Context(), workspace, before, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) readTimelineEventRelationship(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	relationship, err := id.Parse(r.PathValue("relationship"))
	if err != nil {
		httpx.Fail(m.log, w, r, eventdomain.ErrNotFound)
		return
	}
	found, err := m.research.eventRelationships.ByID(r.Context(), workspace, relationship)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) createTimelineEventRelationship(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in timelineEventRelationshipRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	fresh, err := m.research.eventRelationshipCmd.CreateWithEvidence(r.Context(), workspace, caller, in.FromEventID, in.ToEventID, in.Kind, in.Rationale, in.SupportingObservationIDs, in.OpposingObservationIDs)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, fresh)
}

func (m *me) reviewTimelineEventRelationship(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	relationship, err := id.Parse(r.PathValue("relationship"))
	if err != nil {
		httpx.Fail(m.log, w, r, eventdomain.ErrNotFound)
		return
	}
	var in timelineEventRelationshipReviewRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	found, err := m.research.eventRelationshipCmd.Review(r.Context(), workspace, relationship, caller, in.State, in.Note)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}
