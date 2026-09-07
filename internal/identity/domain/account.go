package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	MaxEmailLength = 254
	MaxNameLength  = 120
)

type Email string

func NewEmail(raw string) (Email, error) {
	e := strings.ToLower(strings.TrimSpace(raw))
	if e == "" {
		return "", ErrEmailRequired
	}
	if len(e) > MaxEmailLength {
		return "", ErrEmailTooLong
	}
	local, host, found := strings.Cut(e, "@")
	if !found || local == "" || host == "" || strings.Contains(host, "@") {
		return "", ErrEmailNotAddressable
	}
	if !strings.Contains(host, ".") || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return "", ErrEmailNoDomain
	}
	return Email(e), nil
}

func (e Email) String() string { return string(e) }

type Status uint8

const (
	StatusPending Status = iota
	StatusActive
	StatusArchived
)

func (s Status) String() string {
	switch s {
	case StatusActive:
		return "active"
	case StatusArchived:
		return "archived"
	default:
		return "pending"
	}
}

func ParseStatus(s string) (Status, error) {
	switch s {
	case "pending":
		return StatusPending, nil
	case "active":
		return StatusActive, nil
	case "archived":
		return StatusArchived, nil
	default:
		return StatusPending, ErrStatusUnknown
	}
}

type Account struct {
	ID        id.ID
	Email     Email
	Name      string
	Status    Status
	Version   int
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewAccount(newID id.ID, email Email, name string, at time.Time) (Account, error) {
	if newID.IsZero() {
		return Account{}, ErrIDRequired
	}
	if at.IsZero() {
		return Account{}, ErrTimeRequired
	}
	if email == "" {
		return Account{}, ErrEmailRequired
	}
	name = strings.TrimSpace(name)
	if len(name) > MaxNameLength {
		return Account{}, ErrNameTooLong
	}
	return Account{
		ID:        newID,
		Email:     email,
		Name:      name,
		Status:    StatusPending,
		Version:   1,
		CreatedAt: at,
		UpdatedAt: at,
	}, nil
}

func (a Account) Activate(at time.Time) (Account, error) {
	if at.IsZero() {
		return a, ErrTimeRequired
	}
	switch a.Status {
	case StatusArchived:
		return a, ErrAccountArchived
	case StatusActive:
		return a, ErrAlreadyActive
	}
	return a.advance(at, func(next *Account) { next.Status = StatusActive }), nil
}

func (a Account) Archive(at time.Time) (Account, error) {
	if at.IsZero() {
		return a, ErrTimeRequired
	}
	if a.Status == StatusArchived {
		return a, ErrAccountArchived
	}
	return a.advance(at, func(next *Account) { next.Status = StatusArchived }), nil
}

// ChangeEmail moves the account to an address that has already been proved —
// decisions/0021. The domain does not know how it was proved; what it enforces
// is that an archived account cannot move, because an archived account is
// somebody who left and their address must stop being a way back in.
func (a Account) ChangeEmail(next Email, at time.Time) (Account, error) {
	if at.IsZero() {
		return a, ErrTimeRequired
	}
	if next.String() == "" {
		return a, ErrEmailRequired
	}
	if a.Status == StatusArchived {
		return a, ErrAccountArchived
	}
	if a.Email == next {
		return a, nil
	}
	out := a.advance(at, func(n *Account) { n.Email = next })
	// A confirmed address is a proved address, so a pending account becomes
	// active here for the same reason verification activates one: the only
	// proof an address works is a message arriving at it, and one just did.
	if out.Status == StatusPending {
		out.Status = StatusActive
	}
	return out, nil
}

func (a Account) Rename(name string, at time.Time) (Account, error) {
	if at.IsZero() {
		return a, ErrTimeRequired
	}
	if a.Status == StatusArchived {
		return a, ErrAccountArchived
	}
	name = strings.TrimSpace(name)
	if len(name) > MaxNameLength {
		return a, ErrNameTooLong
	}
	return a.advance(at, func(next *Account) { next.Name = name }), nil
}

func (a Account) CanAuthenticate() bool { return a.Status != StatusArchived }

func (a Account) advance(at time.Time, apply func(*Account)) Account {
	next := a
	apply(&next)
	next.Version = a.Version + 1
	next.UpdatedAt = at
	return next
}
