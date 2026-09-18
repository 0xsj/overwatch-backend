package domain

import (
	"bytes"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	EventRecorded = "review.relation.recorded"
	MaxRationale  = 4000
)

type Kind string

const (
	Supports    Kind = "supports"
	Contradicts Kind = "contradicts"
	Repeats     Kind = "repeats"
	Unresolved  Kind = "unresolved"
)

func (k Kind) String() string { return string(k) }

func ParseKind(raw string) (Kind, error) {
	switch Kind(strings.TrimSpace(raw)) {
	case Supports, Contradicts, Repeats, Unresolved:
		return Kind(strings.TrimSpace(raw)), nil
	default:
		return "", errors.New(errors.Invalid, "relationship kind must be supports, contradicts, repeats, or unresolved")
	}
}

// Relation is the current human assessment of an unordered pair of manual
// observations. The IDs are canonicalised so A-B and B-A cannot become two
// competing rows. Updating the relation changes the current assessment while
// the decision event and audit trail retain the act that caused it.
type Relation struct {
	ID                 id.ID     `json:"relation_id"`
	WorkspaceID        id.ID     `json:"workspace_id"`
	LeftObservationID  id.ID     `json:"left_observation_id"`
	RightObservationID id.ID     `json:"right_observation_id"`
	Kind               Kind      `json:"kind"`
	Rationale          string    `json:"rationale"`
	Author             id.ID     `json:"author"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func New(want, workspace, author, a, b id.ID, kind, rationale string, at time.Time) (Relation, error) {
	if want.IsZero() || workspace.IsZero() || author.IsZero() || a.IsZero() || b.IsZero() || a == b || at.IsZero() {
		return Relation{}, ErrInvalid
	}
	k, err := ParseKind(kind)
	if err != nil {
		return Relation{}, err
	}
	rationale = strings.TrimSpace(rationale)
	if rationale == "" || len(rationale) > MaxRationale || !utf8.ValidString(rationale) || strings.ContainsRune(rationale, 0) {
		return Relation{}, errors.New(errors.Invalid, "relationship rationale requires UTF-8 text up to 4000 bytes")
	}
	if bytes.Compare(a[:], b[:]) > 0 {
		a, b = b, a
	}
	return Relation{ID: want, WorkspaceID: workspace, LeftObservationID: a, RightObservationID: b,
		Kind: k, Rationale: rationale, Author: author, CreatedAt: at, UpdatedAt: at}, nil
}

func (r Relation) Edit(by id.ID, kind, rationale string, at time.Time) (Relation, error) {
	if by.IsZero() || at.IsZero() {
		return r, ErrInvalid
	}
	k, err := ParseKind(kind)
	if err != nil {
		return r, err
	}
	rationale = strings.TrimSpace(rationale)
	if rationale == "" || len(rationale) > MaxRationale || !utf8.ValidString(rationale) || strings.ContainsRune(rationale, 0) {
		return r, errors.New(errors.Invalid, "relationship rationale requires UTF-8 text up to 4000 bytes")
	}
	next := r
	next.Kind, next.Rationale, next.Author, next.UpdatedAt = k, rationale, by, at
	return next, nil
}

// Evidence is the read projection used by the comparison screen. It carries
// the source title here so a list does not turn into one request per row.
type Evidence struct {
	ID           id.ID     `json:"observation_id"`
	WorkspaceID  id.ID     `json:"workspace_id"`
	SourceID     id.ID     `json:"source_id"`
	SourceTitle  string    `json:"source_title"`
	CaptureID    id.ID     `json:"capture_id"`
	ExtractionID *id.ID    `json:"extraction_id,omitempty"`
	Statement    string    `json:"statement"`
	Quote        string    `json:"quote"`
	QuoteStart   int       `json:"quote_start"`
	QuoteEnd     int       `json:"quote_end"`
	Locator      string    `json:"locator,omitempty"`
	Author       id.ID     `json:"author"`
	RecordedAt   time.Time `json:"recorded_at"`
}
