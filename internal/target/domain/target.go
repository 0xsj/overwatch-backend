package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const MaxNameLength = 200

// Kind is what sits at the target's root — decisions/0029. Two values, because
// CLAUDE.md names two framings and calls them "the same machinery at different
// roots": one table with a kind, rather than two tables.
type Kind uint8

const (
	KindOrganisation Kind = iota
	KindPerson
)

func (k Kind) String() string {
	if k == KindPerson {
		return "person"
	}
	return "organisation"
}

func ParseKind(s string) (Kind, error) {
	switch s {
	case "organisation":
		return KindOrganisation, nil
	case "person":
		return KindPerson, nil
	default:
		return KindOrganisation, ErrKindUnknown
	}
}

type Status uint8

const (
	StatusActive Status = iota
	StatusArchived
)

func (s Status) String() string {
	if s == StatusArchived {
		return "archived"
	}
	return "active"
}

func ParseStatus(s string) (Status, error) {
	switch s {
	case "active":
		return StatusActive, nil
	case "archived":
		return StatusArchived, nil
	default:
		return StatusActive, ErrKindUnknown
	}
}

// Target is the thing being looked at.
//
// **WorkspaceID is non-null and this is the first product row to carry one** —
// decisions/0005 becoming structural rather than remembered. A query that omits
// it returns another engagement's targets, with the caller authenticated and
// nothing about the code path looking wrong.
type Target struct {
	ID          id.ID
	WorkspaceID id.ID
	Name        string
	Kind        Kind
	Status      Status
	Version     int
	CreatedBy   id.ID
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ArchivedAt  time.Time

	// RootEntity is the node representing this target in the graph, and is
	// ALWAYS ZERO today — decisions/0029. `entity` does not exist, and the
	// column arrives in the migration that creates the row it points at. It is
	// declared here so a reader looking for 0009's "the target's root entity"
	// finds out where it went rather than assuming it was forgotten.
	RootEntity id.ID
}

func New(newID, workspace, createdBy id.ID, name string, kind Kind, at time.Time) (Target, error) {
	if newID.IsZero() || createdBy.IsZero() {
		return Target{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Target{}, ErrWorkspaceRequired
	}
	if at.IsZero() {
		return Target{}, ErrTimeRequired
	}
	folded, err := cleanName(name)
	if err != nil {
		return Target{}, err
	}
	return Target{
		ID: newID, WorkspaceID: workspace, Name: folded, Kind: kind,
		Status: StatusActive, Version: 1, CreatedBy: createdBy,
		CreatedAt: at, UpdatedAt: at,
	}, nil
}

func (t Target) Archived() bool { return t.Status == StatusArchived }

func (t Target) Rename(name string, at time.Time) (Target, error) {
	if at.IsZero() {
		return t, ErrTimeRequired
	}
	if t.Archived() {
		return t, ErrArchived
	}
	folded, err := cleanName(name)
	if err != nil {
		return t, err
	}
	if folded == t.Name {
		return t, nil
	}
	next := t
	next.Name = folded
	next.Version = t.Version + 1
	next.UpdatedAt = at
	return next, nil
}

// Archive hides a target and keeps the record — decisions/0029 and the same rule
// as every other archived thing here. The NAME is released, because the unique
// index is partial over live rows.
func (t Target) Archive(at time.Time) (Target, error) {
	if at.IsZero() {
		return t, ErrTimeRequired
	}
	if t.Archived() {
		return t, ErrArchived
	}
	next := t
	next.Status = StatusArchived
	next.ArchivedAt = at
	next.UpdatedAt = at
	next.Version = t.Version + 1
	return next, nil
}

func (t Target) Reopen(at time.Time) (Target, error) {
	if at.IsZero() {
		return t, ErrTimeRequired
	}
	if !t.Archived() {
		return t, ErrArchived
	}
	next := t
	next.Status = StatusActive
	next.ArchivedAt = time.Time{}
	next.UpdatedAt = at
	next.Version = t.Version + 1
	return next, nil
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrNameRequired
	}
	if len(name) > MaxNameLength {
		return "", ErrNameTooLong
	}
	return name, nil
}

const (
	EventTargetAdded    = "target.added"
	EventTargetRenamed  = "target.renamed"
	EventTargetArchived = "target.archived"
	EventTargetReopened = "target.reopened"

	SubjectKind = "target"
)

// Added and the rest are DECISIONS — somebody chose to start looking at this.
// They are tenanted to the workspace, so they land on that engagement's audit
// log rather than the firm's.
type Added struct {
	TargetID    string `json:"target_id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
}

type Renamed struct {
	TargetID    string `json:"target_id"`
	WorkspaceID string `json:"workspace_id"`
	From        string `json:"from"`
	To          string `json:"to"`
}

type Archived struct {
	TargetID    string `json:"target_id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
}

type Reopened struct {
	TargetID    string `json:"target_id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
}
