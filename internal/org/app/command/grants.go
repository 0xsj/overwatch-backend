package command

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/org/app/query"
	"github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// GrantWriter is the write side of the grant table. It is separate from
// [GrantRepository], which the opener subscriber uses, because that one needs a
// single insert and nothing else — and a port is declared by what uses it.
type GrantWriter interface {
	AddGrant(ctx context.Context, g domain.Grant) error
	GrantFor(ctx context.Context, org, account, workspace id.ID) (domain.Grant, error)
	SaveGrant(ctx context.Context, g domain.Grant) error
	RevokeGrant(ctx context.Context, org, account, workspace id.ID) error
	LiveMemberFor(ctx context.Context, orgID, account id.ID) (domain.Member, error)
}

// Grants writes who may do what on one engagement — decisions/0023.
type Grants struct {
	repo      GrantWriter
	access    *query.Access
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewGrants(repo GrantWriter, access *query.Access, publisher events.Publisher,
	ids Minter, clock Clock) *Grants {
	if repo == nil || access == nil || publisher == nil || ids == nil || clock == nil {
		panic("org: NewGrants with a nil dependency")
	}
	return &Grants{repo: repo, access: access, publisher: publisher, ids: ids, clock: clock}
}

// Set grants or changes a level. One operation, because the screen is a dropdown
// on a grid — splitting it makes the client ask which case applies, and between
// its read and its write somebody else may have granted.
//
// **The caller needs effective `admin` on this workspace**, not an org role.
// An org admin manages people, not work (decisions/0019), so administering the
// firm does not imply putting somebody on Acme Q3.
func (g *Grants) Set(ctx context.Context, caller, org, workspace, account id.ID,
	level domain.Level) (domain.Grant, error) {
	if err := g.mayAdminister(ctx, caller, org, workspace); err != nil {
		return domain.Grant{}, err
	}
	if !level.Visible() {
		// `none` is a revocation and revoking deletes the row — decisions/0019.
		// There is deliberately no way to write it.
		return domain.Grant{}, domain.ErrGrantEmpty
	}
	if err := g.withinCeiling(ctx, org, account, level); err != nil {
		return domain.Grant{}, err
	}

	at := g.clock.Now()
	held, err := g.repo.GrantFor(ctx, org, account, workspace)
	switch {
	case err == nil:
		if held.Level == level {
			// Idempotent, and silent: PUT to the level somebody already has is
			// the client re-sending, not an act worth a ledger row.
			return held, nil
		}
		next, err := held.Retarget(level, at)
		if err != nil {
			return domain.Grant{}, fmt.Errorf("org: set grant: %w", err)
		}
		if err := g.repo.SaveGrant(ctx, next); err != nil {
			return domain.Grant{}, fmt.Errorf("org: set grant: %w", err)
		}
		return next, g.emit(ctx, domain.EventGrantChanged, workspace, domain.GrantChanged{
			OrgID: org.String(), WorkspaceID: workspace.String(),
			AccountID: account.String(),
			From:      held.Level.String(), To: level.String(),
		})

	case errors.Is(err, domain.ErrGrantNotFound):
		fresh, err := domain.NewGrant(g.ids.NewID(), org, account, workspace, level, at)
		if err != nil {
			return domain.Grant{}, fmt.Errorf("org: set grant: %w", err)
		}
		if err := g.repo.AddGrant(ctx, fresh); err != nil {
			return domain.Grant{}, fmt.Errorf("org: set grant: %w", err)
		}
		return fresh, g.emit(ctx, domain.EventGrantGiven, workspace, domain.GrantGiven{
			OrgID: org.String(), WorkspaceID: workspace.String(),
			AccountID: account.String(), To: level.String(),
		})

	default:
		return domain.Grant{}, fmt.Errorf("org: set grant: %w", err)
	}
}

// Revoke deletes the row.
//
// **Somebody may revoke their own access**, and no guard stops them: the org
// owner is exempt and can always restore it, so there is no state anybody can
// strand themselves in — decisions/0023.
func (g *Grants) Revoke(ctx context.Context, caller, org, workspace, account id.ID) error {
	if err := g.mayAdminister(ctx, caller, org, workspace); err != nil {
		return err
	}
	held, err := g.repo.GrantFor(ctx, org, account, workspace)
	if err != nil {
		if errors.Is(err, domain.ErrGrantNotFound) {
			// Already off the engagement, which is the outcome the caller
			// wanted. Reporting it invites a client to retry a no-op.
			return nil
		}
		return fmt.Errorf("org: revoke grant: %w", err)
	}
	if err := g.repo.RevokeGrant(ctx, org, account, workspace); err != nil {
		if errors.Is(err, domain.ErrGrantNotFound) {
			return nil
		}
		return fmt.Errorf("org: revoke grant: %w", err)
	}
	return g.emit(ctx, domain.EventGrantRevoked, workspace, domain.GrantRevoked{
		OrgID: org.String(), WorkspaceID: workspace.String(),
		AccountID: account.String(), From: held.Level.String(),
	})
}

// mayAdminister is the gate, and it answers [query.ErrNoAccess] for a caller who
// may not — NotFound rather than Forbidden, so somebody who cannot see the
// engagement cannot learn it exists by being refused.
func (g *Grants) mayAdminister(ctx context.Context, caller, org, workspace id.ID) error {
	if caller.IsZero() || org.IsZero() || workspace.IsZero() {
		return domain.ErrIDRequired
	}
	reach, err := g.access.In(ctx, caller, org)
	if err != nil {
		return err
	}
	if !reach.Allows(workspace, domain.LevelAdmin) {
		return query.ErrNoAccess
	}
	return nil
}

// withinCeiling refuses a level the target's org role could never hold.
//
// It is the ERGONOMIC half of decisions/0023 and not the safe half: a role can
// be lowered afterwards and the grant survives it, which is why Effective caps
// at read time as well. Refusing here means the grid never displays a level
// somebody does not have; capping there means a demotion is safe.
func (g *Grants) withinCeiling(ctx context.Context, org, account id.ID, level domain.Level) error {
	member, err := g.repo.LiveMemberFor(ctx, org, account)
	if err != nil {
		if errors.Is(err, domain.ErrMemberNotFound) {
			// A grant naming a non-member is unreachable — Access.In refuses
			// them before any grant is read — so it would render as access
			// nobody has.
			return domain.ErrNotAMember
		}
		return fmt.Errorf("org: ceiling: %w", err)
	}
	if level > member.Role.Ceiling() {
		return fmt.Errorf("%s cannot hold %s: %w", member.Role, level, domain.ErrAboveCeiling)
	}
	return nil
}

// emit tenants the event to the workspace, which is what makes audit record it
// with `scope = workspace` and put it on THAT engagement's log.
//
// **An untenanted grant event lands under `scope = system` and appears on no
// screen at all** — decisions/0023 names it as the failure that passes every
// test which counts rows.
func (g *Grants) emit(ctx context.Context, name string, workspace id.ID, payload any) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, g.ids)
	}
	tenanted, err := prov.WithTenant(workspace.String())
	if err != nil {
		return fmt.Errorf("org: %s: %w", name, err)
	}
	// The subject is the WORKSPACE and not the org: the act is "somebody was put
	// on this engagement", and the engagement's log is where it belongs. The
	// literal is here rather than workspace's own constant because org and
	// workspace are peers and the import checks refuse it — the same contract on
	// a name that the granter subscriber already carries.
	e, err := events.NewDecision(g.ids, g.clock, name,
		"workspace:"+workspace.String(), tenanted, payload)
	if err != nil {
		return fmt.Errorf("org: %s: %w", name, err)
	}
	if err := g.publisher.Publish(ctx, e); err != nil {
		return fmt.Errorf("org: %s: %w", name, err)
	}
	return nil
}
