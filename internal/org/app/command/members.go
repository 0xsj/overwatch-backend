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

// MemberRepository is the write side of membership.
//
// **LockLiveOwners must be called inside a transaction.** Outside one the lock is
// released the instant the statement returns and the check becomes the racy
// count it was written to replace — decisions/0026.
type MemberRepository interface {
	LiveMemberFor(ctx context.Context, orgID, account id.ID) (domain.Member, error)
	SaveMember(ctx context.Context, m domain.Member) error
	LockLiveOwners(ctx context.Context, org id.ID) ([]id.ID, error)
	RevokeGrantsFor(ctx context.Context, org, account id.ID) (int, error)

	MembersOf(ctx context.Context, orgID id.ID) ([]domain.Member, error)

	OrgByID(ctx context.Context, want id.ID) (domain.Org, error)
	SaveOrg(ctx context.Context, o domain.Org) error
}

// Members changes who is in a firm and what they are — decisions/0026.
type Members struct {
	repo      MemberRepository
	publisher events.Publisher
	tx        Transactor
	ids       Minter
	clock     Clock
}

func NewMembers(repo MemberRepository, publisher events.Publisher, tx Transactor,
	ids Minter, clock Clock) *Members {
	if repo == nil || publisher == nil || tx == nil || ids == nil || clock == nil {
		panic("org: NewMembers with a nil dependency")
	}
	return &Members{repo: repo, publisher: publisher, tx: tx, ids: ids, clock: clock}
}

// ChangeRole promotes or demotes.
//
// **Transferring ownership is this command twice** — decisions/0026. Promote
// them, then demote yourself. The other order is refused by the last-owner
// guard, which is correct: that order has a moment with no owner in it.
func (m *Members) ChangeRole(ctx context.Context, caller, org, account id.ID, role domain.Role) error {
	at := m.clock.Now()

	actor, target, err := m.pair(ctx, caller, org, account)
	if err != nil {
		return err
	}
	// Only an owner may MAKE or UNMAKE an owner — the same rule 0025 applies to
	// inviting one. An administrator who can demote the owner can take the firm.
	if (role == domain.RoleOwner || target.Role == domain.RoleOwner) &&
		actor.Role != domain.RoleOwner {
		return domain.ErrOwnerRole
	}
	if target.Role == role {
		return nil
	}

	err = m.tx.InTx(ctx, func(ctx context.Context) error {
		if target.Role == domain.RoleOwner {
			if err := m.keepAnOwner(ctx, org, account); err != nil {
				return err
			}
		}
		next, err := target.ChangeRole(role, at)
		if err != nil {
			return err
		}
		return m.repo.SaveMember(ctx, next)
	})
	if err != nil {
		return err
	}
	return m.emit(ctx, domain.EventRoleChanged, org, domain.RoleChanged{
		OrgID: org.String(), AccountID: account.String(),
		From: target.Role.String(), To: role.String(),
	})
}

// Remove archives a membership and DELETES every grant it held.
//
// **A departure is not a demotion** — decisions/0026. A demotion caps grants and
// keeps them, so restoring a role restores access; a removed member who rejoins
// a year later must start from nothing rather than silently regain a client
// engagement nobody reconsidered.
//
// Removing YOURSELF is leaving, and is answered by the same code with a
// different event and one extra refusal.
func (m *Members) Remove(ctx context.Context, caller, org, account id.ID) error {
	at := m.clock.Now()
	leaving := caller == account

	actor, target, err := m.pair(ctx, caller, org, account)
	if err != nil {
		return err
	}
	if !leaving && target.Role == domain.RoleOwner && actor.Role != domain.RoleOwner {
		return domain.ErrOwnerRole
	}
	if !leaving {
		switch actor.Role {
		case domain.RoleOwner, domain.RoleAdmin:
		default:
			return query.ErrNoAccess
		}
	}

	var revoked int
	err = m.tx.InTx(ctx, func(ctx context.Context) error {
		if target.Role == domain.RoleOwner {
			if err := m.keepAnOwner(ctx, org, account); err != nil {
				return err
			}
		}
		gone, err := target.Archive(at)
		if err != nil {
			return err
		}
		if err := m.repo.SaveMember(ctx, gone); err != nil {
			return err
		}
		revoked, err = m.repo.RevokeGrantsFor(ctx, org, account)
		return err
	})
	if err != nil {
		return err
	}

	if leaving {
		return m.emit(ctx, domain.EventMemberLeft, org, domain.MemberLeft{
			OrgID: org.String(), AccountID: account.String(),
			Role: target.Role.String(), Grants: revoked,
		})
	}
	return m.emit(ctx, domain.EventMemberRemoved, org, domain.MemberRemoved{
		OrgID: org.String(), AccountID: account.String(),
		Role: target.Role.String(), Grants: revoked,
	})
}

func (m *Members) Rename(ctx context.Context, caller, org id.ID, name string) (domain.Org, error) {
	actor, err := m.repo.LiveMemberFor(ctx, org, caller)
	if err != nil {
		return domain.Org{}, query.ErrNoAccess
	}
	switch actor.Role {
	case domain.RoleOwner, domain.RoleAdmin:
	default:
		return domain.Org{}, query.ErrNoAccess
	}
	held, err := m.repo.OrgByID(ctx, org)
	if err != nil {
		return domain.Org{}, fmt.Errorf("org: rename: %w", err)
	}
	next, err := held.Rename(name, m.clock.Now())
	if err != nil {
		return domain.Org{}, err
	}
	if next.Name == held.Name {
		return held, nil
	}
	if err := m.repo.SaveOrg(ctx, next); err != nil {
		return domain.Org{}, fmt.Errorf("org: rename: %w", err)
	}
	return next, m.emit(ctx, domain.EventOrgRenamed, org, domain.OrgRenamed{
		OrgID: org.String(), From: held.Name, To: next.Name,
	})
}

// keepAnOwner is the invariant, and it works only because the rows it counts are
// LOCKED until this transaction commits — decisions/0026. An unlocked count is
// true when it runs and false when it matters: two administrators demoting two
// different owners both read two, both writes are individually valid, and the
// org is left with none.
func (m *Members) keepAnOwner(ctx context.Context, org, leaving id.ID) error {
	owners, err := m.repo.LockLiveOwners(ctx, org)
	if err != nil {
		return fmt.Errorf("org: owners: %w", err)
	}
	remaining := 0
	for _, owner := range owners {
		if owner != leaving {
			remaining++
		}
	}
	if remaining == 0 {
		return domain.ErrLastOwner
	}
	return nil
}

// pair reads the actor and the target, and refuses a caller who is not a live
// member with the same NotFound everything else here answers.
func (m *Members) pair(ctx context.Context, caller, org, account id.ID) (actor, target domain.Member, err error) {
	if caller.IsZero() || org.IsZero() || account.IsZero() {
		return actor, target, domain.ErrIDRequired
	}
	actor, err = m.repo.LiveMemberFor(ctx, org, caller)
	if err != nil {
		return actor, target, query.ErrNoAccess
	}
	if caller != account {
		switch actor.Role {
		case domain.RoleOwner, domain.RoleAdmin:
		default:
			return actor, target, query.ErrNoAccess
		}
	}
	target, err = m.repo.LiveMemberFor(ctx, org, account)
	if err != nil {
		if errors.Is(err, domain.ErrMemberNotFound) {
			return actor, target, domain.ErrMemberNotFound
		}
		return actor, target, fmt.Errorf("org: member: %w", err)
	}
	return actor, target, nil
}

// emit publishes an org-scoped decision — the subject is the org, which is what
// decisions/0024 reads to keep it off every engagement's log.
func (m *Members) emit(ctx context.Context, name string, org id.ID, payload any) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, m.ids)
	}
	e, err := events.NewDecision(m.ids, m.clock, name,
		domain.SubjectKind+":"+org.String(), prov, payload)
	if err != nil {
		return fmt.Errorf("org: %s: %w", name, err)
	}
	if err := m.publisher.Publish(ctx, e); err != nil {
		return fmt.Errorf("org: %s: %w", name, err)
	}
	return nil
}
