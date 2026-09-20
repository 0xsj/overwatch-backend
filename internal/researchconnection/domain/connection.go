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
	MaxRationale       = 4000
	MaxEvidencePerSide = 12
)

type Kind string

const (
	AssociatedWith      Kind = "associated_with"
	MayBelongTo         Kind = "may_belong_to"
	Mentions            Kind = "mentions"
	ConcernsSameEvent   Kind = "concerns_same_event"
	LocatedAt           Kind = "located_at"
	PossibleSameSubject Kind = "possible_same_subject"
)

func (k Kind) String() string { return string(k) }

func ParseKind(raw string) (Kind, error) {
	switch Kind(strings.TrimSpace(raw)) {
	case AssociatedWith, MayBelongTo, Mentions, ConcernsSameEvent, LocatedAt, PossibleSameSubject:
		return Kind(strings.TrimSpace(raw)), nil
	default:
		return "", ErrKindUnknown
	}
}

type State string

const (
	Proposed State = "proposed"
	Accepted State = "accepted"
	Rejected State = "rejected"
	Deferred State = "deferred"
)

func (s State) String() string { return string(s) }

func ParseState(raw string) (State, error) {
	switch State(strings.TrimSpace(raw)) {
	case Proposed, Accepted, Rejected, Deferred:
		return State(strings.TrimSpace(raw)), nil
	default:
		return "", ErrStateUnknown
	}
}

type ReviewFilter string

const (
	ReviewAny        ReviewFilter = ""
	ReviewOpen       ReviewFilter = "open"
	ReviewConflicted ReviewFilter = "conflicted"
	ReviewUncited    ReviewFilter = "uncited"
)

func (f ReviewFilter) String() string { return string(f) }

func ParseReviewFilter(raw string) (ReviewFilter, error) {
	switch ReviewFilter(strings.TrimSpace(raw)) {
	case ReviewAny, ReviewOpen, ReviewConflicted, ReviewUncited:
		return ReviewFilter(strings.TrimSpace(raw)), nil
	default:
		return "", ErrReviewFilterUnknown
	}
}

type BrowseSummary struct {
	ConnectionCount int
	StateCounts     map[State]int
	OpenCount       int
	ConflictedCount int
	UncitedCount    int
}

type ReviewFlags struct {
	Open       bool
	Conflicted bool
	Uncited    bool
}

// Connection is a current human assessment between two research records.
// State describes the investigation assessment, not verified real-world truth.
type Connection struct {
	ID                       id.ID
	WorkspaceID              id.ID
	FromRecordID             id.ID
	ToRecordID               id.ID
	Kind                     Kind
	State                    State
	Rationale                string
	SupportingObservationIDs []id.ID
	OpposingObservationIDs   []id.ID
	Author                   id.ID
	UpdatedBy                id.ID
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

func (c Connection) ReviewFlags() ReviewFlags {
	return ReviewFlags{
		Open:       c.State == Proposed || c.State == Deferred,
		Conflicted: len(c.SupportingObservationIDs) > 0 && len(c.OpposingObservationIDs) > 0,
		Uncited:    len(c.SupportingObservationIDs) == 0 && len(c.OpposingObservationIDs) == 0,
	}
}

// Revision is an append-only copy of a connection assessment. It records what
// the investigator saw at one change, including both sides of the evidence,
// rather than asking a later reader to reconstruct history from the live row.
type Revision struct {
	ID                       id.ID
	WorkspaceID              id.ID
	ConnectionID             id.ID
	Revision                 int
	FromRecordID             id.ID
	FromRecordKind           string
	FromRecordName           string
	FromRecordDescription    string
	FromRecordObservationIDs []id.ID
	ToRecordID               id.ID
	ToRecordKind             string
	ToRecordName             string
	ToRecordDescription      string
	ToRecordObservationIDs   []id.ID
	Kind                     Kind
	State                    State
	Rationale                string
	SupportingObservationIDs []id.ID
	OpposingObservationIDs   []id.ID
	ChangedBy                id.ID
	ChangedAt                time.Time
}

func New(want, workspace, author, from, to id.ID, kind, state, rationale string, supporting, opposing []id.ID, at time.Time) (Connection, error) {
	if want.IsZero() || workspace.IsZero() || author.IsZero() || from.IsZero() || to.IsZero() || from == to || at.IsZero() {
		return Connection{}, ErrInvalid
	}
	parsedKind, err := ParseKind(kind)
	if err != nil {
		return Connection{}, err
	}
	parsedState, err := ParseState(state)
	if err != nil {
		return Connection{}, err
	}
	rationale, err = cleanRationale(rationale)
	if err != nil {
		return Connection{}, err
	}
	supporting, err = evidenceLinks(supporting)
	if err != nil {
		return Connection{}, err
	}
	opposing, err = evidenceLinks(opposing)
	if err != nil {
		return Connection{}, err
	}
	if overlap(supporting, opposing) {
		return Connection{}, ErrEvidenceConflict
	}
	return Connection{
		ID: want, WorkspaceID: workspace, FromRecordID: from, ToRecordID: to,
		Kind: parsedKind, State: parsedState, Rationale: rationale,
		SupportingObservationIDs: supporting, OpposingObservationIDs: opposing,
		Author: author, UpdatedBy: author, CreatedAt: at, UpdatedAt: at,
	}, nil
}

func (c Connection) Edit(by id.ID, kind, state, rationale string, supporting, opposing []id.ID, at time.Time) (Connection, error) {
	if by.IsZero() || at.IsZero() {
		return c, ErrInvalid
	}
	parsedKind, err := ParseKind(kind)
	if err != nil {
		return c, err
	}
	parsedState, err := ParseState(state)
	if err != nil {
		return c, err
	}
	rationale, err = cleanRationale(rationale)
	if err != nil {
		return c, err
	}
	supporting, err = evidenceLinks(supporting)
	if err != nil {
		return c, err
	}
	opposing, err = evidenceLinks(opposing)
	if err != nil {
		return c, err
	}
	if overlap(supporting, opposing) {
		return c, ErrEvidenceConflict
	}
	next := c
	next.Kind, next.State, next.Rationale = parsedKind, parsedState, rationale
	next.SupportingObservationIDs, next.OpposingObservationIDs = supporting, opposing
	next.UpdatedBy, next.UpdatedAt = by, at
	return next, nil
}

func cleanRationale(raw string) (string, error) {
	rationale := strings.TrimSpace(raw)
	if rationale == "" || len(rationale) > MaxRationale || !utf8.ValidString(rationale) || strings.ContainsRune(rationale, 0) {
		return "", ErrRationaleRequired
	}
	return rationale, nil
}

func evidenceLinks(input []id.ID) ([]id.ID, error) {
	if len(input) > MaxEvidencePerSide {
		return nil, ErrEvidenceTooMany
	}
	links := append([]id.ID(nil), input...)
	sort.Slice(links, func(i, j int) bool { return bytes.Compare(links[i][:], links[j][:]) < 0 })
	for i, one := range links {
		if one.IsZero() {
			return nil, ErrInvalid
		}
		if i > 0 && links[i-1] == one {
			return nil, ErrDuplicateEvidence
		}
	}
	return links, nil
}

const EventChanged = "research.connection.changed"

type Changed struct {
	WorkspaceID  string `json:"workspace_id"`
	ConnectionID string `json:"connection_id"`
	UpdatedBy    string `json:"updated_by"`
	Edit         bool   `json:"edit"`
}

func overlap(left, right []id.ID) bool {
	for _, a := range left {
		for _, b := range right {
			if a == b {
				return true
			}
		}
	}
	return false
}
