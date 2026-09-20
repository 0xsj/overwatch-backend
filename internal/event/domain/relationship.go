package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const MaxEventRelationshipDetail = 4000

type RelationshipKind string

const (
	RelationshipRelated        RelationshipKind = "related"
	RelationshipPrecedes       RelationshipKind = "precedes"
	RelationshipOverlaps       RelationshipKind = "overlaps"
	RelationshipSameOccurrence RelationshipKind = "same_occurrence_candidate"
	RelationshipPossiblyCauses RelationshipKind = "possibly_causes"
)

type RelationshipState string

const (
	RelationshipProposed RelationshipState = "proposed"
	RelationshipAccepted RelationshipState = "accepted"
	RelationshipRejected RelationshipState = "rejected"
)

type Relationship struct {
	ID                       id.ID             `json:"relationship_id"`
	WorkspaceID              id.ID             `json:"workspace_id"`
	FromEventID              id.ID             `json:"from_event_id"`
	ToEventID                id.ID             `json:"to_event_id"`
	Kind                     RelationshipKind  `json:"kind"`
	Rationale                string            `json:"rationale"`
	State                    RelationshipState `json:"state"`
	ReviewNote               string            `json:"review_note,omitempty"`
	SupportingObservationIDs []id.ID           `json:"supporting_observation_ids"`
	OpposingObservationIDs   []id.ID           `json:"opposing_observation_ids"`
	Author                   id.ID             `json:"author"`
	UpdatedBy                id.ID             `json:"updated_by"`
	ReviewedBy               *id.ID            `json:"reviewed_by,omitempty"`
	ReviewedAt               *time.Time        `json:"reviewed_at,omitempty"`
	CreatedAt                time.Time         `json:"created_at"`
	UpdatedAt                time.Time         `json:"updated_at"`
}

func NewRelationship(want, workspace, from, to, author id.ID, kind RelationshipKind, rationale string, at time.Time) (Relationship, error) {
	return NewRelationshipWithEvidence(want, workspace, from, to, author, kind, rationale, nil, nil, at)
}

func NewRelationshipWithEvidence(want, workspace, from, to, author id.ID, kind RelationshipKind, rationale string, supporting, opposing []id.ID, at time.Time) (Relationship, error) {
	if want.IsZero() || workspace.IsZero() || from.IsZero() || to.IsZero() || author.IsZero() || at.IsZero() {
		return Relationship{}, ErrIDRequired
	}
	if from == to {
		return Relationship{}, ErrEventRelationshipSelf
	}
	if !validRelationshipKind(kind) {
		return Relationship{}, ErrEventRelationshipUnknown
	}
	rationale = strings.TrimSpace(rationale)
	if rationale == "" || !valid(rationale, MaxEventRelationshipDetail) {
		return Relationship{}, ErrEventRelationshipRationale
	}
	supporting, err := observationLinks(supporting)
	if err != nil {
		return Relationship{}, err
	}
	opposing, err = observationLinks(opposing)
	if err != nil {
		return Relationship{}, err
	}
	if supporting == nil {
		supporting = []id.ID{}
	}
	if opposing == nil {
		opposing = []id.ID{}
	}
	for _, left := range supporting {
		for _, right := range opposing {
			if left == right {
				return Relationship{}, ErrDuplicateRelationshipObservation
			}
		}
	}
	return Relationship{ID: want, WorkspaceID: workspace, FromEventID: from, ToEventID: to, Kind: kind, Rationale: rationale, State: RelationshipProposed, SupportingObservationIDs: supporting, OpposingObservationIDs: opposing, Author: author, UpdatedBy: author, CreatedAt: at, UpdatedAt: at}, nil
}

func (r Relationship) Review(by id.ID, state RelationshipState, note string, at time.Time) (Relationship, error) {
	if r.ID.IsZero() || r.WorkspaceID.IsZero() || by.IsZero() || at.IsZero() {
		return r, ErrIDRequired
	}
	if state != RelationshipProposed && state != RelationshipAccepted && state != RelationshipRejected {
		return r, ErrEventRelationshipStateUnknown
	}
	note = strings.TrimSpace(note)
	if state != RelationshipProposed && (note == "" || !valid(note, MaxEventRelationshipDetail)) {
		return r, ErrEventRelationshipReviewRequired
	}
	next := r
	next.State, next.ReviewNote, next.UpdatedBy, next.UpdatedAt = state, note, by, at
	next.ReviewedBy, next.ReviewedAt = nil, nil
	if state != RelationshipProposed {
		reviewer, reviewedAt := by, at
		next.ReviewedBy, next.ReviewedAt = &reviewer, &reviewedAt
	}
	return next, nil
}

func validRelationshipKind(kind RelationshipKind) bool {
	switch kind {
	case RelationshipRelated, RelationshipPrecedes, RelationshipOverlaps, RelationshipSameOccurrence, RelationshipPossiblyCauses:
		return true
	default:
		return false
	}
}
