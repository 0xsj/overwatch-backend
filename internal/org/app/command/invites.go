package command

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/org/app/query"
	"github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/crypto"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// InviteRepository is the write side of an invitation, plus the two reads that
// decide whether one may be sent.
type InviteRepository interface {
	CreateInvite(ctx context.Context, i domain.Invite) error
	InviteByHash(ctx context.Context, hash string) (domain.Invite, error)
	LiveInviteFor(ctx context.Context, org id.ID, email string) (domain.Invite, error)
	InvitesForOrg(ctx context.Context, org id.ID) ([]domain.Invite, error)
	SaveInvite(ctx context.Context, i domain.Invite) error

	LiveMemberFor(ctx context.Context, orgID, account id.ID) (domain.Member, error)
	MembersOf(ctx context.Context, orgID id.ID) ([]domain.Member, error)
	AddMember(ctx context.Context, m domain.Member) error
	AddGrant(ctx context.Context, g domain.Grant) error
	OrgByID(ctx context.Context, want id.ID) (domain.Org, error)
}

// Directory is the ONE thing this package needs from identity, declared as a
// port so nothing is imported: given account ids, what are their addresses.
// The composition root satisfies it.
//
// It exists because "is this address already a member" cannot be answered from
// org's tables alone — a member row names an account, not an address.
type Directory interface {
	AddressesOf(ctx context.Context, accounts []id.ID) (map[id.ID]string, error)
}

// Mailer is the narrowest thing this needs from pkg/mail.
type Mailer interface {
	Invite(ctx context.Context, to, org, from, token string) error
}

type Tokens interface {
	New() (crypto.Token, error)
}

// Invites asks somebody to join an org — decisions/0025.
type Invites struct {
	repo      InviteRepository
	people    Directory
	access    *query.Access
	mailer    Mailer
	tokens    Tokens
	publisher events.Publisher
	tx        Transactor
	ids       Minter
	clock     Clock
}

// Transactor is here because accepting writes a member AND a grant, and a member
// without their first grant is the empty-org state decisions/0025 exists to
// avoid.
type Transactor interface {
	InTx(ctx context.Context, fn func(context.Context) error) error
}

func NewInvites(repo InviteRepository, people Directory, access *query.Access,
	mailer Mailer, tokens Tokens, publisher events.Publisher, tx Transactor,
	ids Minter, clock Clock) *Invites {
	if repo == nil || people == nil || access == nil || mailer == nil ||
		tokens == nil || publisher == nil || tx == nil || ids == nil || clock == nil {
		panic("org: NewInvites with a nil dependency")
	}
	return &Invites{repo: repo, people: people, access: access, mailer: mailer,
		tokens: tokens, publisher: publisher, tx: tx, ids: ids, clock: clock}
}

// Invitation is what a caller asks for. WorkspaceID and Level are the optional
// first grant, and they arrive together or not at all.
type Invitation struct {
	Email       string
	Role        domain.Role
	WorkspaceID id.ID
	Level       domain.Level
}

// Send mints an invitation and mails it.
//
// **Inviting needs an ORG role; granting needs a WORKSPACE level.** That
// symmetry is the whole of decisions/0019's split expressed as two commands, and
// it is why this checks a role while [Grants.Set] checks a level.
func (i *Invites) Send(ctx context.Context, caller, org id.ID, in Invitation) (domain.Invite, error) {
	at := i.clock.Now()

	inviter, err := i.repo.LiveMemberFor(ctx, org, caller)
	if err != nil {
		if errors.Is(err, domain.ErrMemberNotFound) {
			return domain.Invite{}, query.ErrNoAccess
		}
		return domain.Invite{}, fmt.Errorf("org: invite: %w", err)
	}
	switch inviter.Role {
	case domain.RoleOwner, domain.RoleAdmin:
	default:
		return domain.Invite{}, query.ErrNoAccess
	}
	// Only an owner may invite an owner. Not a general "no inviting above
	// yourself": the roles are not totally ordered — `client` is deliberately
	// off the ladder — so a comparison would invent an order that does not exist.
	if in.Role == domain.RoleOwner && inviter.Role != domain.RoleOwner {
		return domain.Invite{}, domain.ErrOwnerInvite
	}

	email, err := domain.FoldEmail(in.Email)
	if err != nil {
		return domain.Invite{}, err
	}
	if err := i.notAlreadyIn(ctx, org, email); err != nil {
		return domain.Invite{}, err
	}

	// A first grant needs admin on THAT workspace — the same rule 0023 applies
	// to granting directly, checked here so the refusal reaches the person who
	// can act on it rather than the invitee a week later.
	if !in.WorkspaceID.IsZero() {
		reach, err := i.access.In(ctx, caller, org)
		if err != nil {
			return domain.Invite{}, err
		}
		if !reach.Allows(in.WorkspaceID, domain.LevelAdmin) {
			return domain.Invite{}, query.ErrNoAccess
		}
		if in.Level > in.Role.Ceiling() {
			return domain.Invite{}, fmt.Errorf("%s cannot hold %s: %w",
				in.Role, in.Level, domain.ErrAboveCeiling)
		}
	}

	// One live invitation per address per org. Sending a second consumes the
	// first, for the same reason a verification link consumes the outstanding
	// one: two live links means the older still works after somebody asked for
	// a newer.
	if held, err := i.repo.LiveInviteFor(ctx, org, email); err == nil {
		withdrawn, err := held.Revoke(at)
		if err != nil {
			return domain.Invite{}, fmt.Errorf("org: invite: %w", err)
		}
		if err := i.repo.SaveInvite(ctx, withdrawn); err != nil {
			return domain.Invite{}, fmt.Errorf("org: invite: %w", err)
		}
	} else if !errors.Is(err, domain.ErrInviteGone) {
		return domain.Invite{}, fmt.Errorf("org: invite: %w", err)
	}

	minted, err := i.tokens.New()
	if err != nil {
		return domain.Invite{}, fmt.Errorf("org: invite: %w", err)
	}
	fresh, err := domain.NewInvite(i.ids.NewID(), org, caller, email, in.Role, minted.Hash, at)
	if err != nil {
		return domain.Invite{}, fmt.Errorf("org: invite: %w", err)
	}
	if !in.WorkspaceID.IsZero() {
		if fresh, err = fresh.WithGrant(in.WorkspaceID, in.Level); err != nil {
			return domain.Invite{}, fmt.Errorf("org: invite: %w", err)
		}
	}
	if err := i.repo.CreateInvite(ctx, fresh); err != nil {
		return domain.Invite{}, err
	}

	// The mail goes LAST. A link for an invitation nobody stored cannot work,
	// and the person clicking it has no way to find out why.
	firm, err := i.repo.OrgByID(ctx, org)
	if err != nil {
		return domain.Invite{}, fmt.Errorf("org: invite: %w", err)
	}
	from, err := i.people.AddressesOf(ctx, []id.ID{caller})
	if err != nil {
		return domain.Invite{}, fmt.Errorf("org: invite: %w", err)
	}
	if err := i.mailer.Invite(ctx, email, firm.Name, from[caller], minted.Plaintext.Reveal()); err != nil {
		return domain.Invite{}, fmt.Errorf("org: invite: %w", err)
	}

	return fresh, i.emit(ctx, domain.EventInviteSent, org, domain.InviteSent{
		OrgID: org.String(), InviteID: fresh.ID.String(),
		Email: email, Role: in.Role.String(), InvitedBy: caller.String(),
	})
}

// Accept binds an invitation to the account that took it, and writes the member
// and the first grant IN ONE TRANSACTION — decisions/0025. Two transactions
// would leave a window in which the new member is in an org they can see nothing
// in, which is the state this whole design avoids.
//
// The caller's address is supplied rather than read: org cannot see identity's
// tables, and the root already holds it from authenticating them.
func (i *Invites) Accept(ctx context.Context, caller id.ID, callerEmail, presented string) (domain.Invite, error) {
	at := i.clock.Now()
	if presented == "" {
		return domain.Invite{}, domain.ErrInviteGone
	}
	held, err := i.repo.InviteByHash(ctx, crypto.HashToken(presented))
	if err != nil {
		return domain.Invite{}, err
	}
	taken, err := held.Accept(caller, callerEmail, at)
	if err != nil {
		return domain.Invite{}, err
	}

	var member domain.Member
	err = i.tx.InTx(ctx, func(ctx context.Context) error {
		if err := i.repo.SaveInvite(ctx, taken); err != nil {
			return err
		}
		member, err = domain.NewMember(i.ids.NewID(), held.OrgID, caller, held.Role, at)
		if err != nil {
			return err
		}
		if err := i.repo.AddMember(ctx, member); err != nil {
			return err
		}
		if !held.HasGrant() {
			return nil
		}
		grant, err := domain.NewGrant(i.ids.NewID(), held.OrgID, caller,
			held.WorkspaceID, held.Level, at)
		if err != nil {
			return err
		}
		return i.repo.AddGrant(ctx, grant)
	})
	if err != nil {
		return domain.Invite{}, fmt.Errorf("org: accept invite: %w", err)
	}

	if err := i.emit(ctx, domain.EventInviteAccepted, held.OrgID, domain.InviteAccepted{
		OrgID: held.OrgID.String(), InviteID: held.ID.String(),
		AccountID: caller.String(), Role: held.Role.String(),
	}); err != nil {
		return domain.Invite{}, err
	}
	// The membership is WORK — a consequence nobody chose, once the invitation
	// was accepted. decisions/0014.
	if err := i.work(ctx, domain.EventMemberAdded, held.OrgID, domain.MemberAdded{
		OrgID: held.OrgID.String(), MemberID: member.ID.String(),
		AccountID: caller.String(), Role: held.Role.String(),
	}); err != nil {
		return domain.Invite{}, err
	}
	if held.HasGrant() {
		// Tenanted, so it lands on the ENGAGEMENT's log rather than the firm's —
		// one act, two ledgers, joined by correlation.
		if err := i.grantGiven(ctx, held, caller); err != nil {
			return domain.Invite{}, err
		}
	}
	return taken, nil
}

func (i *Invites) Revoke(ctx context.Context, caller, org, invite id.ID) error {
	at := i.clock.Now()
	member, err := i.repo.LiveMemberFor(ctx, org, caller)
	if err != nil {
		return query.ErrNoAccess
	}
	switch member.Role {
	case domain.RoleOwner, domain.RoleAdmin:
	default:
		return query.ErrNoAccess
	}
	held, err := i.repo.InvitesForOrg(ctx, org)
	if err != nil {
		return fmt.Errorf("org: revoke invite: %w", err)
	}
	for _, in := range held {
		if in.ID != invite {
			continue
		}
		withdrawn, err := in.Revoke(at)
		if err != nil {
			return err
		}
		if err := i.repo.SaveInvite(ctx, withdrawn); err != nil {
			return fmt.Errorf("org: revoke invite: %w", err)
		}
		return i.emit(ctx, domain.EventInviteRevoked, org, domain.InviteRevoked{
			OrgID: org.String(), InviteID: invite.String(), Email: in.Email,
		})
	}
	return domain.ErrInviteGone
}

func (i *Invites) notAlreadyIn(ctx context.Context, org id.ID, email string) error {
	members, err := i.repo.MembersOf(ctx, org)
	if err != nil {
		return fmt.Errorf("org: invite: %w", err)
	}
	accounts := make([]id.ID, 0, len(members))
	for _, m := range members {
		accounts = append(accounts, m.AccountID)
	}
	addresses, err := i.people.AddressesOf(ctx, accounts)
	if err != nil {
		return fmt.Errorf("org: invite: %w", err)
	}
	for _, held := range addresses {
		if folded, err := domain.FoldEmail(held); err == nil && folded == email {
			// Re-inviting somebody already in the firm is a mistake worth
			// naming rather than a silent no-op — decisions/0025.
			return domain.ErrAlreadyMember
		}
	}
	return nil
}

// emit publishes an org-scoped DECISION. The subject is the org, which is what
// decisions/0024 reads to give the entry `scope = org` — no tenant, and
// therefore no workspace id, which is what makes the firm's log safe to read.
func (i *Invites) emit(ctx context.Context, name string, org id.ID, payload any) error {
	return i.publish(ctx, name, org, payload, true, id.ID{})
}

func (i *Invites) work(ctx context.Context, name string, org id.ID, payload any) error {
	return i.publish(ctx, name, org, payload, false, id.ID{})
}

func (i *Invites) grantGiven(ctx context.Context, held domain.Invite, account id.ID) error {
	return i.publish(ctx, domain.EventGrantGiven, held.OrgID, domain.GrantGiven{
		OrgID: held.OrgID.String(), WorkspaceID: held.WorkspaceID.String(),
		AccountID: account.String(), To: held.Level.String(),
	}, true, held.WorkspaceID)
}

func (i *Invites) publish(ctx context.Context, name string, org id.ID, payload any,
	decision bool, tenant id.ID) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, i.ids)
	}
	subject := domain.SubjectKind + ":" + org.String()
	if !tenant.IsZero() {
		var err error
		if prov, err = prov.WithTenant(tenant.String()); err != nil {
			return fmt.Errorf("org: %s: %w", name, err)
		}
		subject = "workspace:" + tenant.String()
	}
	var (
		e   events.Event
		err error
	)
	if decision {
		e, err = events.NewDecision(i.ids, i.clock, name, subject, prov, payload)
	} else {
		e, err = events.New(i.ids, i.clock, name, subject, prov, payload)
	}
	if err != nil {
		return fmt.Errorf("org: %s: %w", name, err)
	}
	if err := i.publisher.Publish(ctx, e); err != nil {
		return fmt.Errorf("org: %s: %w", name, err)
	}
	return nil
}
