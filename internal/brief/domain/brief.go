package domain

import (
	"bytes"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	MaxTitleLength        = 400
	MaxQuestionLength     = 4000
	MaxAccountLength      = 8000
	MaxAlternativesLength = 8000
	MaxLimitationsLength  = 4000
	MaxNextStepsLength    = 4000
	MaxObservationLinks   = 12
	MaxClusterLinks       = 8
	MaxQuestionLinks      = 8
	MaxConnectionLinks    = 8
	MaxEventLinks         = 8
)

// Brief is an authored working handoff. The prose is intentionally separate
// from observations: a citation supports a person's account but never becomes
// a generated conclusion by being linked here.
type Brief struct {
	ID             id.ID     `json:"brief_id"`
	WorkspaceID    id.ID     `json:"workspace_id"`
	Title          string    `json:"title"`
	Question       string    `json:"question"`
	CurrentAccount string    `json:"current_account,omitempty"`
	Alternatives   string    `json:"alternatives,omitempty"`
	Limitations    string    `json:"limitations,omitempty"`
	NextSteps      string    `json:"next_steps,omitempty"`
	ObservationIDs []id.ID   `json:"observation_ids"`
	ClusterIDs     []id.ID   `json:"cluster_ids"`
	QuestionIDs    []id.ID   `json:"question_ids"`
	ConnectionIDs  []id.ID   `json:"connection_ids"`
	EventIDs       []id.ID   `json:"event_ids"`
	Author         id.ID     `json:"author"`
	UpdatedBy      id.ID     `json:"updated_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func New(want, workspace, author id.ID, title, question, currentAccount, alternatives, limitations, nextSteps string, observations, questions, connections []id.ID, at time.Time) (Brief, error) {
	return NewWithEventsAndClusters(want, workspace, author, title, question, currentAccount, alternatives, limitations, nextSteps, observations, nil, questions, connections, nil, at)
}

func NewWithEvents(want, workspace, author id.ID, title, question, currentAccount, alternatives, limitations, nextSteps string, observations, questions, connections, events []id.ID, at time.Time) (Brief, error) {
	return NewWithEventsAndClusters(want, workspace, author, title, question, currentAccount, alternatives, limitations, nextSteps, observations, nil, questions, connections, events, at)
}

func NewWithEventsAndClusters(want, workspace, author id.ID, title, question, currentAccount, alternatives, limitations, nextSteps string, observations, clusters, questions, connections, events []id.ID, at time.Time) (Brief, error) {
	if want.IsZero() || author.IsZero() {
		return Brief{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Brief{}, ErrWorkspaceRequired
	}
	if at.IsZero() {
		return Brief{}, ErrTimeRequired
	}
	cleaned, err := cleanWithClusters(title, question, currentAccount, alternatives, limitations, nextSteps, observations, clusters, questions, connections, events)
	if err != nil {
		return Brief{}, err
	}
	return Brief{
		ID: want, WorkspaceID: workspace, Title: cleaned.title, Question: cleaned.question,
		CurrentAccount: cleaned.account, Alternatives: cleaned.alternatives,
		Limitations: cleaned.limitations, NextSteps: cleaned.nextSteps,
		ObservationIDs: cleaned.observations, ClusterIDs: cleaned.clusters, QuestionIDs: cleaned.questions, ConnectionIDs: cleaned.connections, EventIDs: cleaned.events,
		Author: author, UpdatedBy: author, CreatedAt: at, UpdatedAt: at,
	}, nil
}

func (b Brief) Edit(by id.ID, title, question, currentAccount, alternatives, limitations, nextSteps string, observations, questions, connections []id.ID, at time.Time) (Brief, error) {
	return b.EditWithEventsAndClusters(by, title, question, currentAccount, alternatives, limitations, nextSteps, observations, b.ClusterIDs, questions, connections, b.EventIDs, at)
}

func (b Brief) EditWithEvents(by id.ID, title, question, currentAccount, alternatives, limitations, nextSteps string, observations, questions, connections, events []id.ID, at time.Time) (Brief, error) {
	return b.EditWithEventsAndClusters(by, title, question, currentAccount, alternatives, limitations, nextSteps, observations, b.ClusterIDs, questions, connections, events, at)
}

func (b Brief) EditWithEventsAndClusters(by id.ID, title, question, currentAccount, alternatives, limitations, nextSteps string, observations, clusters, questions, connections, events []id.ID, at time.Time) (Brief, error) {
	if by.IsZero() {
		return b, ErrIDRequired
	}
	if at.IsZero() {
		return b, ErrTimeRequired
	}
	cleaned, err := cleanWithClusters(title, question, currentAccount, alternatives, limitations, nextSteps, observations, clusters, questions, connections, events)
	if err != nil {
		return b, err
	}
	next := b
	next.Title, next.Question = cleaned.title, cleaned.question
	next.CurrentAccount, next.Alternatives = cleaned.account, cleaned.alternatives
	next.Limitations, next.NextSteps = cleaned.limitations, cleaned.nextSteps
	next.ObservationIDs, next.ClusterIDs, next.QuestionIDs, next.ConnectionIDs, next.EventIDs = cleaned.observations, cleaned.clusters, cleaned.questions, cleaned.connections, cleaned.events
	next.UpdatedBy, next.UpdatedAt = by, at
	return next, nil
}

type cleanedBrief struct {
	title, question, account, alternatives, limitations, nextSteps string
	observations, clusters, questions, connections, events         []id.ID
}

func clean(title, question, account, alternatives, limitations, nextSteps string, observations, questions, connections, events []id.ID) (cleanedBrief, error) {
	return cleanWithClusters(title, question, account, alternatives, limitations, nextSteps, observations, nil, questions, connections, events)
}

func cleanWithClusters(title, question, account, alternatives, limitations, nextSteps string, observations, clusters, questions, connections, events []id.ID) (cleanedBrief, error) {
	title, question = strings.TrimSpace(title), strings.TrimSpace(question)
	account, alternatives = strings.TrimSpace(account), strings.TrimSpace(alternatives)
	limitations, nextSteps = strings.TrimSpace(limitations), strings.TrimSpace(nextSteps)
	if !valid(title, MaxTitleLength) || title == "" {
		return cleanedBrief{}, ErrTitleRequired
	}
	if question == "" {
		return cleanedBrief{}, ErrQuestionRequired
	}
	if !valid(question, MaxQuestionLength) {
		return cleanedBrief{}, ErrQuestionTooLong
	}
	for value, limits := range map[string]struct {
		limit int
		err   error
	}{
		account:      {MaxAccountLength, ErrAccountTooLong},
		alternatives: {MaxAlternativesLength, ErrAlternativesTooLong},
		limitations:  {MaxLimitationsLength, ErrLimitationsTooLong},
		nextSteps:    {MaxNextStepsLength, ErrNextStepsTooLong},
	} {
		if !valid(value, limits.limit) {
			return cleanedBrief{}, limits.err
		}
	}
	obs, err := links(observations, MaxObservationLinks, ErrTooManyObservations, ErrDuplicateObservation)
	if err != nil {
		return cleanedBrief{}, err
	}
	clusterLinks, err := links(clusters, MaxClusterLinks, ErrTooManyClusters, ErrDuplicateCluster)
	if err != nil {
		return cleanedBrief{}, err
	}
	qs, err := links(questions, MaxQuestionLinks, ErrTooManyQuestions, ErrDuplicateQuestion)
	if err != nil {
		return cleanedBrief{}, err
	}
	conn, err := links(connections, MaxConnectionLinks, ErrTooManyConnections, ErrDuplicateConnection)
	if err != nil {
		return cleanedBrief{}, err
	}
	es, err := links(events, MaxEventLinks, ErrTooManyEvents, ErrDuplicateEvent)
	if err != nil {
		return cleanedBrief{}, err
	}
	return cleanedBrief{title: title, question: question, account: account, alternatives: alternatives, limitations: limitations, nextSteps: nextSteps, observations: obs, clusters: clusterLinks, questions: qs, connections: conn, events: es}, nil
}

func valid(value string, limit int) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0) && len(value) <= limit
}

func links(input []id.ID, limit int, tooMany, duplicate error) ([]id.ID, error) {
	if len(input) > limit {
		return nil, tooMany
	}
	out := append([]id.ID(nil), input...)
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i][:], out[j][:]) < 0 })
	for i, one := range out {
		if one.IsZero() {
			return nil, ErrIDRequired
		}
		if i > 0 && out[i-1] == one {
			return nil, duplicate
		}
	}
	return out, nil
}

const EventChanged = "brief.working.changed"

type Changed struct {
	WorkspaceID string `json:"workspace_id"`
	BriefID     string `json:"brief_id"`
	UpdatedBy   string `json:"updated_by"`
	Edit        bool   `json:"edit"`
}

const EventSnapshotCreated = "brief.snapshot.created"

const EventSnapshotReviewAssigned = "brief.snapshot.review.assigned"

type SnapshotReviewAssigned struct {
	WorkspaceID string `json:"workspace_id"`
	SnapshotID  string `json:"snapshot_id"`
	AssignedBy  string `json:"assigned_by"`
	AssigneeID  string `json:"assignee_id,omitempty"`
}

const EventSnapshotReviewDecided = "brief.snapshot.review.decided"

type SnapshotReviewDecided struct {
	WorkspaceID string `json:"workspace_id"`
	SnapshotID  string `json:"snapshot_id"`
	DecisionID  string `json:"decision_id"`
	ReviewerID  string `json:"reviewer_id"`
	State       string `json:"state"`
}

const EventSnapshotCommentCreated = "brief.snapshot.comment.created"

type SnapshotCommentCreated struct {
	WorkspaceID string `json:"workspace_id"`
	SnapshotID  string `json:"snapshot_id"`
	CommentID   string `json:"comment_id"`
	AuthorID    string `json:"author_id"`
}

const EventSnapshotShareCreated = "brief.snapshot.share.created"

type SnapshotShareCreated struct {
	WorkspaceID string `json:"workspace_id"`
	SnapshotID  string `json:"snapshot_id"`
	ShareID     string `json:"share_id"`
	CreatedBy   string `json:"created_by"`
}

const EventSnapshotShareRevoked = "brief.snapshot.share.revoked"

type SnapshotShareRevoked struct {
	WorkspaceID string `json:"workspace_id"`
	SnapshotID  string `json:"snapshot_id"`
	ShareID     string `json:"share_id"`
	RevokedBy   string `json:"revoked_by"`
}

// EventSnapshotHandoffAccessed records a successful recipient-view read. The
// payload names the access mode, never the opaque share token.
const EventSnapshotHandoffAccessed = "brief.snapshot.handoff.accessed"

type SnapshotHandoffAccessed struct {
	WorkspaceID string `json:"workspace_id"`
	SnapshotID  string `json:"snapshot_id"`
	AccessMode  string `json:"access_mode"`
}

// EventSnapshotHandoffExported records a successful export of the
// recipient-safe handoff. It is separate from access because an export is a
// durable disclosure even when the recipient never opens the rendered view.
const EventSnapshotHandoffExported = "brief.snapshot.handoff.exported"

type SnapshotHandoffExported struct {
	WorkspaceID string `json:"workspace_id"`
	SnapshotID  string `json:"snapshot_id"`
	AccessMode  string `json:"access_mode"`
}

// QuestionSnapshot preserves the question's wording and disposition at the
// time of a freeze. Linking only to a mutable question id would make an old
// handoff silently change meaning when somebody later edits the question.
type QuestionSnapshot struct {
	ID             id.ID   `json:"question_id"`
	Prompt         string  `json:"question"`
	State          string  `json:"state"`
	Resolution     string  `json:"resolution,omitempty"`
	ObservationIDs []id.ID `json:"observation_ids"`
}

// ConnectionSnapshot preserves the current assessment and endpoint context at
// freeze time. A connection and its records remain editable in the live
// investigation, so a snapshot must not silently change when they are later
// edited.
type ConnectionSnapshot struct {
	ID                       id.ID   `json:"connection_id"`
	FromRecordID             id.ID   `json:"from_record_id"`
	FromRecordKind           string  `json:"from_record_kind"`
	FromRecordName           string  `json:"from_record_name"`
	FromRecordDescription    string  `json:"from_record_description,omitempty"`
	FromRecordObservationIDs []id.ID `json:"from_record_observation_ids"`
	ToRecordID               id.ID   `json:"to_record_id"`
	ToRecordKind             string  `json:"to_record_kind"`
	ToRecordName             string  `json:"to_record_name"`
	ToRecordDescription      string  `json:"to_record_description,omitempty"`
	ToRecordObservationIDs   []id.ID `json:"to_record_observation_ids"`
	Kind                     string  `json:"kind"`
	State                    string  `json:"state"`
	Rationale                string  `json:"rationale"`
	SupportingObservationIDs []id.ID `json:"supporting_observation_ids"`
	OpposingObservationIDs   []id.ID `json:"opposing_observation_ids"`
}

// EventRecordSnapshot preserves the authored record labels used to explain an
// event at freeze time. The record itself remains a separate live object; the
// snapshot must not silently change when that record is later edited.
type EventRecordSnapshot struct {
	ID             id.ID                       `json:"record_id"`
	Kind           string                      `json:"kind"`
	Name           string                      `json:"name"`
	Description    string                      `json:"description,omitempty"`
	ObservationIDs []id.ID                     `json:"observation_ids"`
	PlaceGeometry  *EventPlaceGeometrySnapshot `json:"place_geometry,omitempty"`
}

type EventPlaceGeometrySnapshot struct {
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	Precision      string  `json:"precision"`
	ObservationIDs []id.ID `json:"observation_ids"`
}

// EventSnapshot preserves the reported event wording and its reviewed record
// context at the moment a working brief becomes an immutable handoff.
type EventSnapshot struct {
	ID                 id.ID                 `json:"event_id"`
	RevisionID         id.ID                 `json:"event_revision_id,omitempty"`
	Revision           int                   `json:"event_revision,omitempty"`
	Title              string                `json:"title"`
	Description        string                `json:"description,omitempty"`
	ReportedTime       string                `json:"reported_time,omitempty"`
	TimePrecision      string                `json:"time_precision"`
	SortDate           string                `json:"sort_date,omitempty"`
	Location           string                `json:"location,omitempty"`
	ObservationIDs     []id.ID               `json:"observation_ids"`
	ParticipantRecords []EventRecordSnapshot `json:"participant_records"`
	LocationRecord     *EventRecordSnapshot  `json:"location_record,omitempty"`
}

// EventRelationshipSnapshot preserves the authored interpretation between two
// events included in the handoff, including its review state and evidence
// sides. It is a snapshot value, not a live graph edge.
type EventRelationshipSnapshot struct {
	ID                       id.ID   `json:"relationship_id"`
	FromEventID              id.ID   `json:"from_event_id"`
	ToEventID                id.ID   `json:"to_event_id"`
	Kind                     string  `json:"kind"`
	Rationale                string  `json:"rationale"`
	State                    string  `json:"state"`
	ReviewNote               string  `json:"review_note,omitempty"`
	SupportingObservationIDs []id.ID `json:"supporting_observation_ids"`
	OpposingObservationIDs   []id.ID `json:"opposing_observation_ids"`
}

// ClusterSnapshot preserves the authored grouping and its qualification at
// freeze time. The live cluster may be edited later; a handoff must retain
// what the analyst actually grouped when it was frozen.
type ClusterSnapshot struct {
	ID             id.ID   `json:"cluster_id"`
	Kind           string  `json:"kind"`
	Title          string  `json:"title"`
	Description    string  `json:"description,omitempty"`
	ObservationIDs []id.ID `json:"observation_ids"`
}

// Snapshot is an immutable handoff of one working brief. Observations remain
// links to their immutable manual records; question wording is copied because
// questions are editable investigation records.
type Snapshot struct {
	ID                 id.ID                       `json:"snapshot_id"`
	WorkspaceID        id.ID                       `json:"workspace_id"`
	BriefID            id.ID                       `json:"brief_id"`
	Title              string                      `json:"title"`
	Question           string                      `json:"question"`
	CurrentAccount     string                      `json:"current_account,omitempty"`
	Alternatives       string                      `json:"alternatives,omitempty"`
	Limitations        string                      `json:"limitations,omitempty"`
	NextSteps          string                      `json:"next_steps,omitempty"`
	ObservationIDs     []id.ID                     `json:"observation_ids"`
	Clusters           []ClusterSnapshot           `json:"clusters"`
	Questions          []QuestionSnapshot          `json:"questions"`
	Connections        []ConnectionSnapshot        `json:"connections"`
	Events             []EventSnapshot             `json:"events"`
	EventRelationships []EventRelationshipSnapshot `json:"event_relationships"`
	Author             id.ID                       `json:"author"`
	UpdatedBy          id.ID                       `json:"updated_by"`
	FrozenBy           id.ID                       `json:"frozen_by"`
	SourceUpdatedAt    time.Time                   `json:"source_updated_at"`
	FrozenAt           time.Time                   `json:"frozen_at"`
}

func NewSnapshot(want, frozenBy id.ID, brief Brief, questions []QuestionSnapshot, connections []ConnectionSnapshot, at time.Time) (Snapshot, error) {
	return NewSnapshotWithEventsAndClusters(want, frozenBy, brief, nil, questions, connections, nil, at)
}

func NewSnapshotWithEvents(want, frozenBy id.ID, brief Brief, questions []QuestionSnapshot, connections []ConnectionSnapshot, events []EventSnapshot, at time.Time) (Snapshot, error) {
	return NewSnapshotWithEventsAndClusters(want, frozenBy, brief, nil, questions, connections, events, at)
}

func NewSnapshotWithEventsAndClusters(want, frozenBy id.ID, brief Brief, clusters []ClusterSnapshot, questions []QuestionSnapshot, connections []ConnectionSnapshot, events []EventSnapshot, at time.Time) (Snapshot, error) {
	return NewSnapshotWithEventsAndClustersAndRelationships(want, frozenBy, brief, clusters, questions, connections, events, nil, at)
}

func NewSnapshotWithEventsAndClustersAndRelationships(want, frozenBy id.ID, brief Brief, clusters []ClusterSnapshot, questions []QuestionSnapshot, connections []ConnectionSnapshot, events []EventSnapshot, eventRelationships []EventRelationshipSnapshot, at time.Time) (Snapshot, error) {
	if want.IsZero() || frozenBy.IsZero() || brief.ID.IsZero() {
		return Snapshot{}, ErrIDRequired
	}
	if at.IsZero() {
		return Snapshot{}, ErrTimeRequired
	}
	if len(clusters) != len(brief.ClusterIDs) {
		return Snapshot{}, ErrClusterSnapshotMissing
	}
	clusterIDs := make(map[id.ID]struct{}, len(clusters))
	for _, cluster := range clusters {
		if cluster.ID.IsZero() || strings.TrimSpace(cluster.Kind) == "" || strings.TrimSpace(cluster.Title) == "" || len(cluster.ObservationIDs) == 0 {
			return Snapshot{}, ErrClusterSnapshotMissing
		}
		if _, exists := clusterIDs[cluster.ID]; exists {
			return Snapshot{}, ErrClusterSnapshotMissing
		}
		clusterIDs[cluster.ID] = struct{}{}
	}
	for _, wantCluster := range brief.ClusterIDs {
		if _, found := clusterIDs[wantCluster]; !found {
			return Snapshot{}, ErrClusterSnapshotMissing
		}
	}
	if len(questions) != len(brief.QuestionIDs) {
		return Snapshot{}, ErrQuestionSnapshotMissing
	}
	questionIDs := make([]id.ID, 0, len(questions))
	for _, question := range questions {
		if question.ID.IsZero() || question.Prompt == "" {
			return Snapshot{}, ErrQuestionSnapshotMissing
		}
		questionIDs = append(questionIDs, question.ID)
	}
	for _, wantQuestion := range brief.QuestionIDs {
		found := false
		for _, question := range questionIDs {
			if wantQuestion == question {
				found = true
				break
			}
		}
		if !found {
			return Snapshot{}, ErrQuestionSnapshotMissing
		}
	}
	if len(connections) != len(brief.ConnectionIDs) {
		return Snapshot{}, ErrConnectionSnapshotMissing
	}
	connectionIDs := make(map[id.ID]struct{}, len(connections))
	for _, connection := range connections {
		if connection.ID.IsZero() || connection.FromRecordID.IsZero() || connection.ToRecordID.IsZero() || connection.FromRecordID == connection.ToRecordID || connection.Kind == "" || connection.State == "" || strings.TrimSpace(connection.Rationale) == "" {
			return Snapshot{}, ErrConnectionSnapshotMissing
		}
		if _, exists := connectionIDs[connection.ID]; exists {
			return Snapshot{}, ErrConnectionSnapshotMissing
		}
		connectionIDs[connection.ID] = struct{}{}
	}
	for _, wantConnection := range brief.ConnectionIDs {
		if _, found := connectionIDs[wantConnection]; !found {
			return Snapshot{}, ErrConnectionSnapshotMissing
		}
	}
	if len(events) != len(brief.EventIDs) {
		return Snapshot{}, ErrEventSnapshotMissing
	}
	eventIDs := make(map[id.ID]struct{}, len(events))
	for _, event := range events {
		if event.ID.IsZero() || strings.TrimSpace(event.Title) == "" || strings.TrimSpace(event.TimePrecision) == "" {
			return Snapshot{}, ErrEventSnapshotMissing
		}
		if _, exists := eventIDs[event.ID]; exists {
			return Snapshot{}, ErrEventSnapshotMissing
		}
		eventIDs[event.ID] = struct{}{}
		seenParticipants := make(map[id.ID]struct{}, len(event.ParticipantRecords))
		for _, participant := range event.ParticipantRecords {
			if participant.ID.IsZero() || strings.TrimSpace(participant.Kind) == "" || strings.TrimSpace(participant.Name) == "" {
				return Snapshot{}, ErrEventSnapshotMissing
			}
			if _, exists := seenParticipants[participant.ID]; exists {
				return Snapshot{}, ErrEventSnapshotMissing
			}
			seenParticipants[participant.ID] = struct{}{}
		}
		if event.LocationRecord != nil && (event.LocationRecord.ID.IsZero() || strings.TrimSpace(event.LocationRecord.Kind) == "" || strings.TrimSpace(event.LocationRecord.Name) == "") {
			return Snapshot{}, ErrEventSnapshotMissing
		}
	}
	for _, wantEvent := range brief.EventIDs {
		if _, found := eventIDs[wantEvent]; !found {
			return Snapshot{}, ErrEventSnapshotMissing
		}
	}
	linkedEventIDs := make(map[id.ID]struct{}, len(eventIDs))
	for _, relationship := range eventRelationships {
		if relationship.ID.IsZero() || relationship.FromEventID.IsZero() || relationship.ToEventID.IsZero() || relationship.FromEventID == relationship.ToEventID || relationship.Kind == "" || relationship.State == "" || strings.TrimSpace(relationship.Rationale) == "" {
			return Snapshot{}, ErrEventSnapshotMissing
		}
		if _, ok := eventIDs[relationship.FromEventID]; !ok {
			return Snapshot{}, ErrEventSnapshotMissing
		}
		if _, ok := eventIDs[relationship.ToEventID]; !ok {
			return Snapshot{}, ErrEventSnapshotMissing
		}
		if _, exists := linkedEventIDs[relationship.ID]; exists {
			return Snapshot{}, ErrEventSnapshotMissing
		}
		linkedEventIDs[relationship.ID] = struct{}{}
		if len(relationship.SupportingObservationIDs) > 8 || len(relationship.OpposingObservationIDs) > 8 {
			return Snapshot{}, ErrEventSnapshotMissing
		}
		seenSupporting := make(map[id.ID]struct{}, len(relationship.SupportingObservationIDs))
		for _, observation := range relationship.SupportingObservationIDs {
			if observation.IsZero() {
				return Snapshot{}, ErrEventSnapshotMissing
			}
			if _, exists := seenSupporting[observation]; exists {
				return Snapshot{}, ErrEventSnapshotMissing
			}
			seenSupporting[observation] = struct{}{}
		}
		seenOpposing := make(map[id.ID]struct{}, len(relationship.OpposingObservationIDs))
		for _, observation := range relationship.OpposingObservationIDs {
			if observation.IsZero() {
				return Snapshot{}, ErrEventSnapshotMissing
			}
			if _, exists := seenOpposing[observation]; exists {
				return Snapshot{}, ErrEventSnapshotMissing
			}
			if _, overlaps := seenSupporting[observation]; overlaps {
				return Snapshot{}, ErrEventSnapshotMissing
			}
			seenOpposing[observation] = struct{}{}
		}
	}
	questionCopies := make([]QuestionSnapshot, 0, len(questions))
	for _, question := range questions {
		question.ObservationIDs = append([]id.ID(nil), question.ObservationIDs...)
		questionCopies = append(questionCopies, question)
	}
	connectionCopies := make([]ConnectionSnapshot, 0, len(connections))
	for _, connection := range connections {
		connection.SupportingObservationIDs = append([]id.ID(nil), connection.SupportingObservationIDs...)
		connection.OpposingObservationIDs = append([]id.ID(nil), connection.OpposingObservationIDs...)
		connection.FromRecordObservationIDs = append([]id.ID(nil), connection.FromRecordObservationIDs...)
		connection.ToRecordObservationIDs = append([]id.ID(nil), connection.ToRecordObservationIDs...)
		connectionCopies = append(connectionCopies, connection)
	}
	eventCopies := make([]EventSnapshot, 0, len(events))
	for _, event := range events {
		event.ObservationIDs = append([]id.ID(nil), event.ObservationIDs...)
		participants := make([]EventRecordSnapshot, 0, len(event.ParticipantRecords))
		for _, participant := range event.ParticipantRecords {
			participant.ObservationIDs = append([]id.ID(nil), participant.ObservationIDs...)
			participants = append(participants, participant)
		}
		event.ParticipantRecords = participants
		if event.LocationRecord != nil {
			location := *event.LocationRecord
			location.ObservationIDs = append([]id.ID(nil), location.ObservationIDs...)
			event.LocationRecord = &location
		}
		eventCopies = append(eventCopies, event)
	}
	clusterCopies := make([]ClusterSnapshot, 0, len(clusters))
	for _, cluster := range clusters {
		cluster.ObservationIDs = append([]id.ID(nil), cluster.ObservationIDs...)
		clusterCopies = append(clusterCopies, cluster)
	}
	relationshipCopies := make([]EventRelationshipSnapshot, 0, len(eventRelationships))
	for _, relationship := range eventRelationships {
		relationship.SupportingObservationIDs = append([]id.ID(nil), relationship.SupportingObservationIDs...)
		relationship.OpposingObservationIDs = append([]id.ID(nil), relationship.OpposingObservationIDs...)
		relationshipCopies = append(relationshipCopies, relationship)
	}
	return Snapshot{
		ID: want, WorkspaceID: brief.WorkspaceID, BriefID: brief.ID,
		Title: brief.Title, Question: brief.Question, CurrentAccount: brief.CurrentAccount,
		Alternatives: brief.Alternatives, Limitations: brief.Limitations, NextSteps: brief.NextSteps,
		ObservationIDs: append([]id.ID(nil), brief.ObservationIDs...), Clusters: clusterCopies, Questions: questionCopies, Connections: connectionCopies, Events: eventCopies, EventRelationships: relationshipCopies,
		Author: brief.Author, UpdatedBy: brief.UpdatedBy, FrozenBy: frozenBy,
		SourceUpdatedAt: brief.UpdatedAt, FrozenAt: at,
	}, nil
}

type SnapshotCreated struct {
	WorkspaceID string `json:"workspace_id"`
	SnapshotID  string `json:"snapshot_id"`
	BriefID     string `json:"brief_id"`
	FrozenBy    string `json:"frozen_by"`
}
