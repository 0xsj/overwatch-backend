package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

const MaxNameLength = 120

var (
	ErrIDRequired    = errors.New(errors.Invalid, "an identifier is required")
	ErrTimeRequired  = errors.New(errors.Invalid, "an instant is required")
	ErrNameRequired  = errors.New(errors.Invalid, "a workspace needs a name")
	ErrNameTooLong   = errors.New(errors.Invalid, "the name is too long")
	ErrStatusUnknown = errors.New(errors.Invalid, "not a status this system knows")
	ErrNotFound      = errors.New(errors.NotFound, "workspace")
	ErrNameTaken     = errors.New(errors.Conflict, "that name is already used in this organisation")
	ErrArchived      = errors.New(errors.Conflict, "the workspace is archived")
	ErrStaleWrite    = errors.New(errors.Conflict, "the record changed since it was read")
)

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
		return StatusActive, ErrStatusUnknown
	}
}

type Workspace struct {
	ID         id.ID
	OrgID      id.ID
	Name       string
	Status     Status
	Version    int
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ArchivedAt time.Time
}

func New(newID, orgID id.ID, name string, at time.Time) (Workspace, error) {
	if newID.IsZero() || orgID.IsZero() {
		return Workspace{}, ErrIDRequired
	}
	if at.IsZero() {
		return Workspace{}, ErrTimeRequired
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Workspace{}, ErrNameRequired
	}
	if len(name) > MaxNameLength {
		return Workspace{}, ErrNameTooLong
	}
	return Workspace{
		ID:        newID,
		OrgID:     orgID,
		Name:      name,
		Status:    StatusActive,
		Version:   1,
		CreatedAt: at,
		UpdatedAt: at,
	}, nil
}

func (w Workspace) Archived() bool { return w.Status == StatusArchived }

func (w Workspace) Rename(name string, at time.Time) (Workspace, error) {
	if at.IsZero() {
		return w, ErrTimeRequired
	}
	if w.Archived() {
		return w, ErrArchived
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return w, ErrNameRequired
	}
	if len(name) > MaxNameLength {
		return w, ErrNameTooLong
	}
	next := w
	next.Name = name
	next.Version = w.Version + 1
	next.UpdatedAt = at
	return next, nil
}

func (w Workspace) Archive(at time.Time) (Workspace, error) {
	if at.IsZero() {
		return w, ErrTimeRequired
	}
	if w.Archived() {
		return w, ErrArchived
	}
	next := w
	next.Status = StatusArchived
	next.ArchivedAt = at
	next.UpdatedAt = at
	next.Version = w.Version + 1
	return next, nil
}

const (
	EventWorkspaceCreated  = "workspace.created"
	EventWorkspaceArchived = "workspace.archived"

	SubjectKind = "workspace"
)

type Created struct {
	WorkspaceID string `json:"workspace_id"`
	OrgID       string `json:"org_id"`
}

type Archived struct {
	WorkspaceID string `json:"workspace_id"`
	OrgID       string `json:"org_id"`
}
