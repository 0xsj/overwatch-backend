package query

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// ErrNoAccess is NotFound and deliberately NOT Forbidden — decisions/0005 and
// 0019. `none` means the workspace is INVISIBLE: a 403 tells an analyst that a
// client they cannot see exists, which for the client behind that wall is the
// leak itself. The same error covers a workspace that does not exist and one
// they are simply not on, because distinguishing them is the disclosure.
var ErrNoAccess = errors.New(errors.NotFound, "workspace")

// AccessReader is the gate's read side. Two rows, and they are read together
// because the answer is min(role ceiling, max(grants)) and neither half means
// anything alone.
// Blocker is an org an account cannot walk away from: it is the last owner and
// somebody else is still in it — decisions/0028.
type Blocker struct {
	OrgID id.ID
	Name  string
}

type AccessReader interface {
	LiveMemberFor(ctx context.Context, orgID, account id.ID) (domain.Member, error)
	GrantsForAccount(ctx context.Context, account, org id.ID) ([]domain.Grant, error)

	MembersOf(ctx context.Context, orgID id.ID) ([]domain.Member, error)
	GrantsOnWorkspace(ctx context.Context, workspace id.ID) ([]domain.Grant, error)

	OrgsForAccount(ctx context.Context, account id.ID) ([]domain.Org, error)
}

// Reach is one caller's standing in one org: their role, and every grant they
// hold in it. It is resolved ONCE and answered from many times, which is what
// keeps /v1/me one query per org rather than one per workspace.
type Reach struct {
	OrgID  id.ID
	Role   domain.Role
	levels map[id.ID][]domain.Level
}

// On is the whole authorisation rule, at one call site.
//
// **PRECONDITION: the workspace must belong to this Reach's org.** Nothing here
// can check it — org cannot read workspace's table (decisions/0017) — and the
// owner exemption makes getting it wrong a disclosure rather than a mistake: an
// owner is `admin` on every workspace in THEIR org, and this method cannot see
// the second half, so an arbitrary id handed to an owner's Reach comes back
// `admin`. That is how a stranger read somebody else's engagement audit on
// 2026-09-07.
//
// Callers establish it one of two ways, and there is no third:
//
//	list  ask workspace.InOrg(thisOrg) and only ask about what it returned
//	point ask workspace.OrgOf(id) FIRST, then build the Reach for that org
//
// **An unknown workspace and a workspace with no grant answer identically**, and
// that is not laziness: the caller is told what they may do, which is nothing,
// and is never told whether the thing they named exists.
func (r Reach) On(workspace id.ID) domain.Level {
	return domain.Effective(r.Role, r.levels[workspace])
}

func (r Reach) Visible(workspace id.ID) bool { return r.On(workspace).Visible() }

// Allows is how a command asks. `Allows(id, domain.LevelWrite)` reads as the
// rule it enforces, and keeps working when a rung is inserted.
func (r Reach) Allows(workspace id.ID, want domain.Level) bool {
	return r.On(workspace).AtLeast(want)
}

type Access struct{ reader AccessReader }

func NewAccess(reader AccessReader) *Access {
	if reader == nil {
		panic("org: NewAccess with a nil reader")
	}
	return &Access{reader: reader}
}

// In resolves a caller's standing in one org. A caller who is not a live member
// gets ErrNoAccess — the org is as invisible as a workspace they are not on.
func (a *Access) In(ctx context.Context, account, org id.ID) (Reach, error) {
	if account.IsZero() || org.IsZero() {
		return Reach{}, domain.ErrIDRequired
	}
	member, err := a.reader.LiveMemberFor(ctx, org, account)
	if err != nil {
		if errors.Is(err, domain.ErrMemberNotFound) {
			return Reach{}, ErrNoAccess
		}
		return Reach{}, fmt.Errorf("org: reach: %w", err)
	}

	// An owner is admin everywhere with no row — decisions/0019. Reading their
	// grants anyway would be a query whose result is discarded, and the day
	// somebody deletes the exemption from Effective it would silently start
	// mattering.
	if member.Role.SeesEveryWorkspace() {
		return Reach{OrgID: org, Role: member.Role}, nil
	}

	held, err := a.reader.GrantsForAccount(ctx, account, org)
	if err != nil {
		return Reach{}, fmt.Errorf("org: reach: %w", err)
	}
	levels := make(map[id.ID][]domain.Level, len(held))
	for _, g := range held {
		levels[g.WorkspaceID] = append(levels[g.WorkspaceID], g.Level)
	}
	return Reach{OrgID: org, Role: member.Role, levels: levels}, nil
}

// Require is the capability gate decisions/0018 named as owed and 0019 made
// specific. A workspace operation calls it FIRST and uses nothing it returns
// until it has.
//
// **It refuses with ErrNoAccess and never with Forbidden**, so the caller cannot
// tell "you may not" from "there is no such thing".
func (a *Access) Require(ctx context.Context, account, org, workspace id.ID, want domain.Level) (Reach, error) {
	if workspace.IsZero() {
		return Reach{}, domain.ErrIDRequired
	}
	reach, err := a.In(ctx, account, org)
	if err != nil {
		return Reach{}, err
	}
	if !reach.Allows(workspace, want) {
		return Reach{}, ErrNoAccess
	}
	return reach, nil
}

// Seat is one person's access to one engagement, for the members × workspaces
// grid. It carries an account id and no name, because org cannot see identity's
// tables — the root joins them, the same shape /v1/me already has.
type WorkspaceSeat struct {
	AccountID id.ID
	Level     domain.Level
	Role      domain.Role
}

// OnWorkspace lists who is on one engagement.
//
// **Effective and not stored.** A member holding an `admin` grant whose role was
// later lowered to `client` appears as `read`, because that is what they can
// actually do — decisions/0019 and 0023. Showing the stored level would make the
// grid display access nobody has.
//
// The ORG OWNER is included with `admin` despite holding no row, because the
// exemption is the reason they can see it and a grid that omits them is a grid
// that says nobody administers this engagement.
//
// **It does not authorise.** The caller resolves its own Reach first; see root.
func (a *Access) OnWorkspace(ctx context.Context, org, workspace id.ID) ([]WorkspaceSeat, error) {
	if org.IsZero() || workspace.IsZero() {
		return nil, domain.ErrIDRequired
	}
	members, err := a.reader.MembersOf(ctx, org)
	if err != nil {
		return nil, fmt.Errorf("org: on workspace: %w", err)
	}
	held, err := a.reader.GrantsOnWorkspace(ctx, workspace)
	if err != nil {
		return nil, fmt.Errorf("org: on workspace: %w", err)
	}
	levels := make(map[id.ID][]domain.Level, len(held))
	for _, g := range held {
		levels[g.AccountID] = append(levels[g.AccountID], g.Level)
	}

	out := make([]WorkspaceSeat, 0, len(members))
	for _, m := range members {
		level := domain.Effective(m.Role, levels[m.AccountID])
		if !level.Visible() {
			continue
		}
		out = append(out, WorkspaceSeat{AccountID: m.AccountID, Level: level, Role: m.Role})
	}
	return out, nil
}

// Stranding lists the orgs this account cannot leave — decisions/0028.
//
// **An org that is ONLY this account does not block anything.** The rule 0026
// protects is "an org nobody can administer", and an org with no members at all
// is not that: Access.In refuses a non-member before any grant is read, so it is
// unreachable by construction. Nobody is stranded in a room nobody is in.
//
// Without that carve-out nobody could ever close an account, because
// registration makes every account the last owner of its own personal org.
func (a *Access) Stranding(ctx context.Context, account id.ID) ([]Blocker, error) {
	if account.IsZero() {
		return nil, domain.ErrIDRequired
	}
	orgs, err := a.reader.OrgsForAccount(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("org: stranding: %w", err)
	}
	var blocked []Blocker
	for _, org := range orgs {
		members, err := a.reader.MembersOf(ctx, org.ID)
		if err != nil {
			return nil, fmt.Errorf("org: stranding: %w", err)
		}
		var others, owners int
		for _, m := range members {
			if m.AccountID != account {
				others++
			}
			if m.Role == domain.RoleOwner {
				owners++
			}
		}
		mine := false
		for _, m := range members {
			if m.AccountID == account && m.Role == domain.RoleOwner {
				mine = true
			}
		}
		if mine && owners == 1 && others > 0 {
			blocked = append(blocked, Blocker{OrgID: org.ID, Name: org.Name})
		}
	}
	return blocked, nil
}
