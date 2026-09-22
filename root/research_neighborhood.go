package root

import (
	stderrors "errors"
	"net/http"
	"strconv"
	"strings"

	eventdomain "github.com/0xsj/overwatch-backend/internal/event/domain"
	connectiondomain "github.com/0xsj/overwatch-backend/internal/researchconnection/domain"
	recorddomain "github.com/0xsj/overwatch-backend/internal/researchentity/domain"
	reviewdomain "github.com/0xsj/overwatch-backend/internal/review/domain"
	owerrors "github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

const neighborhoodLimit = 50
const neighborhoodMaxDepth = 2

type researchRecordNeighborhoodMeta struct {
	Depth           int  `json:"depth"`
	MaxDepth        int  `json:"max_depth"`
	RecordLimit     int  `json:"record_limit"`
	Truncated       bool `json:"truncated"`
	RecordCount     int  `json:"record_count"`
	ConnectionCount int  `json:"connection_count"`
	EventCount      int  `json:"event_count"`
	CitationCount   int  `json:"citation_count"`
}

// researchRecordNeighborhoodResponse is a bounded, read-only join of the
// authored surfaces around one record. It is deliberately composed in root:
// records, connections, timeline events, and evidence remain independent
// domains, while this response gives the investigation UI one navigable read.
type researchRecordNeighborhoodResponse struct {
	Meta        researchRecordNeighborhoodMeta `json:"meta"`
	Record      researchRecordResponse         `json:"record"`
	Records     []researchRecordResponse       `json:"records"`
	Connections []researchConnectionResponse   `json:"connections"`
	Events      []eventdomain.Event            `json:"events"`
	Citations   []reviewdomain.Evidence        `json:"citations"`
}

func neighborhoodParams(r *http.Request) (int, int, error) {
	depth, limit := 1, neighborhoodLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("depth")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > neighborhoodMaxDepth {
			return 0, 0, owerrors.New(owerrors.Invalid, "neighborhood depth must be 1 or 2")
		}
		depth = parsed
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > neighborhoodLimit {
			return 0, 0, owerrors.New(owerrors.Invalid, "neighborhood limit must be between 1 and 50")
		}
		limit = parsed
	}
	return depth, limit, nil
}

func (m *me) readResearchRecordNeighborhood(w http.ResponseWriter, r *http.Request) {
	caller, workspace, org, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	maxSensitivity, err := m.sourceSensitivityScope(r.Context(), caller, workspace, org)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	depth, limit, err := neighborhoodParams(r)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	want, err := id.Parse(r.PathValue("record"))
	if err != nil {
		httpx.Fail(m.log, w, r, recorddomain.ErrNotFound)
		return
	}
	anchor, err := m.research.records.ByID(r.Context(), workspace, want, maxSensitivity)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}

	maxRecords := limit + 1 // include the anchor outside the requested neighbor limit
	recordIDs := []id.ID{anchor.ID}
	seenRecords := map[id.ID]struct{}{anchor.ID: {}}
	truncated := false
	addRecord := func(record id.ID) bool {
		if record.IsZero() {
			return false
		}
		if _, exists := seenRecords[record]; exists {
			return true
		}
		if len(recordIDs) >= maxRecords {
			truncated = true
			return false
		}
		seenRecords[record] = struct{}{}
		recordIDs = append(recordIDs, record)
		return true
	}

	connections := make([]connectiondomain.Connection, 0)
	seenConnections := map[id.ID]struct{}{}
	events := make([]eventdomain.Event, 0)
	seenEvents := map[id.ID]struct{}{}
	frontier := []id.ID{anchor.ID}
	for level := 1; level <= depth && len(frontier) > 0; level++ {
		next := make([]id.ID, 0)
		seenNext := map[id.ID]struct{}{}
		queueNext := func(record id.ID) {
			if record.IsZero() || record == anchor.ID {
				return
			}
			if _, exists := seenNext[record]; exists {
				return
			}
			seenNext[record] = struct{}{}
			next = append(next, record)
		}

		for _, current := range frontier {
			foundConnections, err := m.research.connections.ForRecord(r.Context(), workspace, current, neighborhoodLimit, maxSensitivity)
			if err != nil {
				httpx.Fail(m.log, w, r, err)
				return
			}
			for _, connection := range foundConnections {
				fromVisible := addRecord(connection.FromRecordID)
				toVisible := addRecord(connection.ToRecordID)
				if !fromVisible || !toVisible {
					continue
				}
				if _, exists := seenConnections[connection.ID]; !exists {
					seenConnections[connection.ID] = struct{}{}
					connections = append(connections, connection)
				}
				if level < depth {
					queueNext(connection.FromRecordID)
					queueNext(connection.ToRecordID)
				}
			}

			foundEvents, err := m.research.events.ForRecord(r.Context(), workspace, current, neighborhoodLimit, maxSensitivity)
			if err != nil {
				httpx.Fail(m.log, w, r, err)
				return
			}
			for _, event := range foundEvents {
				allRecordsVisible := true
				for _, participant := range event.ParticipantRecordIDs {
					if !addRecord(participant) {
						allRecordsVisible = false
					}
				}
				if event.LocationRecordID != nil && !addRecord(*event.LocationRecordID) {
					allRecordsVisible = false
				}
				if !allRecordsVisible {
					continue
				}
				if _, exists := seenEvents[event.ID]; !exists {
					seenEvents[event.ID] = struct{}{}
					events = append(events, event)
				}
				if level < depth {
					for _, participant := range event.ParticipantRecordIDs {
						queueNext(participant)
					}
					if event.LocationRecordID != nil {
						queueNext(*event.LocationRecordID)
					}
				}
			}
		}
		frontier = next
	}

	records := map[id.ID]recorddomain.Record{anchor.ID: anchor}
	for _, recordID := range recordIDs[1:] {
		found, err := m.research.records.ByID(r.Context(), workspace, recordID, maxSensitivity)
		if err != nil {
			if stderrors.Is(err, recorddomain.ErrNotFound) {
				continue
			}
			httpx.Fail(m.log, w, r, err)
			return
		}
		records[recordID] = found
	}

	observationIDs := make([]id.ID, 0, 12+len(connections)*24+len(events)*8)
	seenObservations := map[id.ID]struct{}{}
	addObservation := func(observation id.ID) {
		if observation.IsZero() {
			return
		}
		if _, exists := seenObservations[observation]; exists {
			return
		}
		if len(observationIDs) >= 100 {
			truncated = true
			return
		}
		seenObservations[observation] = struct{}{}
		observationIDs = append(observationIDs, observation)
	}
	for _, recordID := range recordIDs {
		record, exists := records[recordID]
		if !exists {
			continue
		}
		for _, observation := range record.ObservationIDs {
			addObservation(observation)
		}
		if record.PlaceGeometry != nil {
			for _, observation := range record.PlaceGeometry.ObservationIDs {
				addObservation(observation)
			}
		}
	}
	for _, connection := range connections {
		for _, observation := range connection.SupportingObservationIDs {
			addObservation(observation)
		}
		for _, observation := range connection.OpposingObservationIDs {
			addObservation(observation)
		}
	}
	for _, event := range events {
		for _, observation := range event.ObservationIDs {
			addObservation(observation)
		}
	}

	citations := make([]reviewdomain.Evidence, 0, len(observationIDs))
	for _, observation := range observationIDs {
		found, err := m.research.relations.EvidenceByIDVisible(r.Context(), workspace, observation, maxSensitivity)
		if err != nil {
			if stderrors.Is(err, reviewdomain.ErrNotFound) {
				continue
			}
			httpx.Fail(m.log, w, r, err)
			return
		}
		citations = append(citations, found)
	}

	related := make([]researchRecordResponse, 0, len(records)-1)
	for _, recordID := range recordIDs {
		if recordID == anchor.ID {
			continue
		}
		if found, exists := records[recordID]; exists {
			related = append(related, asResearchRecord(found))
		}
	}
	connectionResponses := make([]researchConnectionResponse, 0, len(connections))
	for _, connection := range connections {
		connectionResponses = append(connectionResponses, asResearchConnection(connection))
	}

	httpx.WriteJSON(w, r, http.StatusOK, researchRecordNeighborhoodResponse{
		Meta: researchRecordNeighborhoodMeta{
			Depth: depth, MaxDepth: neighborhoodMaxDepth, RecordLimit: limit, Truncated: truncated,
			RecordCount: len(related) + 1, ConnectionCount: len(connectionResponses), EventCount: len(events), CitationCount: len(citations),
		},
		Record: asResearchRecord(anchor), Records: related, Connections: connectionResponses, Events: events, Citations: citations,
	})
}
