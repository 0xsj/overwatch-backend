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
	MaxQuestionLinks      = 8
	MaxConnectionLinks    = 8
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
	QuestionIDs    []id.ID   `json:"question_ids"`
	ConnectionIDs  []id.ID   `json:"connection_ids"`
	Author         id.ID     `json:"author"`
	UpdatedBy      id.ID     `json:"updated_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func New(want, workspace, author id.ID, title, question, currentAccount, alternatives, limitations, nextSteps string, observations, questions, connections []id.ID, at time.Time) (Brief, error) {
	if want.IsZero() || author.IsZero() {
		return Brief{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Brief{}, ErrWorkspaceRequired
	}
	if at.IsZero() {
		return Brief{}, ErrTimeRequired
	}
	cleaned, err := clean(title, question, currentAccount, alternatives, limitations, nextSteps, observations, questions, connections)
	if err != nil {
		return Brief{}, err
	}
	return Brief{
		ID: want, WorkspaceID: workspace, Title: cleaned.title, Question: cleaned.question,
		CurrentAccount: cleaned.account, Alternatives: cleaned.alternatives,
		Limitations: cleaned.limitations, NextSteps: cleaned.nextSteps,
		ObservationIDs: cleaned.observations, QuestionIDs: cleaned.questions, ConnectionIDs: cleaned.connections,
		Author: author, UpdatedBy: author, CreatedAt: at, UpdatedAt: at,
	}, nil
}

func (b Brief) Edit(by id.ID, title, question, currentAccount, alternatives, limitations, nextSteps string, observations, questions, connections []id.ID, at time.Time) (Brief, error) {
	if by.IsZero() {
		return b, ErrIDRequired
	}
	if at.IsZero() {
		return b, ErrTimeRequired
	}
	cleaned, err := clean(title, question, currentAccount, alternatives, limitations, nextSteps, observations, questions, connections)
	if err != nil {
		return b, err
	}
	next := b
	next.Title, next.Question = cleaned.title, cleaned.question
	next.CurrentAccount, next.Alternatives = cleaned.account, cleaned.alternatives
	next.Limitations, next.NextSteps = cleaned.limitations, cleaned.nextSteps
	next.ObservationIDs, next.QuestionIDs, next.ConnectionIDs = cleaned.observations, cleaned.questions, cleaned.connections
	next.UpdatedBy, next.UpdatedAt = by, at
	return next, nil
}

type cleanedBrief struct {
	title, question, account, alternatives, limitations, nextSteps string
	observations, questions, connections                           []id.ID
}

func clean(title, question, account, alternatives, limitations, nextSteps string, observations, questions, connections []id.ID) (cleanedBrief, error) {
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
	qs, err := links(questions, MaxQuestionLinks, ErrTooManyQuestions, ErrDuplicateQuestion)
	if err != nil {
		return cleanedBrief{}, err
	}
	cs, err := links(connections, MaxConnectionLinks, ErrTooManyConnections, ErrDuplicateConnection)
	if err != nil {
		return cleanedBrief{}, err
	}
	return cleanedBrief{title: title, question: question, account: account, alternatives: alternatives, limitations: limitations, nextSteps: nextSteps, observations: obs, questions: qs, connections: cs}, nil
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

// Snapshot is an immutable handoff of one working brief. Observations remain
// links to their immutable manual records; question wording is copied because
// questions are editable investigation records.
type Snapshot struct {
	ID              id.ID                `json:"snapshot_id"`
	WorkspaceID     id.ID                `json:"workspace_id"`
	BriefID         id.ID                `json:"brief_id"`
	Title           string               `json:"title"`
	Question        string               `json:"question"`
	CurrentAccount  string               `json:"current_account,omitempty"`
	Alternatives    string               `json:"alternatives,omitempty"`
	Limitations     string               `json:"limitations,omitempty"`
	NextSteps       string               `json:"next_steps,omitempty"`
	ObservationIDs  []id.ID              `json:"observation_ids"`
	Questions       []QuestionSnapshot   `json:"questions"`
	Connections     []ConnectionSnapshot `json:"connections"`
	Author          id.ID                `json:"author"`
	UpdatedBy       id.ID                `json:"updated_by"`
	FrozenBy        id.ID                `json:"frozen_by"`
	SourceUpdatedAt time.Time            `json:"source_updated_at"`
	FrozenAt        time.Time            `json:"frozen_at"`
}

func NewSnapshot(want, frozenBy id.ID, brief Brief, questions []QuestionSnapshot, connections []ConnectionSnapshot, at time.Time) (Snapshot, error) {
	if want.IsZero() || frozenBy.IsZero() || brief.ID.IsZero() {
		return Snapshot{}, ErrIDRequired
	}
	if at.IsZero() {
		return Snapshot{}, ErrTimeRequired
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
	return Snapshot{
		ID: want, WorkspaceID: brief.WorkspaceID, BriefID: brief.ID,
		Title: brief.Title, Question: brief.Question, CurrentAccount: brief.CurrentAccount,
		Alternatives: brief.Alternatives, Limitations: brief.Limitations, NextSteps: brief.NextSteps,
		ObservationIDs: append([]id.ID(nil), brief.ObservationIDs...), Questions: questionCopies, Connections: connectionCopies,
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
