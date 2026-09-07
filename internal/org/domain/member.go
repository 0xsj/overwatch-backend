package domain

import (
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Role uint8

const (
	RoleOwner Role = iota
	RoleAdmin
	RoleMember
	RoleGuest
	RoleClient
)

func (r Role) String() string {
	switch r {
	case RoleOwner:
		return "owner"
	case RoleAdmin:
		return "admin"
	case RoleMember:
		return "member"
	case RoleGuest:
		return "guest"
	case RoleClient:
		return "client"
	default:
		return "member"
	}
}

func ParseRole(s string) (Role, error) {
	switch s {
	case "owner":
		return RoleOwner, nil
	case "admin":
		return RoleAdmin, nil
	case "member":
		return RoleMember, nil
	case "guest":
		return RoleGuest, nil
	case "client":
		return RoleClient, nil
	default:
		return RoleMember, ErrRoleUnknown
	}
}

// Ceiling is the highest Level this role may ever reach on a workspace, and it
// is the outer min() of decisions/0019's `min(ceiling, max(grants))`. It is what
// stops a forgotten grant surviving a demotion.
func (r Role) Ceiling() Level {
	switch r {
	case RoleOwner, RoleAdmin:
		return LevelAdmin
	case RoleMember, RoleGuest:
		return LevelWrite
	default:
		// A client is off the ladder — decisions/0019. Its capabilities come
		// from the role, and the ladder caps it at what it can never exceed.
		return LevelRead
	}
}

// SeesEveryWorkspace is the ONE exemption — decisions/0019. An owner is admin on
// every workspace in their org with no grant written, because the firm's
// principal is accountable for every engagement it runs.
//
// **Admin is deliberately not exempt.** An admin manages people, not work: they
// invite, remove and change roles, and are granted an engagement like anybody
// else. Promoting somebody to handle invitations must not hand them every
// client's wall.
func (r Role) SeesEveryWorkspace() bool { return r == RoleOwner }

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

type Member struct {
	ID         id.ID
	OrgID      id.ID
	AccountID  id.ID
	Role       Role
	Status     Status
	Version    int
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ArchivedAt time.Time
}

func NewMember(newID, orgID, accountID id.ID, role Role, at time.Time) (Member, error) {
	if newID.IsZero() || orgID.IsZero() || accountID.IsZero() {
		return Member{}, ErrIDRequired
	}
	if at.IsZero() {
		return Member{}, ErrTimeRequired
	}
	return Member{
		ID:        newID,
		OrgID:     orgID,
		AccountID: accountID,
		Role:      role,
		Status:    StatusActive,
		Version:   1,
		CreatedAt: at,
		UpdatedAt: at,
	}, nil
}

func (m Member) Archived() bool { return m.Status == StatusArchived }

// ChangeRole is a demotion or a promotion. The LAST-OWNER rule is not here:
// it is a property of a SET of rows under a predicate, which a single value
// cannot see — decisions/0026 puts it in the command, over locked rows.
func (m Member) ChangeRole(role Role, at time.Time) (Member, error) {
	if at.IsZero() {
		return m, ErrTimeRequired
	}
	if m.Archived() {
		return m, ErrMemberArchived
	}
	if m.Role == role {
		return m, nil
	}
	next := m
	next.Role = role
	next.Version = m.Version + 1
	next.UpdatedAt = at
	return next, nil
}

func (m Member) Archive(at time.Time) (Member, error) {
	if at.IsZero() {
		return m, ErrTimeRequired
	}
	if m.Archived() {
		return m, ErrMemberArchived
	}
	next := m
	next.Status = StatusArchived
	next.ArchivedAt = at
	next.UpdatedAt = at
	next.Version = m.Version + 1
	return next, nil
}
