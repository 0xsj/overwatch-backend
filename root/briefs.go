package root

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	assistdomain "github.com/0xsj/overwatch-backend/internal/assistance/domain"
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
	ClusterIDs     []id.ID `json:"cluster_ids"`
	QuestionIDs    []id.ID `json:"question_ids"`
	ConnectionIDs  []id.ID `json:"connection_ids"`
	EventIDs       []id.ID `json:"event_ids"`
}

type briefDraftRequest struct {
	ObservationIDs []id.ID `json:"observation_ids"`
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
	ClusterIDs     []string `json:"cluster_ids"`
	QuestionIDs    []string `json:"question_ids"`
	ConnectionIDs  []string `json:"connection_ids"`
	EventIDs       []string `json:"event_ids"`
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

type snapshotEventRecordResponse struct {
	RecordID       string                         `json:"record_id"`
	Kind           string                         `json:"kind"`
	Name           string                         `json:"name"`
	Description    string                         `json:"description,omitempty"`
	ObservationIDs []string                       `json:"observation_ids"`
	PlaceGeometry  *snapshotPlaceGeometryResponse `json:"place_geometry,omitempty"`
}

type snapshotPlaceGeometryResponse struct {
	Latitude       float64  `json:"latitude"`
	Longitude      float64  `json:"longitude"`
	Precision      string   `json:"precision"`
	ObservationIDs []string `json:"observation_ids"`
}

type snapshotEventResponse struct {
	EventID            string                        `json:"event_id"`
	EventRevisionID    string                        `json:"event_revision_id,omitempty"`
	EventRevision      int                           `json:"event_revision,omitempty"`
	Title              string                        `json:"title"`
	Description        string                        `json:"description,omitempty"`
	ReportedTime       string                        `json:"reported_time,omitempty"`
	TimePrecision      string                        `json:"time_precision"`
	SortDate           string                        `json:"sort_date,omitempty"`
	Location           string                        `json:"location,omitempty"`
	ObservationIDs     []string                      `json:"observation_ids"`
	ParticipantRecords []snapshotEventRecordResponse `json:"participant_records"`
	LocationRecord     *snapshotEventRecordResponse  `json:"location_record,omitempty"`
}

type snapshotEventRelationshipResponse struct {
	RelationshipID           string   `json:"relationship_id"`
	FromEventID              string   `json:"from_event_id"`
	ToEventID                string   `json:"to_event_id"`
	Kind                     string   `json:"kind"`
	Rationale                string   `json:"rationale"`
	State                    string   `json:"state"`
	ReviewNote               string   `json:"review_note,omitempty"`
	SupportingObservationIDs []string `json:"supporting_observation_ids"`
	OpposingObservationIDs   []string `json:"opposing_observation_ids"`
}

type briefSnapshotResponse struct {
	SnapshotID         string                              `json:"snapshot_id"`
	WorkspaceID        string                              `json:"workspace_id"`
	BriefID            string                              `json:"brief_id"`
	Title              string                              `json:"title"`
	Question           string                              `json:"question"`
	CurrentAccount     string                              `json:"current_account,omitempty"`
	Alternatives       string                              `json:"alternatives,omitempty"`
	Limitations        string                              `json:"limitations,omitempty"`
	NextSteps          string                              `json:"next_steps,omitempty"`
	ObservationIDs     []string                            `json:"observation_ids"`
	Clusters           []snapshotClusterResponse           `json:"clusters"`
	Questions          []snapshotQuestionResponse          `json:"questions"`
	Connections        []snapshotConnectionResponse        `json:"connections"`
	Events             []snapshotEventResponse             `json:"events"`
	EventRelationships []snapshotEventRelationshipResponse `json:"event_relationships"`
	Author             string                              `json:"author"`
	UpdatedBy          string                              `json:"updated_by"`
	FrozenBy           string                              `json:"frozen_by"`
	SourceUpdatedAt    string                              `json:"source_updated_at"`
	FrozenAt           string                              `json:"frozen_at"`
}

type recipientHandoffClusterResponse struct {
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

type recipientHandoffQuestionResponse struct {
	Question   string `json:"question"`
	State      string `json:"state"`
	Resolution string `json:"resolution,omitempty"`
}

type recipientHandoffConnectionResponse struct {
	FromName  string `json:"from_name"`
	ToName    string `json:"to_name"`
	Kind      string `json:"kind"`
	State     string `json:"state"`
	Rationale string `json:"rationale"`
}

type recipientHandoffEventResponse struct {
	Title         string `json:"title"`
	Description   string `json:"description,omitempty"`
	ReportedTime  string `json:"reported_time,omitempty"`
	TimePrecision string `json:"time_precision"`
	SortDate      string `json:"sort_date,omitempty"`
	Location      string `json:"location,omitempty"`
}

type recipientHandoffEventRelationshipResponse struct {
	FromTitle string `json:"from_title"`
	ToTitle   string `json:"to_title"`
	Kind      string `json:"kind"`
	State     string `json:"state"`
	Rationale string `json:"rationale"`
}

type briefRecipientHandoffResponse struct {
	SnapshotID         string                                      `json:"snapshot_id"`
	WorkspaceID        string                                      `json:"workspace_id"`
	Visibility         string                                      `json:"visibility"`
	Redactions         []string                                    `json:"redactions"`
	Title              string                                      `json:"title"`
	Question           string                                      `json:"question"`
	CurrentAccount     string                                      `json:"current_account,omitempty"`
	Alternatives       string                                      `json:"alternatives,omitempty"`
	Limitations        string                                      `json:"limitations,omitempty"`
	NextSteps          string                                      `json:"next_steps,omitempty"`
	Clusters           []recipientHandoffClusterResponse           `json:"clusters"`
	Questions          []recipientHandoffQuestionResponse          `json:"questions"`
	Connections        []recipientHandoffConnectionResponse        `json:"connections"`
	Events             []recipientHandoffEventResponse             `json:"events"`
	EventRelationships []recipientHandoffEventRelationshipResponse `json:"event_relationships"`
	SourceUpdatedAt    string                                      `json:"source_updated_at"`
	FrozenAt           string                                      `json:"frozen_at"`
}

type briefRecipientHandoffPageResponse struct {
	Items      []briefRecipientHandoffResponse `json:"items"`
	NextCursor *id.ID                          `json:"next_cursor"`
}

type briefHandoffShareResponse struct {
	ShareID     string `json:"share_id"`
	WorkspaceID string `json:"workspace_id"`
	SnapshotID  string `json:"snapshot_id"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
	RevokedAt   string `json:"revoked_at,omitempty"`
	RevokedBy   string `json:"revoked_by,omitempty"`
	Token       string `json:"token,omitempty"`
}

type briefHandoffExportResponse struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Content     string `json:"content"`
}

type snapshotClusterResponse struct {
	ClusterID      string   `json:"cluster_id"`
	Kind           string   `json:"kind"`
	Title          string   `json:"title"`
	Description    string   `json:"description,omitempty"`
	ObservationIDs []string `json:"observation_ids"`
}

type snapshotReviewDecisionResponse struct {
	DecisionID string `json:"decision_id"`
	ReviewerID string `json:"reviewer_id"`
	State      string `json:"state"`
	Note       string `json:"note,omitempty"`
	CreatedAt  string `json:"created_at"`
}

type briefSnapshotReviewResponse struct {
	WorkspaceID string                           `json:"workspace_id"`
	SnapshotID  string                           `json:"snapshot_id"`
	State       string                           `json:"state"`
	AssigneeID  string                           `json:"assignee_id,omitempty"`
	AssignedBy  string                           `json:"assigned_by,omitempty"`
	AssignedAt  string                           `json:"assigned_at,omitempty"`
	UpdatedAt   string                           `json:"updated_at"`
	Decisions   []snapshotReviewDecisionResponse `json:"decisions"`
}

type snapshotReviewAssignmentRequest struct {
	AssigneeID *id.ID `json:"assignee_id"`
}

type snapshotReviewDecisionRequest struct {
	State string `json:"state"`
	Note  string `json:"note"`
}

type snapshotCommentRequest struct {
	Body string `json:"body"`
}

type snapshotCommentResponse struct {
	CommentID   string `json:"comment_id"`
	WorkspaceID string `json:"workspace_id"`
	SnapshotID  string `json:"snapshot_id"`
	Author      string `json:"author"`
	Body        string `json:"body"`
	CreatedAt   string `json:"created_at"`
}

func asBriefSnapshotReview(review briefdomain.SnapshotReview) briefSnapshotReviewResponse {
	decisions := make([]snapshotReviewDecisionResponse, 0, len(review.Decisions))
	for _, one := range review.Decisions {
		decisions = append(decisions, snapshotReviewDecisionResponse{DecisionID: one.ID.String(), ReviewerID: one.ReviewerID.String(), State: string(one.State), Note: one.Note, CreatedAt: one.CreatedAt.UTC().Format(time.RFC3339Nano)})
	}
	out := briefSnapshotReviewResponse{WorkspaceID: review.WorkspaceID.String(), SnapshotID: review.SnapshotID.String(), State: string(review.State), UpdatedAt: review.UpdatedAt.UTC().Format(time.RFC3339Nano), Decisions: decisions}
	if review.AssigneeID != nil {
		out.AssigneeID = review.AssigneeID.String()
	}
	if review.AssignedBy != nil {
		out.AssignedBy = review.AssignedBy.String()
	}
	if review.AssignedAt != nil {
		out.AssignedAt = review.AssignedAt.UTC().Format(time.RFC3339Nano)
	}
	return out
}

func asSnapshotComment(comment briefdomain.SnapshotComment) snapshotCommentResponse {
	return snapshotCommentResponse{CommentID: comment.ID.String(), WorkspaceID: comment.WorkspaceID.String(), SnapshotID: comment.SnapshotID.String(), Author: comment.AuthorID.String(), Body: comment.Body, CreatedAt: comment.CreatedAt.UTC().Format(time.RFC3339Nano)}
}

func asWorkingBrief(b briefdomain.Brief) workingBriefResponse {
	observations := make([]string, 0, len(b.ObservationIDs))
	for _, one := range b.ObservationIDs {
		observations = append(observations, one.String())
	}
	clusters := make([]string, 0, len(b.ClusterIDs))
	for _, one := range b.ClusterIDs {
		clusters = append(clusters, one.String())
	}
	questions := make([]string, 0, len(b.QuestionIDs))
	for _, one := range b.QuestionIDs {
		questions = append(questions, one.String())
	}
	connections := make([]string, 0, len(b.ConnectionIDs))
	for _, one := range b.ConnectionIDs {
		connections = append(connections, one.String())
	}
	events := make([]string, 0, len(b.EventIDs))
	for _, one := range b.EventIDs {
		events = append(events, one.String())
	}
	return workingBriefResponse{
		BriefID: b.ID.String(), WorkspaceID: b.WorkspaceID.String(), Title: b.Title, Question: b.Question,
		CurrentAccount: b.CurrentAccount, Alternatives: b.Alternatives, Limitations: b.Limitations, NextSteps: b.NextSteps,
		ObservationIDs: observations, ClusterIDs: clusters, QuestionIDs: questions, ConnectionIDs: connections, EventIDs: events, Author: b.Author.String(), UpdatedBy: b.UpdatedBy.String(),
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
	clusters := make([]snapshotClusterResponse, 0, len(s.Clusters))
	for _, one := range s.Clusters {
		observations := make([]string, 0, len(one.ObservationIDs))
		for _, observation := range one.ObservationIDs {
			observations = append(observations, observation.String())
		}
		clusters = append(clusters, snapshotClusterResponse{ClusterID: one.ID.String(), Kind: one.Kind, Title: one.Title, Description: one.Description, ObservationIDs: observations})
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
	events := make([]snapshotEventResponse, 0, len(s.Events))
	for _, one := range s.Events {
		observations := make([]string, 0, len(one.ObservationIDs))
		for _, observation := range one.ObservationIDs {
			observations = append(observations, observation.String())
		}
		participants := make([]snapshotEventRecordResponse, 0, len(one.ParticipantRecords))
		for _, participant := range one.ParticipantRecords {
			participantObservations := make([]string, 0, len(participant.ObservationIDs))
			for _, observation := range participant.ObservationIDs {
				participantObservations = append(participantObservations, observation.String())
			}
			var geometry *snapshotPlaceGeometryResponse
			if participant.PlaceGeometry != nil {
				geometryObservations := make([]string, 0, len(participant.PlaceGeometry.ObservationIDs))
				for _, observation := range participant.PlaceGeometry.ObservationIDs {
					geometryObservations = append(geometryObservations, observation.String())
				}
				geometry = &snapshotPlaceGeometryResponse{Latitude: participant.PlaceGeometry.Latitude, Longitude: participant.PlaceGeometry.Longitude, Precision: participant.PlaceGeometry.Precision, ObservationIDs: geometryObservations}
			}
			participants = append(participants, snapshotEventRecordResponse{RecordID: participant.ID.String(), Kind: participant.Kind, Name: participant.Name, Description: participant.Description, ObservationIDs: participantObservations, PlaceGeometry: geometry})
		}
		var location *snapshotEventRecordResponse
		if one.LocationRecord != nil {
			observations := make([]string, 0, len(one.LocationRecord.ObservationIDs))
			for _, observation := range one.LocationRecord.ObservationIDs {
				observations = append(observations, observation.String())
			}
			var geometry *snapshotPlaceGeometryResponse
			if one.LocationRecord.PlaceGeometry != nil {
				geometryObservations := make([]string, 0, len(one.LocationRecord.PlaceGeometry.ObservationIDs))
				for _, observation := range one.LocationRecord.PlaceGeometry.ObservationIDs {
					geometryObservations = append(geometryObservations, observation.String())
				}
				geometry = &snapshotPlaceGeometryResponse{Latitude: one.LocationRecord.PlaceGeometry.Latitude, Longitude: one.LocationRecord.PlaceGeometry.Longitude, Precision: one.LocationRecord.PlaceGeometry.Precision, ObservationIDs: geometryObservations}
			}
			location = &snapshotEventRecordResponse{RecordID: one.LocationRecord.ID.String(), Kind: one.LocationRecord.Kind, Name: one.LocationRecord.Name, Description: one.LocationRecord.Description, ObservationIDs: observations, PlaceGeometry: geometry}
		}
		eventRevisionID := ""
		if !one.RevisionID.IsZero() {
			eventRevisionID = one.RevisionID.String()
		}
		events = append(events, snapshotEventResponse{EventID: one.ID.String(), EventRevisionID: eventRevisionID, EventRevision: one.Revision, Title: one.Title, Description: one.Description, ReportedTime: one.ReportedTime, TimePrecision: one.TimePrecision, SortDate: one.SortDate, Location: one.Location, ObservationIDs: observations, ParticipantRecords: participants, LocationRecord: location})
	}
	eventRelationships := make([]snapshotEventRelationshipResponse, 0, len(s.EventRelationships))
	for _, one := range s.EventRelationships {
		supporting := make([]string, 0, len(one.SupportingObservationIDs))
		for _, observation := range one.SupportingObservationIDs {
			supporting = append(supporting, observation.String())
		}
		opposing := make([]string, 0, len(one.OpposingObservationIDs))
		for _, observation := range one.OpposingObservationIDs {
			opposing = append(opposing, observation.String())
		}
		eventRelationships = append(eventRelationships, snapshotEventRelationshipResponse{RelationshipID: one.ID.String(), FromEventID: one.FromEventID.String(), ToEventID: one.ToEventID.String(), Kind: one.Kind, Rationale: one.Rationale, State: one.State, ReviewNote: one.ReviewNote, SupportingObservationIDs: supporting, OpposingObservationIDs: opposing})
	}
	return briefSnapshotResponse{
		SnapshotID: s.ID.String(), WorkspaceID: s.WorkspaceID.String(), BriefID: s.BriefID.String(), Title: s.Title, Question: s.Question,
		CurrentAccount: s.CurrentAccount, Alternatives: s.Alternatives, Limitations: s.Limitations, NextSteps: s.NextSteps,
		ObservationIDs: observations, Clusters: clusters, Questions: questions, Connections: connections, Events: events, EventRelationships: eventRelationships, Author: s.Author.String(), UpdatedBy: s.UpdatedBy.String(), FrozenBy: s.FrozenBy.String(),
		SourceUpdatedAt: s.SourceUpdatedAt.UTC().Format(time.RFC3339Nano), FrozenAt: s.FrozenAt.UTC().Format(time.RFC3339Nano),
	}
}

func asBriefRecipientHandoff(s briefdomain.Snapshot) briefRecipientHandoffResponse {
	clusters := make([]recipientHandoffClusterResponse, 0, len(s.Clusters))
	for _, one := range s.Clusters {
		clusters = append(clusters, recipientHandoffClusterResponse{Kind: one.Kind, Title: one.Title, Description: one.Description})
	}
	questions := make([]recipientHandoffQuestionResponse, 0, len(s.Questions))
	for _, one := range s.Questions {
		questions = append(questions, recipientHandoffQuestionResponse{Question: one.Prompt, State: one.State, Resolution: one.Resolution})
	}
	connections := make([]recipientHandoffConnectionResponse, 0, len(s.Connections))
	for _, one := range s.Connections {
		connections = append(connections, recipientHandoffConnectionResponse{FromName: one.FromRecordName, ToName: one.ToRecordName, Kind: one.Kind, State: one.State, Rationale: one.Rationale})
	}
	events := make([]recipientHandoffEventResponse, 0, len(s.Events))
	for _, one := range s.Events {
		events = append(events, recipientHandoffEventResponse{Title: one.Title, Description: one.Description, ReportedTime: one.ReportedTime, TimePrecision: one.TimePrecision, SortDate: one.SortDate, Location: one.Location})
	}
	eventRelationships := make([]recipientHandoffEventRelationshipResponse, 0, len(s.EventRelationships))
	eventTitles := make(map[id.ID]string, len(s.Events))
	for _, event := range s.Events {
		eventTitles[event.ID] = event.Title
	}
	for _, one := range s.EventRelationships {
		eventRelationships = append(eventRelationships, recipientHandoffEventRelationshipResponse{FromTitle: eventTitles[one.FromEventID], ToTitle: eventTitles[one.ToEventID], Kind: one.Kind, State: one.State, Rationale: one.Rationale})
	}
	return briefRecipientHandoffResponse{
		SnapshotID: s.ID.String(), WorkspaceID: s.WorkspaceID.String(), Visibility: "recipient",
		Redactions: []string{"citations", "source_and_capture_details", "internal_identifiers", "review_comments"},
		Title:      s.Title, Question: s.Question, CurrentAccount: s.CurrentAccount, Alternatives: s.Alternatives, Limitations: s.Limitations, NextSteps: s.NextSteps,
		Clusters: clusters, Questions: questions, Connections: connections, Events: events, EventRelationships: eventRelationships,
		SourceUpdatedAt: s.SourceUpdatedAt.UTC().Format(time.RFC3339Nano), FrozenAt: s.FrozenAt.UTC().Format(time.RFC3339Nano),
	}
}

func asBriefHandoffShare(share briefdomain.HandoffShare, token string) briefHandoffShareResponse {
	out := briefHandoffShareResponse{ShareID: share.ID.String(), WorkspaceID: share.WorkspaceID.String(), SnapshotID: share.SnapshotID.String(), CreatedBy: share.CreatedBy.String(), CreatedAt: share.CreatedAt.UTC().Format(time.RFC3339Nano), Token: token}
	if share.RevokedAt != nil {
		out.RevokedAt = share.RevokedAt.UTC().Format(time.RFC3339Nano)
	}
	if share.RevokedBy != nil {
		out.RevokedBy = share.RevokedBy.String()
	}
	return out
}

func recipientHandoffMarkdown(snapshot briefdomain.Snapshot) string {
	var out strings.Builder
	writeSection := func(title, value string) {
		out.WriteString("## ")
		out.WriteString(title)
		out.WriteString("\n\n")
		if strings.TrimSpace(value) == "" {
			out.WriteString("Not recorded.\n\n")
			return
		}
		out.WriteString(value)
		out.WriteString("\n\n")
	}
	out.WriteString("# ")
	out.WriteString(snapshot.Title)
	out.WriteString("\n\n")
	out.WriteString("Recipient-safe frozen handoff\n\n")
	out.WriteString("Frozen at: ")
	out.WriteString(snapshot.FrozenAt.UTC().Format(time.RFC3339Nano))
	out.WriteString("\n")
	out.WriteString("Source brief updated: ")
	out.WriteString(snapshot.SourceUpdatedAt.UTC().Format(time.RFC3339Nano))
	out.WriteString("\n\n")
	out.WriteString("> This export preserves the authored handoff while omitting citations, source and capture details, internal identifiers, and reviewer conversation.\n\n")
	writeSection("Investigation question", snapshot.Question)
	writeSection("Current account", snapshot.CurrentAccount)
	writeSection("Alternatives", snapshot.Alternatives)
	writeSection("Limitations", snapshot.Limitations)
	writeSection("Next steps", snapshot.NextSteps)
	out.WriteString("## Evidence clusters at freeze\n\n")
	if len(snapshot.Clusters) == 0 {
		out.WriteString("No evidence clusters were included.\n\n")
	} else {
		for _, cluster := range snapshot.Clusters {
			out.WriteString("- **")
			out.WriteString(cluster.Kind)
			out.WriteString("** ")
			out.WriteString(cluster.Title)
			if strings.TrimSpace(cluster.Description) != "" {
				out.WriteString(" — ")
				out.WriteString(cluster.Description)
			}
			out.WriteString("\n")
		}
		out.WriteString("\n")
	}
	out.WriteString("## Questions at freeze\n\n")
	if len(snapshot.Questions) == 0 {
		out.WriteString("No questions were included.\n\n")
	} else {
		for _, question := range snapshot.Questions {
			out.WriteString("- **")
			out.WriteString(question.State)
			out.WriteString("** ")
			out.WriteString(question.Prompt)
			if strings.TrimSpace(question.Resolution) != "" {
				out.WriteString(" — ")
				out.WriteString(question.Resolution)
			}
			out.WriteString("\n")
		}
		out.WriteString("\n")
	}
	out.WriteString("## Connections at freeze\n\n")
	if len(snapshot.Connections) == 0 {
		out.WriteString("No connections were included.\n\n")
	} else {
		for _, connection := range snapshot.Connections {
			out.WriteString("- **")
			out.WriteString(connection.State)
			out.WriteString("** ")
			out.WriteString(connection.FromRecordName)
			out.WriteString(" → ")
			out.WriteString(connection.ToRecordName)
			out.WriteString(" (")
			out.WriteString(connection.Kind)
			out.WriteString(") — ")
			out.WriteString(connection.Rationale)
			out.WriteString("\n")
		}
		out.WriteString("\n")
	}
	out.WriteString("## Events at freeze\n\n")
	if len(snapshot.Events) == 0 {
		out.WriteString("No events were included.\n\n")
	} else {
		for _, event := range snapshot.Events {
			out.WriteString("- **")
			out.WriteString(event.Title)
			out.WriteString("** (")
			out.WriteString(event.TimePrecision)
			out.WriteString(")")
			if strings.TrimSpace(event.ReportedTime) != "" {
				out.WriteString(" — ")
				out.WriteString(event.ReportedTime)
			}
			if strings.TrimSpace(event.Description) != "" {
				out.WriteString("\n  ")
				out.WriteString(event.Description)
			}
			out.WriteString("\n")
		}
		out.WriteString("\n")
	}
	out.WriteString("## Event relationships at freeze\n\n")
	if len(snapshot.EventRelationships) == 0 {
		out.WriteString("No event relationships were included.\n\n")
	} else {
		titles := make(map[id.ID]string, len(snapshot.Events))
		for _, event := range snapshot.Events {
			titles[event.ID] = event.Title
		}
		for _, relationship := range snapshot.EventRelationships {
			out.WriteString("- **")
			out.WriteString(relationship.State)
			out.WriteString("** ")
			out.WriteString(titles[relationship.FromEventID])
			out.WriteString(" → ")
			out.WriteString(titles[relationship.ToEventID])
			out.WriteString(" (")
			out.WriteString(relationship.Kind)
			out.WriteString(") — ")
			out.WriteString(relationship.Rationale)
			out.WriteString("\n")
		}
		out.WriteString("\n")
	}
	return out.String()
}

func asBriefHandoffExport(snapshot briefdomain.Snapshot) briefHandoffExportResponse {
	return briefHandoffExportResponse{Filename: "recipient-handoff.md", ContentType: "text/markdown", Content: recipientHandoffMarkdown(snapshot)}
}

func handoffShareDigest(token string) (string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 16 {
		return "", false
	}
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:]), true
}

func (m *me) readWorkingBrief(w http.ResponseWriter, r *http.Request) {
	workspace, maxSensitivity, ok := m.onWorkspaceResearchRead(w, r)
	if !ok {
		return
	}
	found, err := m.research.brief.ByWorkspaceVisible(r.Context(), workspace, maxSensitivity)
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
	saved, err := m.research.briefCmd.SaveWithEventsAndClusters(r.Context(), workspace, caller, in.Title, in.Question, in.CurrentAccount, in.Alternatives, in.Limitations, in.NextSteps, in.ObservationIDs, in.ClusterIDs, in.QuestionIDs, in.ConnectionIDs, in.EventIDs)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asWorkingBrief(saved))
}

func (m *me) listBriefDrafts(w http.ResponseWriter, r *http.Request) {
	workspace, maxSensitivity, ok := m.onWorkspaceResearchRead(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.briefDrafts.ListVisible(r.Context(), workspace, before, size, maxSensitivity)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) readBriefDraft(w http.ResponseWriter, r *http.Request) {
	workspace, maxSensitivity, ok := m.onWorkspaceResearchRead(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(strings.TrimSpace(r.PathValue("draft")))
	if err != nil || want.IsZero() {
		httpx.Fail(m.log, w, r, assistdomain.ErrNotFound)
		return
	}
	found, err := m.research.briefDrafts.ByIDVisible(r.Context(), workspace, want, maxSensitivity)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) createBriefDraft(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in briefDraftRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	fresh, err := m.research.briefDraftCmd.Generate(r.Context(), workspace, in.ObservationIDs, caller)
	if err != nil {
		if !fresh.ID.IsZero() {
			httpx.WriteJSON(w, r, http.StatusCreated, fresh)
			return
		}
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, fresh)
}

func (m *me) listBriefSnapshots(w http.ResponseWriter, r *http.Request) {
	workspace, maxSensitivity, ok := m.onWorkspaceResearchRead(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.brief.SnapshotsVisible(r.Context(), workspace, before, size, maxSensitivity)
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
	workspace, maxSensitivity, ok := m.onWorkspaceResearchRead(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("snapshot"))
	if err != nil {
		httpx.Fail(m.log, w, r, briefdomain.ErrNotFound)
		return
	}
	found, err := m.research.brief.SnapshotByIDVisible(r.Context(), workspace, want, maxSensitivity)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asBriefSnapshot(found))
}

// briefSnapshotActivity is the handoff-specific audit view. It is deliberately
// separate from the full engagement ledger so an operator can answer who
// reviewed, commented on, shared, exported, or opened this frozen handoff
// without reconstructing the snapshot from a broad activity stream.
func (m *me) briefSnapshotActivity(w http.ResponseWriter, r *http.Request) {
	workspace, maxSensitivity, ok := m.onWorkspaceResearchRead(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("snapshot"))
	if err != nil {
		httpx.Fail(m.log, w, r, briefdomain.ErrNotFound)
		return
	}
	if _, err := m.research.brief.SnapshotByIDVisible(r.Context(), workspace, want, maxSensitivity); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	after, size, facet := pageParams(r)
	page, err := m.ledger.ForWorkspaceDetail(r.Context(), workspace, "snapshot_id", want.String(), facet, after, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, renderPage(page))
}

func (m *me) listBriefRecipientHandoffs(w http.ResponseWriter, r *http.Request) {
	workspace, maxSensitivity, ok := m.onDeliverableResearchRead(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.brief.SnapshotsVisible(r.Context(), workspace, before, size, maxSensitivity)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	items := make([]briefRecipientHandoffResponse, 0, len(found.Items))
	for _, one := range found.Items {
		items = append(items, asBriefRecipientHandoff(one))
	}
	httpx.WriteJSON(w, r, http.StatusOK, briefRecipientHandoffPageResponse{Items: items, NextCursor: found.NextCursor})
}

func (m *me) readBriefRecipientHandoff(w http.ResponseWriter, r *http.Request) {
	caller, workspace, org, ok := m.onDeliverable(w, r, orgdomain.LevelRead)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("snapshot"))
	if err != nil {
		httpx.Fail(m.log, w, r, briefdomain.ErrNotFound)
		return
	}
	maxSensitivity, err := m.sourceSensitivityScope(r.Context(), caller, workspace, org)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	found, err := m.research.brief.SnapshotByIDVisible(r.Context(), workspace, want, maxSensitivity)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	if err := m.research.briefCmd.RecordSnapshotHandoffAccess(r.Context(), workspace, found.ID, caller, "direct"); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asBriefRecipientHandoff(found))
}

func (m *me) readBriefRecipientHandoffExport(w http.ResponseWriter, r *http.Request) {
	caller, workspace, org, ok := m.onDeliverable(w, r, orgdomain.LevelRead)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("snapshot"))
	if err != nil {
		httpx.Fail(m.log, w, r, briefdomain.ErrNotFound)
		return
	}
	maxSensitivity, err := m.sourceSensitivityScope(r.Context(), caller, workspace, org)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	found, err := m.research.brief.SnapshotByIDVisible(r.Context(), workspace, want, maxSensitivity)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	if err := m.research.briefCmd.RecordSnapshotHandoffExport(r.Context(), workspace, found.ID, caller, "direct"); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asBriefHandoffExport(found))
}

func (m *me) readBriefSharedHandoff(w http.ResponseWriter, r *http.Request) {
	caller, workspace, org, ok := m.onDeliverable(w, r, orgdomain.LevelRead)
	if !ok {
		return
	}
	digest, valid := handoffShareDigest(r.PathValue("token"))
	if !valid {
		httpx.Fail(m.log, w, r, briefdomain.ErrNotFound)
		return
	}
	maxSensitivity, err := m.sourceSensitivityScope(r.Context(), caller, workspace, org)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	found, err := m.research.brief.SnapshotByHandoffShareVisible(r.Context(), workspace, digest, maxSensitivity)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	if err := m.research.briefCmd.RecordSnapshotHandoffAccess(r.Context(), workspace, found.ID, caller, "shared"); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asBriefRecipientHandoff(found))
}

func (m *me) readBriefSharedHandoffExport(w http.ResponseWriter, r *http.Request) {
	caller, workspace, org, ok := m.onDeliverable(w, r, orgdomain.LevelRead)
	if !ok {
		return
	}
	digest, valid := handoffShareDigest(r.PathValue("token"))
	if !valid {
		httpx.Fail(m.log, w, r, briefdomain.ErrNotFound)
		return
	}
	maxSensitivity, err := m.sourceSensitivityScope(r.Context(), caller, workspace, org)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	found, err := m.research.brief.SnapshotByHandoffShareVisible(r.Context(), workspace, digest, maxSensitivity)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	if err := m.research.briefCmd.RecordSnapshotHandoffExport(r.Context(), workspace, found.ID, caller, "shared"); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asBriefHandoffExport(found))
}

func (m *me) listBriefSnapshotShares(w http.ResponseWriter, r *http.Request) {
	workspace, maxSensitivity, ok := m.onWorkspaceResearchRead(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("snapshot"))
	if err != nil {
		httpx.Fail(m.log, w, r, briefdomain.ErrNotFound)
		return
	}
	if _, err := m.research.brief.SnapshotByIDVisible(r.Context(), workspace, want, maxSensitivity); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	found, err := m.research.brief.SnapshotHandoffShares(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := make([]briefHandoffShareResponse, 0, len(found))
	for _, share := range found {
		out = append(out, asBriefHandoffShare(share, ""))
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

func (m *me) createBriefSnapshotShare(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("snapshot"))
	if err != nil {
		httpx.Fail(m.log, w, r, briefdomain.ErrNotFound)
		return
	}
	share, token, err := m.research.briefCmd.CreateSnapshotShare(r.Context(), workspace, want, caller)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, asBriefHandoffShare(share, token))
}

func (m *me) revokeBriefSnapshotShare(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("share"))
	if err != nil {
		httpx.Fail(m.log, w, r, briefdomain.ErrNotFound)
		return
	}
	share, err := m.research.briefCmd.RevokeSnapshotShare(r.Context(), workspace, want, caller)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asBriefHandoffShare(share, ""))
}

func (m *me) listBriefSnapshotComments(w http.ResponseWriter, r *http.Request) {
	workspace, maxSensitivity, ok := m.onWorkspaceResearchRead(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("snapshot"))
	if err != nil {
		httpx.Fail(m.log, w, r, briefdomain.ErrNotFound)
		return
	}
	if _, err := m.research.brief.SnapshotByIDVisible(r.Context(), workspace, want, maxSensitivity); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	comments, err := m.research.brief.SnapshotComments(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := make([]snapshotCommentResponse, 0, len(comments))
	for _, comment := range comments {
		out = append(out, asSnapshotComment(comment))
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

func (m *me) addBriefSnapshotComment(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("snapshot"))
	if err != nil {
		httpx.Fail(m.log, w, r, briefdomain.ErrNotFound)
		return
	}
	var in snapshotCommentRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	comment, err := m.research.briefCmd.AddSnapshotComment(r.Context(), workspace, want, caller, in.Body)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, asSnapshotComment(comment))
}

func (m *me) readBriefSnapshotReview(w http.ResponseWriter, r *http.Request) {
	workspace, maxSensitivity, ok := m.onWorkspaceResearchRead(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("snapshot"))
	if err != nil {
		httpx.Fail(m.log, w, r, briefdomain.ErrNotFound)
		return
	}
	if _, err := m.research.brief.SnapshotByIDVisible(r.Context(), workspace, want, maxSensitivity); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	found, err := m.research.brief.SnapshotReview(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asBriefSnapshotReview(found))
}

func (m *me) assignBriefSnapshotReviewer(w http.ResponseWriter, r *http.Request) {
	caller, workspace, org, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("snapshot"))
	if err != nil {
		httpx.Fail(m.log, w, r, briefdomain.ErrNotFound)
		return
	}
	var in snapshotReviewAssignmentRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	if in.AssigneeID != nil {
		seats, err := m.access.OnWorkspace(r.Context(), org, workspace)
		if err != nil {
			httpx.Fail(m.log, w, r, err)
			return
		}
		found := false
		for _, seat := range seats {
			if seat.AccountID == *in.AssigneeID && seat.Role != orgdomain.RoleClient && seat.Level.AtLeast(orgdomain.LevelWrite) {
				found = true
				break
			}
		}
		if !found {
			httpx.Fail(m.log, w, r, briefdomain.ErrReviewerNotInWorkspace)
			return
		}
	}
	review, err := m.research.briefCmd.AssignSnapshotReviewer(r.Context(), workspace, want, caller, in.AssigneeID)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asBriefSnapshotReview(review))
}

func (m *me) decideBriefSnapshotReview(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("snapshot"))
	if err != nil {
		httpx.Fail(m.log, w, r, briefdomain.ErrNotFound)
		return
	}
	var in snapshotReviewDecisionRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	review, err := m.research.briefCmd.DecideSnapshotReview(r.Context(), workspace, want, caller, in.State, in.Note)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asBriefSnapshotReview(review))
}
