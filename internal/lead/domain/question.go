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
	MaxQuestionLength   = 400
	MaxContextLength    = 4000
	MaxResolutionLength = 4000
	MaxObservationLinks = 8
)

type ContextKind string

const (
	ContextQuestion          ContextKind = "question"
	ContextRecord            ContextKind = "record"
	ContextEvent             ContextKind = "event"
	ContextConnection        ContextKind = "connection"
	ContextEventRelationship ContextKind = "event_relationship"
	ContextBrief             ContextKind = "brief"
	ContextCluster           ContextKind = "cluster"
)

func ParseContextKind(raw string) (ContextKind, error) {
	switch ContextKind(strings.TrimSpace(raw)) {
	case ContextQuestion, ContextRecord, ContextEvent, ContextConnection, ContextEventRelationship, ContextBrief, ContextCluster:
		return ContextKind(strings.TrimSpace(raw)), nil
	default:
		return "", ErrContextKindUnknown
	}
}

type State string

const (
	Open      State = "open"
	Answered  State = "answered"
	Dismissed State = "dismissed"
	Deferred  State = "deferred"
)

func (s State) String() string { return string(s) }

func ParseState(raw string) (State, error) {
	switch State(strings.TrimSpace(raw)) {
	case Open, Answered, Dismissed, Deferred:
		return State(strings.TrimSpace(raw)), nil
	default:
		return "", ErrStateUnknown
	}
}

// Question is an investigation-owned uncertainty. Resolution is text rather
// than a claim or entity link: an answer can be provisional and must not
// silently become a fact in another domain.
type Question struct {
	ID             id.ID     `json:"question_id"`
	WorkspaceID    id.ID     `json:"workspace_id"`
	Prompt         string    `json:"question"`
	Context        string    `json:"context,omitempty"`
	ContextKind    string    `json:"context_kind,omitempty"`
	ContextID      id.ID     `json:"context_id,omitempty"`
	State          State     `json:"state"`
	Resolution     string    `json:"resolution,omitempty"`
	Author         id.ID     `json:"author"`
	UpdatedBy      id.ID     `json:"updated_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	ObservationIDs []id.ID   `json:"observation_ids"`
}

func New(want, workspace, author id.ID, prompt, context, state, resolution string,
	observations []id.ID, at time.Time) (Question, error) {
	return NewWithContext(want, workspace, author, "", id.ID{}, prompt, context, state, resolution, observations, at)
}

func NewWithContext(want, workspace, author id.ID, contextKind string, contextID id.ID,
	prompt, context, state, resolution string, observations []id.ID, at time.Time) (Question, error) {
	if want.IsZero() || author.IsZero() {
		return Question{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Question{}, ErrWorkspaceRequired
	}
	if at.IsZero() {
		return Question{}, ErrTimeRequired
	}
	parsedContext, normalizedContextID, err := questionContext(contextKind, contextID)
	if err != nil {
		return Question{}, err
	}
	stateValue, promptValue, contextValue, resolutionValue, links, err :=
		clean(prompt, context, state, resolution, observations)
	if err != nil {
		return Question{}, err
	}
	return Question{
		ID: want, WorkspaceID: workspace, Prompt: promptValue,
		Context: contextValue, State: stateValue, Resolution: resolutionValue,
		ContextKind: string(parsedContext), ContextID: normalizedContextID,
		Author: author, UpdatedBy: author, CreatedAt: at, UpdatedAt: at,
		ObservationIDs: links,
	}, nil
}

func (q Question) Edit(by id.ID, prompt, context, state, resolution string,
	observations []id.ID, at time.Time) (Question, error) {
	return q.EditWithContext(by, "", id.ID{}, prompt, context, state, resolution, observations, at)
}

func (q Question) EditWithContext(by id.ID, contextKind string, contextID id.ID,
	prompt, context, state, resolution string, observations []id.ID, at time.Time) (Question, error) {
	if by.IsZero() {
		return q, ErrIDRequired
	}
	if at.IsZero() {
		return q, ErrTimeRequired
	}
	parsedContext, normalizedContextID, err := questionContext(contextKind, contextID)
	if err != nil {
		return q, err
	}
	stateValue, promptValue, contextValue, resolutionValue, links, err :=
		clean(prompt, context, state, resolution, observations)
	if err != nil {
		return q, err
	}
	next := q
	next.Prompt, next.Context, next.State = promptValue, contextValue, stateValue
	next.ContextKind, next.ContextID = string(parsedContext), normalizedContextID
	next.Resolution, next.ObservationIDs = resolutionValue, links
	next.UpdatedBy, next.UpdatedAt = by, at
	return next, nil
}

func questionContext(kind string, value id.ID) (ContextKind, id.ID, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" && value.IsZero() {
		return "", id.ID{}, nil
	}
	if kind == "" || value.IsZero() {
		return "", id.ID{}, ErrContextHalfSet
	}
	parsed, err := ParseContextKind(kind)
	if err != nil {
		return "", id.ID{}, err
	}
	return parsed, value, nil
}

func clean(prompt, context, state, resolution string, observations []id.ID) (
	State, string, string, string, []id.ID, error) {
	prompt = strings.TrimSpace(prompt)
	context = strings.TrimSpace(context)
	resolution = strings.TrimSpace(resolution)
	if prompt == "" || !utf8.ValidString(prompt) || strings.ContainsRune(prompt, 0) {
		return "", "", "", "", nil, ErrQuestionRequired
	}
	if len(prompt) > MaxQuestionLength {
		return "", "", "", "", nil, ErrQuestionTooLong
	}
	if !utf8.ValidString(context) || strings.ContainsRune(context, 0) || len(context) > MaxContextLength {
		return "", "", "", "", nil, ErrContextTooLong
	}
	if !utf8.ValidString(resolution) || strings.ContainsRune(resolution, 0) || len(resolution) > MaxResolutionLength {
		return "", "", "", "", nil, ErrResolutionTooLong
	}
	parsed, err := ParseState(state)
	if err != nil {
		return "", "", "", "", nil, err
	}
	if parsed != Open && resolution == "" {
		return "", "", "", "", nil, ErrResolutionRequired
	}
	if parsed == Open && resolution != "" {
		return "", "", "", "", nil, ErrResolutionNotAllowed
	}
	links, err := observationLinks(observations)
	if err != nil {
		return "", "", "", "", nil, err
	}
	return parsed, prompt, context, resolution, links, nil
}

func observationLinks(input []id.ID) ([]id.ID, error) {
	if len(input) > MaxObservationLinks {
		return nil, ErrTooManyObservations
	}
	links := append([]id.ID(nil), input...)
	sort.Slice(links, func(i, j int) bool { return bytes.Compare(links[i][:], links[j][:]) < 0 })
	for i, one := range links {
		if one.IsZero() {
			return nil, ErrIDRequired
		}
		if i > 0 && links[i-1] == one {
			return nil, ErrDuplicateObservation
		}
	}
	return links, nil
}

const EventChanged = "lead.question.changed"

type Changed struct {
	WorkspaceID string `json:"workspace_id"`
	QuestionID  string `json:"question_id"`
	State       string `json:"state"`
	UpdatedBy   string `json:"updated_by"`
	Edit        bool   `json:"edit"`
}
