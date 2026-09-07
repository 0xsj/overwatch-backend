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
	ErrNotArchived   = errors.New(errors.Conflict, "the workspace is not closed")
	ErrStaleWrite    = errors.New(errors.Conflict, "the record changed since it was read")
	// A redelivery, not a failure — decisions/0007 and 0017.
	ErrAlreadyProvisioned = errors.New(errors.Conflict, "already provisioned from that event")
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

	// SourceEvent is the event this workspace was provisioned from, and is zero
	// for one a person created — decisions/0017.
	SourceEvent id.ID

	// CreatedBy is who started this engagement — decisions/0020. It is
	// IMMUTABLE, it is never consulted by the authorisation gate, and it may be
	// ZERO on a row written before it was recorded. Zero means "never
	// recorded", not "nobody": the two are a pair CLAUDE.md says must not
	// collapse, and a backfilled guess would collapse them.
	CreatedBy id.ID
}

// New refuses a zero CreatedBy while the struct permits one, and the asymmetry
// is the point: LOADING a row written before decisions/0020 is a different
// operation from CREATING one now, and only the second can insist.
func New(newID, orgID, createdBy id.ID, name string, at time.Time) (Workspace, error) {
	if newID.IsZero() || orgID.IsZero() || createdBy.IsZero() {
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
		CreatedBy: createdBy,
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

// Reopen brings a closed engagement back — decisions/0027. Closing is
// reversible because a client engagement that resumes next quarter is ordinary,
// and the alternative splits one client's record across two workspaces.
//
// **It can fail on the NAME.** Closing releases it — the unique index is partial
// — so another engagement may have taken it meanwhile. The store answers
// ErrNameTaken and the caller renames first; silently renaming here would change
// a client's record without saying so.
func (w Workspace) Reopen(at time.Time) (Workspace, error) {
	if at.IsZero() {
		return w, ErrTimeRequired
	}
	if !w.Archived() {
		return w, ErrNotArchived
	}
	next := w
	next.Status = StatusActive
	next.ArchivedAt = time.Time{}
	next.UpdatedAt = at
	next.Version = w.Version + 1
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
	EventWorkspaceRenamed  = "workspace.renamed"
	EventWorkspaceReopened = "workspace.reopened"
	EventWorkspaceArchived = "workspace.archived"

	// EventWorkspaceOpened is the DECISION beside the work — decisions/0020 and
	// 0014. `workspace.created` is a unit of work: it can fail, and when the
	// registration chain does it nobody chose anything. A PERSON opening an
	// engagement chose to start work for a client, and that is an act with an
	// author, no outcome column, and a reason to outlive the journal's
	// retention. One act, two records, joined by correlation.
	EventWorkspaceOpened = "workspace.opened"

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

type Renamed struct {
	WorkspaceID string `json:"workspace_id"`
	OrgID       string `json:"org_id"`
	From        string `json:"from"`
	To          string `json:"to"`
}

type Reopened struct {
	WorkspaceID string `json:"workspace_id"`
	OrgID       string `json:"org_id"`
	Name        string `json:"name"`
}

// Opened names who started the engagement. org subscribes to it to give the
// opener an admin grant, which is why OpenedBy is in the payload rather than
// left to be read off the actor: a subscriber must not have to trust that a
// middleware was wired — decisions/0013.
type Opened struct {
	WorkspaceID string `json:"workspace_id"`
	OrgID       string `json:"org_id"`
	OpenedBy    string `json:"opened_by"`
}
