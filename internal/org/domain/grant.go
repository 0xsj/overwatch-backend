package domain

import (
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Level uint8

const (
	LevelNone Level = iota
	LevelRead
	LevelWrite
	LevelAdmin
)

func (l Level) String() string {
	switch l {
	case LevelRead:
		return "read"
	case LevelWrite:
		return "write"
	case LevelAdmin:
		return "admin"
	default:
		return "none"
	}
}

func ParseLevel(s string) (Level, error) {
	switch s {
	case "none":
		return LevelNone, nil
	case "read":
		return LevelRead, nil
	case "write":
		return LevelWrite, nil
	case "admin":
		return LevelAdmin, nil
	default:
		return LevelNone, ErrLevelUnknown
	}
}

// Visible is the predicate every list is filtered by. `none` means the workspace
// is ABSENT, not refused — a 403 tells an analyst that a client they cannot see
// exists, which for the client behind that wall is the leak itself.
func (l Level) Visible() bool { return l > LevelNone }

// AtLeast is how a caller asks its question. `grant.AtLeast(LevelWrite)` reads
// as the rule it enforces; `grant >= 2` does not, and stops being true the day a
// rung is inserted.
func (l Level) AtLeast(want Level) bool { return l >= want }

// Grant is one member's access to one workspace.
//
// WorkspaceID is an opaque column and NOT a foreign key — decisions/0017 forbids
// one crossing a schema. This table lives in org rather than workspace because
// every authorisation check reads the role and the grant together, and splitting
// the two inputs of one min() across two schemas makes the check two reads now
// and two network calls the day these become services.
type Grant struct {
	ID          id.ID
	OrgID       id.ID
	AccountID   id.ID
	WorkspaceID id.ID
	Level       Level
	Version     int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func NewGrant(newID, orgID, accountID, workspaceID id.ID, level Level, at time.Time) (Grant, error) {
	if newID.IsZero() || orgID.IsZero() || accountID.IsZero() || workspaceID.IsZero() {
		return Grant{}, ErrIDRequired
	}
	if at.IsZero() {
		return Grant{}, ErrTimeRequired
	}
	// A grant of `none` is a revocation, and revoking is deleting the row. A
	// stored `none` would be a second way to express absence, and the two would
	// disagree the first time a query forgot one of them.
	if !level.Visible() {
		return Grant{}, ErrGrantEmpty
	}
	return Grant{
		ID:          newID,
		OrgID:       orgID,
		AccountID:   accountID,
		WorkspaceID: workspaceID,
		Level:       level,
		Version:     1,
		CreatedAt:   at,
		UpdatedAt:   at,
	}, nil
}

func (g Grant) Retarget(level Level, at time.Time) (Grant, error) {
	if at.IsZero() {
		return g, ErrTimeRequired
	}
	if !level.Visible() {
		return g, ErrGrantEmpty
	}
	next := g
	next.Level = level
	next.Version = g.Version + 1
	next.UpdatedAt = at
	return next, nil
}

// Effective is decisions/0019's whole rule, in one place:
//
//	min(role ceiling, max(grants))
//
// GitHub's union is inside the max, where it is safe. 0005's intersection is the
// outer min, which is what stops a forgotten grant surviving a revoked role.
//
// **An empty grant set is `none`.** The intersection of no grants is not
// everything, and a member with no grant on a workspace cannot see it exists.
// The single exemption is the org owner, who is admin everywhere with no row.
func Effective(role Role, held []Level) Level {
	if role.SeesEveryWorkspace() {
		return LevelAdmin
	}
	best := LevelNone
	for _, l := range held {
		if l > best {
			best = l
		}
	}
	if ceiling := role.Ceiling(); best > ceiling {
		return ceiling
	}
	return best
}
