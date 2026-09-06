package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	MaxUserAgentLength = 512
	MaxAddressLength   = 45
)

type Session struct {
	ID        id.ID
	AccountID id.ID
	Hash      string
	UserAgent string
	Address   string
	IssuedAt  time.Time
	ExpiresAt time.Time
	RevokedAt time.Time
}

func NewSession(newID, accountID id.ID, hash, userAgent, address string, at, expires time.Time) (Session, error) {
	if newID.IsZero() || accountID.IsZero() {
		return Session{}, ErrIDRequired
	}
	if at.IsZero() || expires.IsZero() {
		return Session{}, ErrTimeRequired
	}
	if hash == "" {
		return Session{}, ErrHashRequired
	}
	if !expires.After(at) {
		return Session{}, ErrExpiryInThePast
	}
	return Session{
		ID:        newID,
		AccountID: accountID,
		Hash:      hash,
		UserAgent: truncate(strings.TrimSpace(userAgent), MaxUserAgentLength),
		Address:   truncate(strings.TrimSpace(address), MaxAddressLength),
		IssuedAt:  at,
		ExpiresAt: expires,
	}, nil
}

func (s Session) Live(at time.Time) bool {
	return s.RevokedAt.IsZero() && s.ExpiresAt.After(at)
}

func (s Session) Revoked() bool { return !s.RevokedAt.IsZero() }

func (s Session) Expired(at time.Time) bool { return !s.ExpiresAt.After(at) }

func (s Session) Revoke(at time.Time) (Session, error) {
	if at.IsZero() {
		return s, ErrTimeRequired
	}
	if s.Revoked() {
		return s, ErrAlreadyRevoked
	}
	next := s
	next.RevokedAt = at
	return next, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
