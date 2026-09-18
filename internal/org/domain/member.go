package domain

import (
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"

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

	// ExpiresAt is the TIME BOX. `0019` defines `guest` and `client` as
	// time-boxed and `0025` names it again; nothing stored one until
	// decisions/0042 gave `client` real access to deliverables and made a
	// client invited in January a client reading Q3's report in perpetuity.
	//
	// **Non-zero EXACTLY for `guest` and `client`.** An owner, an admin or a
	// member has no end date — they are the firm — and a nullable column that
	// could carry one for them would be a rule enforced nowhere, which is
	// CLAUDE.md §5 in one field.
	ExpiresAt time.Time
}

// TimeBoxed says a role must carry an end date. It is one function so the
// domain, the schema constraint and the write path cannot disagree about which
// two roles it is.
func (r Role) TimeBoxed() bool { return r == RoleGuest || r == RoleClient }

// Expired answers whether this membership's time box has passed. A membership
// with no box never expires, which is every owner, admin and member.
//
// **It is asked at the GATE and not only by a sweep.** A sweep runs on an
// interval, and between two ticks an expired guest still holds everything they
// held — so the authoritative answer has to be computed when access is
// evaluated. There is deliberately no `expired` status: a status a sweep sets
// is a second authority over a time this row already knows, and it would be
// wrong every time somebody changed the date.
func (m Member) Expired(now time.Time) bool {
	return !m.ExpiresAt.IsZero() && !now.Before(m.ExpiresAt)
}

// Box sets or clears the time box, and refuses every combination that would make
// the rule a lie.
func (m Member) Box(until time.Time, at time.Time) (Member, error) {
	if at.IsZero() {
		return m, ErrTimeRequired
	}
	if m.Role.TimeBoxed() {
		if until.IsZero() {
			return m, ErrTimeBoxRequired
		}
		if !until.After(at) {
			// An end date already past is not a time box, it is a removal
			// spelled confusingly. Somebody meant one of the two and should say
			// which.
			return m, ErrTimeBoxPast
		}
	} else if !until.IsZero() {
		return m, ErrTimeBoxForbidden
	}
	next := m
	next.ExpiresAt = until
	next.Version = m.Version + 1
	next.UpdatedAt = at
	return next, nil
}

// NewMember seats somebody. `until` is the TIME BOX and is REQUIRED for a guest
// or a client and forbidden for anybody else — see [Member.Box].
func NewMember(newID, orgID, accountID id.ID, role Role, until time.Time,
	at time.Time) (Member, error) {
	if newID.IsZero() || orgID.IsZero() || accountID.IsZero() {
		return Member{}, ErrIDRequired
	}
	if at.IsZero() {
		return Member{}, ErrTimeRequired
	}
	fresh := Member{
		ID:        newID,
		OrgID:     orgID,
		AccountID: accountID,
		Role:      role,
		Status:    StatusActive,
		Version:   1,
		CreatedAt: at,
		UpdatedAt: at,
	}
	boxed, err := fresh.Box(until, at)
	if err != nil {
		return Member{}, err
	}
	// Box bumps the version, which is right for an edit and wrong for a birth.
	boxed.Version = 1
	return boxed, nil
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
	// **CHANGING ROLE MOVES THE TIME BOX WITH IT.** Promoting a guest to a
	// member clears the date — a member is the firm and has no end — and
	// demoting anybody TO a guest or a client leaves them needing one, which
	// `Box` then refuses to skip.
	//
	// The caller supplies the date through [Member.Box] afterwards, so this
	// clears rather than guesses: a promotion that silently kept a stale expiry
	// would time-box somebody who is no longer time-boxed.
	next.ExpiresAt = time.Time{}
	if role.TimeBoxed() {
		return next, ErrTimeBoxRequired
	}
	return next, nil
}

// ChangeRoleUntil is the demotion-to-a-boxed-role path, as one act. Two calls
// would leave a guest with no end date between them, and the row would be
// refused by the constraint rather than by a rule anybody can read.
func (m Member) ChangeRoleUntil(role Role, until time.Time, at time.Time) (Member, error) {
	next, err := m.ChangeRole(role, at)
	if err != nil && !errors.Is(err, ErrTimeBoxRequired) {
		return m, err
	}
	boxed, err := next.Box(until, at)
	if err != nil {
		return m, err
	}
	boxed.Version = next.Version
	return boxed, nil
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
