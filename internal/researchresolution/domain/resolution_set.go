package domain

import (
	"bytes"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

// ResolutionSet is the bounded, explicit review record for treating several
// authored records as aliases of one canonical record. It intentionally keeps
// the alias records alive; accepting it only adds their citations to the
// canonical record.
type ResolutionSet struct {
	ID                            id.ID      `json:"resolution_set_id"`
	WorkspaceID                   id.ID      `json:"workspace_id"`
	AliasRecordIDs                []id.ID    `json:"alias_record_ids"`
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

const MaxResolutionSetAliases = 3

func NewSet(want, workspace, canonical, proposer id.ID, aliases []id.ID, rationale string, at time.Time) (ResolutionSet, error) {
	if want.IsZero() || canonical.IsZero() || proposer.IsZero() {
		return ResolutionSet{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return ResolutionSet{}, ErrWorkspaceRequired
	}
	if at.IsZero() {
		return ResolutionSet{}, ErrTimeRequired
	}
	if len(aliases) < 1 || len(aliases) > MaxResolutionSetAliases {
		return ResolutionSet{}, ErrResolutionSetSize
	}
	aliases = uniqueIDs(aliases)
	if len(aliases) < 1 || len(aliases) > MaxResolutionSetAliases {
		return ResolutionSet{}, ErrResolutionSetSize
	}
	for _, alias := range aliases {
		if alias.IsZero() {
			return ResolutionSet{}, ErrIDRequired
		}
		if alias == canonical {
			return ResolutionSet{}, ErrResolutionSetOverlap
		}
	}
	rationale = strings.TrimSpace(rationale)
	if rationale == "" || !utf8.ValidString(rationale) || strings.ContainsRune(rationale, 0) {
		return ResolutionSet{}, ErrRationaleRequired
	}
	if len(rationale) > MaxRationale {
		return ResolutionSet{}, ErrRationaleTooLong
	}
	return ResolutionSet{ID: want, WorkspaceID: workspace, AliasRecordIDs: aliases, CanonicalRecordID: canonical, State: Proposed, Rationale: rationale, ProposedBy: proposer, ProposedAt: at, CanonicalObservationIDsBefore: []id.ID{}, AddedObservationIDs: []id.ID{}}, nil
}

func (r ResolutionSet) Accept(by id.ID, before, added []id.ID, at time.Time) (ResolutionSet, error) {
	if by.IsZero() {
		return ResolutionSet{}, ErrIDRequired
	}
	if at.IsZero() {
		return ResolutionSet{}, ErrTimeRequired
	}
	if r.State != Proposed {
		return ResolutionSet{}, ErrStateConflict
	}
	before, err := observationLinks(before)
	if err != nil {
		return ResolutionSet{}, err
	}
	added, err = observationLinks(added)
	if err != nil {
		return ResolutionSet{}, err
	}
	if len(union(before, added)) > 12 {
		return ResolutionSet{}, ErrObservationTooMany
	}
	next := r
	next.State, next.ReviewedBy, next.ReviewedAt = Accepted, by, &at
	next.CanonicalObservationIDsBefore, next.AddedObservationIDs = before, added
	return next, nil
}

func (r ResolutionSet) Reject(by id.ID, at time.Time) (ResolutionSet, error) {
	if by.IsZero() {
		return ResolutionSet{}, ErrIDRequired
	}
	if at.IsZero() {
		return ResolutionSet{}, ErrTimeRequired
	}
	if r.State != Proposed {
		return ResolutionSet{}, ErrStateConflict
	}
	next := r
	next.State, next.ReviewedBy, next.ReviewedAt = Rejected, by, &at
	return next, nil
}

func (r ResolutionSet) Reverse(by id.ID, at time.Time) (ResolutionSet, error) {
	if by.IsZero() {
		return ResolutionSet{}, ErrIDRequired
	}
	if at.IsZero() {
		return ResolutionSet{}, ErrTimeRequired
	}
	if r.State != Accepted {
		return ResolutionSet{}, ErrStateConflict
	}
	next := r
	next.State, next.ReversedBy, next.ReversedAt = Reversed, by, &at
	return next, nil
}

func (r ResolutionSet) CanonicalObservationIDsAfter() []id.ID {
	return union(r.CanonicalObservationIDsBefore, r.AddedObservationIDs)
}

func (r ResolutionSet) ReviewedAtValue() time.Time {
	if r.ReviewedAt == nil {
		return time.Time{}
	}
	return *r.ReviewedAt
}

func (r ResolutionSet) ReversedAtValue() time.Time {
	if r.ReversedAt == nil {
		return time.Time{}
	}
	return *r.ReversedAt
}

func uniqueIDs(input []id.ID) []id.ID {
	out := append([]id.ID(nil), input...)
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i][:], out[j][:]) < 0 })
	unique := out[:0]
	for _, one := range out {
		if one.IsZero() || len(unique) == 0 || unique[len(unique)-1] != one {
			unique = append(unique, one)
		}
	}
	return unique
}
