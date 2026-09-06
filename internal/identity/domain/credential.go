package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const MaxCredentialNameLength = 60

type Kind uint8

const (
	KindPassword Kind = iota
	KindAPIKey
)

func (k Kind) String() string {
	if k == KindAPIKey {
		return "api_key"
	}
	return "password"
}

func ParseKind(s string) (Kind, error) {
	switch s {
	case "password":
		return KindPassword, nil
	case "api_key":
		return KindAPIKey, nil
	default:
		return KindPassword, ErrKindUnknown
	}
}

type Credential struct {
	ID        id.ID
	AccountID id.ID
	Kind      Kind
	Hash      string
	Name      string
	ExpiresAt time.Time
	RevokedAt time.Time
	Version   int
	CreatedAt time.Time
	UpdatedAt time.Time
}

func NewPassword(newID, accountID id.ID, hash string, at time.Time) (Credential, error) {
	return newCredential(newID, accountID, KindPassword, hash, "", time.Time{}, at)
}

func NewAPIKey(newID, accountID id.ID, hash, name string, expires time.Time, at time.Time) (Credential, error) {
	return newCredential(newID, accountID, KindAPIKey, hash, name, expires, at)
}

func newCredential(newID, accountID id.ID, kind Kind, hash, name string, expires, at time.Time) (Credential, error) {
	if newID.IsZero() || accountID.IsZero() {
		return Credential{}, ErrIDRequired
	}
	if at.IsZero() {
		return Credential{}, ErrTimeRequired
	}
	if hash == "" {
		return Credential{}, ErrHashRequired
	}
	name = strings.TrimSpace(name)
	if kind == KindAPIKey && name == "" {
		return Credential{}, ErrKeyNeedsName
	}
	if len(name) > MaxCredentialNameLength {
		return Credential{}, ErrNameTooLong
	}
	if !expires.IsZero() && !expires.After(at) {
		return Credential{}, ErrExpiryInThePast
	}
	return Credential{
		ID:        newID,
		AccountID: accountID,
		Kind:      kind,
		Hash:      hash,
		Name:      name,
		ExpiresAt: expires,
		Version:   1,
		CreatedAt: at,
		UpdatedAt: at,
	}, nil
}

func (c Credential) Live(at time.Time) bool {
	if !c.RevokedAt.IsZero() {
		return false
	}
	if c.ExpiresAt.IsZero() {
		return true
	}
	return c.ExpiresAt.After(at)
}

func (c Credential) Revoked() bool { return !c.RevokedAt.IsZero() }

func (c Credential) Expired(at time.Time) bool {
	return !c.ExpiresAt.IsZero() && !c.ExpiresAt.After(at)
}

func (c Credential) Revoke(at time.Time) (Credential, error) {
	if at.IsZero() {
		return c, ErrTimeRequired
	}
	if c.Revoked() {
		return c, ErrAlreadyRevoked
	}
	next := c
	next.RevokedAt = at
	next.UpdatedAt = at
	next.Version = c.Version + 1
	return next, nil
}

func (c Credential) Rehash(hash string, at time.Time) (Credential, error) {
	if at.IsZero() {
		return c, ErrTimeRequired
	}
	if hash == "" {
		return c, ErrHashRequired
	}
	if c.Revoked() {
		return c, ErrAlreadyRevoked
	}
	next := c
	next.Hash = hash
	next.UpdatedAt = at
	next.Version = c.Version + 1
	return next, nil
}
