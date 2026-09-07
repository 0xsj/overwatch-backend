package query

import (
	"context"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Reader is the read side, and it is not the write side's Repository. The two
// overlap on nothing: provisioning writes an org and a member, and this reads a
// join neither of those statements touches.
type Reader interface {
	OrgsForAccount(ctx context.Context, account id.ID) ([]domain.Org, error)
	LiveMemberFor(ctx context.Context, orgID, account id.ID) (domain.Member, error)
	MembersOf(ctx context.Context, orgID id.ID) ([]domain.Member, error)
}

// Membership is a view. An org id and a name would be the org; the role is what
// makes this the answer to "where do I belong", and it comes from a different
// table than the rest of the row.
type Membership struct {
	OrgID id.ID
	Name  string
	Role  domain.Role
}

type Orgs struct{ reader Reader }

func NewOrgs(reader Reader) *Orgs {
	if reader == nil {
		panic("org: NewOrgs with a nil reader")
	}
	return &Orgs{reader: reader}
}

// For answers with every org this account is a live member of, oldest first.
// An account with none gets an EMPTY SLICE and no error: "you belong nowhere" is
// an answer, and a NotFound would make the caller distinguish it from a broken
// query.
func (o *Orgs) For(ctx context.Context, account id.ID) ([]Membership, error) {
	if account.IsZero() {
		return nil, domain.ErrIDRequired
	}
	orgs, err := o.reader.OrgsForAccount(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("org: memberships: %w", err)
	}

	out := make([]Membership, 0, len(orgs))
	for _, org := range orgs {
		member, err := o.reader.LiveMemberFor(ctx, org.ID, account)
		if err != nil {
			// The membership is what put this org in the list, so its
			// disappearance between the two reads means somebody was archived
			// mid-answer. Drop the row rather than fail the request: the caller
			// asked where they can go, and the correct answer is "not there".
			if errors.Is(err, domain.ErrMemberNotFound) {
				continue
			}
			return nil, fmt.Errorf("org: memberships: %w", err)
		}
		out = append(out, Membership{OrgID: org.ID, Name: org.Name, Role: member.Role})
	}
	return out, nil
}

// Seat is one person's standing in an org, as the members screen needs it.
//
// **It carries an account id and no email or name.** Those are identity's, and
// org cannot see identity's tables — decisions/0017. The composition root joins
// the two, which is the same shape /v1/me already has and the reason neither
// domain has to learn the other's vocabulary.
type Seat struct {
	AccountID id.ID
	Role      domain.Role
	Status    domain.Status
	JoinedAt  time.Time
}

// Members answers with the live members of one org, oldest first.
//
// **It does not check that the caller belongs to the org.** This package cannot
// see who is asking, and a query that silently authorises is the shape of an
// access bug — it looks safe at the call site and is wrong at every other one.
// The caller resolves its Reach first; see [Access.In] and root's members
// handler.
//
// Archived members are excluded here rather than by the caller. A screen that
// must not show them cannot be relied on to remember, and anybody who wants the
// history asks a different question.
func (o *Orgs) Members(ctx context.Context, org id.ID) ([]Seat, error) {
	if org.IsZero() {
		return nil, domain.ErrIDRequired
	}
	found, err := o.reader.MembersOf(ctx, org)
	if err != nil {
		return nil, fmt.Errorf("org: members: %w", err)
	}
	out := make([]Seat, 0, len(found))
	for _, m := range found {
		out = append(out, Seat{
			AccountID: m.AccountID,
			Role:      m.Role,
			Status:    m.Status,
			JoinedAt:  m.CreatedAt,
		})
	}
	return out, nil
}
