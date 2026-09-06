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
