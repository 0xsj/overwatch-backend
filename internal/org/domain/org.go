package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const MaxNameLength = 120

type Org struct {
	ID        id.ID
	Name      string
	Version   int
	CreatedAt time.Time
	UpdatedAt time.Time

	// SourceEvent is the event this org was provisioned from, and is zero for
	// one a person created. It is the idempotency key decisions/0017 needs: a
	// redelivered account.created must not produce a second org.
	SourceEvent id.ID
}

func NewOrg(newID id.ID, name string, at time.Time) (Org, error) {
	if newID.IsZero() {
		return Org{}, ErrIDRequired
	}
	if at.IsZero() {
		return Org{}, ErrTimeRequired
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Org{}, ErrNameRequired
	}
	if len(name) > MaxNameLength {
		return Org{}, ErrNameTooLong
	}
	return Org{ID: newID, Name: name, Version: 1, CreatedAt: at, UpdatedAt: at}, nil
}

func (o Org) Rename(name string, at time.Time) (Org, error) {
	if at.IsZero() {
		return o, ErrTimeRequired
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return o, ErrNameRequired
	}
	if len(name) > MaxNameLength {
		return o, ErrNameTooLong
	}
	next := o
	next.Name = name
	next.Version = o.Version + 1
	next.UpdatedAt = at
	return next, nil
}

const (
	EventOrgCreated     = "org.created"
	EventGrantGiven     = "org.grant.given"
	EventGrantChanged   = "org.grant.changed"
	EventGrantRevoked   = "org.grant.revoked"
	EventInviteSent     = "org.invite.sent"
	EventInviteAccepted = "org.invite.accepted"
	EventInviteRevoked  = "org.invite.revoked"
	EventRoleChanged    = "org.member.role_changed"
	EventMemberRemoved  = "org.member.removed"
	EventMemberLeft     = "org.member.left"
	EventOrgRenamed     = "org.renamed"
	EventMemberAdded    = "org.member.added"
	EventMemberArchived = "org.member.archived"

	SubjectKind = "org"
)

type Created struct {
	OrgID   string `json:"org_id"`
	OwnerID string `json:"owner_id"`
}

type MemberAdded struct {
	OrgID     string `json:"org_id"`
	MemberID  string `json:"member_id"`
	AccountID string `json:"account_id"`
	Role      string `json:"role"`
}

// GrantGiven, GrantChanged and GrantRevoked are three names rather than one
// `grant.set` with a nullable field — decisions/0023. A revocation is not a
// change to nothing: it is what somebody does when a person leaves an
// engagement, and collapsing them makes "who was removed from Acme Q3" a query
// with a null test in it.
type GrantGiven struct {
	OrgID       string `json:"org_id"`
	WorkspaceID string `json:"workspace_id"`
	AccountID   string `json:"account_id"`
	To          string `json:"to"`
}

// GrantChanged carries FROM and TO. The mock draws that pair as first-class, and
// a ledger holding only the new value cannot answer "who widened this" — the
// question asked after an engagement rather than during it.
type GrantChanged struct {
	OrgID       string `json:"org_id"`
	WorkspaceID string `json:"workspace_id"`
	AccountID   string `json:"account_id"`
	From        string `json:"from"`
	To          string `json:"to"`
}

type GrantRevoked struct {
	OrgID       string `json:"org_id"`
	WorkspaceID string `json:"workspace_id"`
	AccountID   string `json:"account_id"`
	From        string `json:"from"`
}

type InviteSent struct {
	OrgID     string `json:"org_id"`
	InviteID  string `json:"invite_id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	InvitedBy string `json:"invited_by"`
}

type InviteAccepted struct {
	OrgID     string `json:"org_id"`
	InviteID  string `json:"invite_id"`
	AccountID string `json:"account_id"`
	Role      string `json:"role"`
}

type InviteRevoked struct {
	OrgID    string `json:"org_id"`
	InviteID string `json:"invite_id"`
	Email    string `json:"email"`
}

type RoleChanged struct {
	OrgID     string `json:"org_id"`
	AccountID string `json:"account_id"`
	From      string `json:"from"`
	To        string `json:"to"`
}

// MemberRemoved and MemberLeft are two names for the same row change —
// decisions/0026. "Did they leave or were they removed" is the first question
// anybody asks about a departure, and one event with an actor comparison makes
// it a query rather than a fact.
type MemberRemoved struct {
	OrgID     string `json:"org_id"`
	AccountID string `json:"account_id"`
	Role      string `json:"role"`
	Grants    int    `json:"grants_revoked"`
}

type MemberLeft struct {
	OrgID     string `json:"org_id"`
	AccountID string `json:"account_id"`
	Role      string `json:"role"`
	Grants    int    `json:"grants_revoked"`
}

type OrgRenamed struct {
	OrgID string `json:"org_id"`
	From  string `json:"from"`
	To    string `json:"to"`
}
