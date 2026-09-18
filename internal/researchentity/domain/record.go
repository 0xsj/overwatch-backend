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
	MaxNameLength        = 400
	MaxDescriptionLength = 4000
	MaxObservationLinks  = 12
)

type Kind string

const (
	Person       Kind = "person"
	Account      Kind = "account"
	Organisation Kind = "organisation"
	Place        Kind = "place"
)

func (k Kind) String() string { return string(k) }

func ParseKind(raw string) (Kind, error) {
	switch Kind(strings.TrimSpace(raw)) {
	case Person, Account, Organisation, Place:
		return Kind(strings.TrimSpace(raw)), nil
	default:
		return "", ErrKindUnknown
	}
}

// Record is a researcher-authored OSINT object. It names what the researcher
// is tracking without asserting that two records identify the same real-world
// subject.
type Record struct {
	ID             id.ID     `json:"record_id"`
	WorkspaceID    id.ID     `json:"workspace_id"`
	Kind           Kind      `json:"kind"`
	Name           string    `json:"name"`
	Description    string    `json:"description,omitempty"`
	ObservationIDs []id.ID   `json:"observation_ids"`
	Author         id.ID     `json:"author"`
	UpdatedBy      id.ID     `json:"updated_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func New(want, workspace, author id.ID, kind, name, description string, observations []id.ID, at time.Time) (Record, error) {
	if want.IsZero() || author.IsZero() {
		return Record{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Record{}, ErrWorkspaceRequired
	}
	if at.IsZero() {
		return Record{}, ErrTimeRequired
	}
	cleaned, err := clean(kind, name, description, observations)
	if err != nil {
		return Record{}, err
	}
	return Record{ID: want, WorkspaceID: workspace, Kind: cleaned.kind, Name: cleaned.name,
		Description: cleaned.description, ObservationIDs: cleaned.observations,
		Author: author, UpdatedBy: author, CreatedAt: at, UpdatedAt: at}, nil
}

func (r Record) Edit(by id.ID, kind, name, description string, observations []id.ID, at time.Time) (Record, error) {
	if by.IsZero() {
		return r, ErrIDRequired
	}
	if at.IsZero() {
		return r, ErrTimeRequired
	}
	cleaned, err := clean(kind, name, description, observations)
	if err != nil {
		return r, err
	}
	next := r
	next.Kind, next.Name, next.Description = cleaned.kind, cleaned.name, cleaned.description
	next.ObservationIDs = cleaned.observations
	next.UpdatedBy, next.UpdatedAt = by, at
	return next, nil
}

type cleanedRecord struct {
	kind              Kind
	name, description string
	observations      []id.ID
}

func clean(kind, name, description string, observations []id.ID) (cleanedRecord, error) {
	parsed, err := ParseKind(kind)
	if err != nil {
		return cleanedRecord{}, err
	}
	name, description = strings.TrimSpace(name), strings.TrimSpace(description)
	if name == "" || !valid(name, MaxNameLength) {
		if name != "" && len(name) > MaxNameLength {
			return cleanedRecord{}, ErrNameTooLong
		}
		return cleanedRecord{}, ErrNameRequired
	}
	if !valid(description, MaxDescriptionLength) {
		return cleanedRecord{}, ErrDescriptionTooLong
	}
	links, err := observationLinks(observations)
	if err != nil {
		return cleanedRecord{}, err
	}
	return cleanedRecord{kind: parsed, name: name, description: description, observations: links}, nil
}

func valid(value string, limit int) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0) && len(value) <= limit
}

func observationLinks(input []id.ID) ([]id.ID, error) {
	if len(input) > MaxObservationLinks {
		return nil, ErrObservationTooMany
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

const EventChanged = "research.entity.changed"

type Changed struct {
	WorkspaceID string `json:"workspace_id"`
	RecordID    string `json:"record_id"`
	UpdatedBy   string `json:"updated_by"`
	Edit        bool   `json:"edit"`
}
