package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

// InviteTTL is seven days — decisions/0025. Long for a token, and short for a
// person who has to notice an email among the rest of their week.
const InviteTTL = 7 * 24 * time.Hour

// Invite is somebody being asked to join an org.
//
// **Email is the address the inviter TYPED**, not a copy of an account's — the
// invitee may have no account at all. Accepting compares it to the caller's
// address, which is what makes an invitation address-bound rather than a bearer
// credential a forwarded mail could spend.
type Invite struct {
	ID        id.ID
	OrgID     id.ID
	Email     string
	Role      Role
	InvitedBy id.ID

	// WorkspaceID and Level are the optional first grant, and they are set
	// together or not at all. An invitation with neither drops somebody into an
	// org where they can see nothing.
	WorkspaceID id.ID
	Level       Level

	Hash       string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	AcceptedAt time.Time
	AcceptedBy id.ID
	RevokedAt  time.Time
}

func NewInvite(newID, org, invitedBy id.ID, email string, role Role, hash string, at time.Time) (Invite, error) {
	if newID.IsZero() || org.IsZero() || invitedBy.IsZero() {
		return Invite{}, ErrIDRequired
	}
	if at.IsZero() {
		return Invite{}, ErrTimeRequired
	}
	if hash == "" {
		return Invite{}, ErrHashRequired
	}
	folded, err := FoldEmail(email)
	if err != nil {
		return Invite{}, err
	}
	return Invite{
		ID: newID, OrgID: org, Email: folded, Role: role, InvitedBy: invitedBy,
		Hash: hash, CreatedAt: at, ExpiresAt: at.Add(InviteTTL),
	}, nil
}

// WithGrant attaches the first engagement. It is a separate step from NewInvite
// so that "an invitation with no grant" stays expressible — it is the correct
// shape when somebody is being added to the firm before there is work for them.
func (i Invite) WithGrant(workspace id.ID, level Level) (Invite, error) {
	if workspace.IsZero() {
		return i, ErrIDRequired
	}
	if !level.Visible() {
		return i, ErrGrantEmpty
	}
	next := i
	next.WorkspaceID = workspace
	next.Level = level
	return next, nil
}

func (i Invite) HasGrant() bool { return !i.WorkspaceID.IsZero() }

func (i Invite) Accepted() bool { return !i.AcceptedAt.IsZero() }
func (i Invite) Revoked() bool  { return !i.RevokedAt.IsZero() }

// Live is the predicate, evaluated against a supplied instant rather than
// time.Now — every interesting case here is a boundary.
func (i Invite) Live(at time.Time) bool {
	return !i.Accepted() && !i.Revoked() && i.ExpiresAt.After(at)
}

// Accept binds the invitation to the account that took it.
//
// **It refuses an address that is not the one invited.** That single comparison
// is what separates this from a bearer credential, and it happens here rather
// than in the command so that no caller can forget it.
func (i Invite) Accept(by id.ID, email string, at time.Time) (Invite, error) {
	if by.IsZero() {
		return i, ErrIDRequired
	}
	if at.IsZero() {
		return i, ErrTimeRequired
	}
	if !i.Live(at) {
		return i, ErrInviteGone
	}
	folded, err := FoldEmail(email)
	if err != nil {
		return i, err
	}
	if folded != i.Email {
		return i, ErrInviteAddress
	}
	next := i
	next.AcceptedAt = at
	next.AcceptedBy = by
	return next, nil
}

func (i Invite) Revoke(at time.Time) (Invite, error) {
	if at.IsZero() {
		return i, ErrTimeRequired
	}
	if i.Accepted() {
		// A taken invitation is history. Withdrawing it would be pretending
		// somebody did not join; removing them is a different command.
		return i, ErrInviteTaken
	}
	if i.Revoked() {
		return i, ErrInviteGone
	}
	next := i
	next.RevokedAt = at
	return next, nil
}

// FoldEmail normalises the one way this package compares addresses. It is
// deliberately not a validator: identity owns what a deliverable address looks
// like, and org only needs two of them to compare equal when they are the same
// person — see internal/org/doc.go.
func FoldEmail(s string) (string, error) {
	folded := strings.ToLower(strings.TrimSpace(s))
	if folded == "" {
		return "", ErrEmailRequired
	}
	if len(folded) > 254 {
		return "", ErrEmailTooLong
	}
	return folded, nil
}
