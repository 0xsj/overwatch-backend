package domain

import (
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Role uint8

const RoleOwner Role = iota

func (r Role) String() string { return "owner" }

func ParseRole(s string) (Role, error) {
	if s == "owner" {
		return RoleOwner, nil
	}
	return RoleOwner, ErrRoleUnknown
}

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
