package domain

import (
	"bytes"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const MaxRationale = 4000

type State string

const (
	Proposed State = "proposed"
	Accepted State = "accepted"
	Rejected State = "rejected"
	Reversed State = "reversed"
)

func (s State) String() string { return string(s) }

func ParseState(raw string) (State, error) {
	switch State(strings.TrimSpace(raw)) {
	case Proposed, Accepted, Rejected, Reversed:
		return State(strings.TrimSpace(raw)), nil
	default:
		return "", ErrStateUnknown
	}
}

type Resolution struct {
	ID                            id.ID      `json:"resolution_id"`
	WorkspaceID                   id.ID      `json:"workspace_id"`
	AliasRecordID                 id.ID      `json:"alias_record_id"`
	CanonicalRecordID             id.ID      `json:"canonical_record_id"`
	State                         State      `json:"state"`
	Rationale                     string     `json:"rationale"`
	ProposedBy                    id.ID      `json:"proposed_by"`
	ProposedAt                    time.Time  `json:"proposed_at"`
	ReviewedBy                    id.ID      `json:"reviewed_by,omitempty"`
	ReviewedAt                    *time.Time `json:"reviewed_at,omitempty"`
	ReversedBy                    id.ID      `json:"reversed_by,omitempty"`
	ReversedAt                    *time.Time `json:"reversed_at,omitempty"`
	CanonicalObservationIDsBefore []id.ID    `json:"canonical_observation_ids_before"`
	AddedObservationIDs           []id.ID    `json:"added_observation_ids"`
}

// Impact is the read-side projection for a resolution review. It deliberately
// reports references rather than rewriting them: accepting a resolution keeps
// authored connection, event, brief, and snapshot identifiers intact.
type Impact struct {
	Connections []ConnectionImpact `json:"connections"`
	Events      []EventImpact      `json:"events"`
	Briefs      []BriefImpact      `json:"briefs"`
	Snapshots   []SnapshotImpact   `json:"snapshots"`
}

type ConnectionImpact struct {
	ID             id.ID  `json:"connection_id"`
	FromRecordID   id.ID  `json:"from_record_id"`
	FromRecordName string `json:"from_record_name"`
	ToRecordID     id.ID  `json:"to_record_id"`
	ToRecordName   string `json:"to_record_name"`
	Kind           string `json:"kind"`
	State          string `json:"state"`
}

type EventImpact struct {
	ID       id.ID  `json:"event_id"`
	Title    string `json:"title"`
	SortDate string `json:"sort_date,omitempty"`
}

type BriefImpact struct {
	ID        id.ID     `json:"brief_id"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SnapshotImpact struct {
	ID       id.ID     `json:"snapshot_id"`
	BriefID  id.ID     `json:"brief_id"`
	Title    string    `json:"title"`
	FrozenAt time.Time `json:"frozen_at"`
}

func (r Resolution) ReviewedAtValue() time.Time {
	if r.ReviewedAt == nil {
		return time.Time{}
	}
	return *r.ReviewedAt
}

func (r Resolution) ReversedAtValue() time.Time {
	if r.ReversedAt == nil {
		return time.Time{}
	}
	return *r.ReversedAt
}

func New(want, workspace, alias, canonical, proposer id.ID, rationale string, at time.Time) (Resolution, error) {
	if want.IsZero() || alias.IsZero() || canonical.IsZero() || proposer.IsZero() {
		return Resolution{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Resolution{}, ErrWorkspaceRequired
	}
	if alias == canonical {
		return Resolution{}, ErrSameRecord
	}
	if at.IsZero() {
		return Resolution{}, ErrTimeRequired
	}
	rationale = strings.TrimSpace(rationale)
	if rationale == "" || !utf8.ValidString(rationale) || strings.ContainsRune(rationale, 0) {
		return Resolution{}, ErrRationaleRequired
	}
	if len(rationale) > MaxRationale {
		return Resolution{}, ErrRationaleTooLong
	}
	return Resolution{ID: want, WorkspaceID: workspace, AliasRecordID: alias, CanonicalRecordID: canonical, State: Proposed, Rationale: rationale, ProposedBy: proposer, ProposedAt: at, CanonicalObservationIDsBefore: []id.ID{}, AddedObservationIDs: []id.ID{}}, nil
}

func (r Resolution) Accept(by id.ID, before, added []id.ID, at time.Time) (Resolution, error) {
	if by.IsZero() {
		return Resolution{}, ErrIDRequired
	}
	if at.IsZero() {
		return Resolution{}, ErrTimeRequired
	}
	if r.State != Proposed {
		return Resolution{}, ErrStateConflict
	}
	before, err := observationLinks(before)
	if err != nil {
		return Resolution{}, err
	}
	added, err = observationLinks(added)
	if err != nil {
		return Resolution{}, err
	}
	if len(union(before, added)) > 12 {
		return Resolution{}, ErrObservationTooMany
	}
	next := r
	next.State, next.ReviewedBy, next.ReviewedAt = Accepted, by, &at
	next.CanonicalObservationIDsBefore, next.AddedObservationIDs = before, added
	return next, nil
}

func (r Resolution) Reject(by id.ID, at time.Time) (Resolution, error) {
	if by.IsZero() {
		return Resolution{}, ErrIDRequired
	}
	if at.IsZero() {
		return Resolution{}, ErrTimeRequired
	}
	if r.State != Proposed {
		return Resolution{}, ErrStateConflict
	}
	next := r
	next.State, next.ReviewedBy, next.ReviewedAt = Rejected, by, &at
	return next, nil
}

func (r Resolution) Reverse(by id.ID, at time.Time) (Resolution, error) {
	if by.IsZero() {
		return Resolution{}, ErrIDRequired
	}
	if at.IsZero() {
		return Resolution{}, ErrTimeRequired
	}
	if r.State != Accepted {
		return Resolution{}, ErrStateConflict
	}
	next := r
	next.State, next.ReversedBy, next.ReversedAt = Reversed, by, &at
	return next, nil
}

func observationLinks(input []id.ID) ([]id.ID, error) {
	if len(input) > 12 {
		return nil, ErrObservationTooMany
	}
	links := append([]id.ID(nil), input...)
	sort.Slice(links, func(i, j int) bool { return bytes.Compare(links[i][:], links[j][:]) < 0 })
	for i, one := range links {
		if one.IsZero() {
			return nil, ErrIDRequired
		}
		if i > 0 && links[i-1] == one {
			return nil, ErrObservationTooMany
		}
	}
	return links, nil
}

func union(left, right []id.ID) []id.ID {
	all := append(append([]id.ID(nil), left...), right...)
	sort.Slice(all, func(i, j int) bool { return bytes.Compare(all[i][:], all[j][:]) < 0 })
	out := all[:0]
	for _, one := range all {
		if len(out) == 0 || out[len(out)-1] != one {
			out = append(out, one)
		}
	}
	return out
}

func (r Resolution) CanonicalObservationIDsAfter() []id.ID {
	return union(r.CanonicalObservationIDsBefore, r.AddedObservationIDs)
}

const EventChanged = "research.resolution.changed"

type Changed struct {
	WorkspaceID       string `json:"workspace_id"`
	ResolutionID      string `json:"resolution_id"`
	AliasRecordID     string `json:"alias_record_id"`
	CanonicalRecordID string `json:"canonical_record_id"`
	State             string `json:"state"`
	ChangedBy         string `json:"changed_by"`
}
