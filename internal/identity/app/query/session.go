package query

import (
	"context"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/identity/domain"
	"github.com/0xsj/overwatch-backend/pkg/crypto"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// ErrNoSession is the one answer to a token that is unknown, revoked or expired.
// Which of the three it was is not the caller's business, and telling them turns
// the endpoint into an oracle for guessing tokens.
var ErrNoSession = errors.New(errors.Unauthenticated, "not signed in")

// Reader is the READ side and shares nothing with the write ports next door.
// Authenticating a request is the hottest path in the system and needs neither a
// transaction, a publisher nor a hasher.
type Reader interface {
	SessionByHash(ctx context.Context, hash string) (domain.Session, error)
	AccountByID(ctx context.Context, account id.ID) (domain.Account, error)
}

type Clock interface {
	Now() time.Time
}

// Caller is a view, not an aggregate: what a middleware needs to name the actor
// and nothing more.
type Caller struct {
	AccountID id.ID
	Email     domain.Email
	Status    domain.Status
	SessionID id.ID
	ExpiresAt time.Time
}

// Verified reports whether this caller may ACT, as distinct from whether they
// are who they say — decisions/0018. A pending caller is authenticated and is
// refused at the capability gate, not here.
func (c Caller) Verified() bool { return c.Status == domain.StatusActive }

type Sessions struct {
	reader Reader
	clock  Clock
}

func NewSessions(reader Reader, clock Clock) *Sessions {
	if reader == nil || clock == nil {
		panic("identity: NewSessions with a nil dependency")
	}
	return &Sessions{reader: reader, clock: clock}
}

// Authenticate turns a presented token into a caller. It is a lookup by the
// token's HASH — a fixed-width value — so there is no scan and nothing to
// compare byte by byte.
func (s *Sessions) Authenticate(ctx context.Context, presented string) (Caller, error) {
	if presented == "" {
		return Caller{}, ErrNoSession
	}
	session, err := s.reader.SessionByHash(ctx, crypto.HashToken(presented))
	if err != nil {
		if errors.Is(err, domain.ErrSessionGone) {
			return Caller{}, ErrNoSession
		}
		return Caller{}, fmt.Errorf("identity: authenticate: %w", err)
	}
	now := s.clock.Now()
	if !session.Live(now) {
		return Caller{}, ErrNoSession
	}

	account, err := s.reader.AccountByID(ctx, session.AccountID)
	if err != nil {
		if errors.Is(err, domain.ErrAccountNotFound) {
			return Caller{}, ErrNoSession
		}
		return Caller{}, fmt.Errorf("identity: authenticate: %w", err)
	}
	// An account archived AFTER a session was minted must stop working
	// immediately. The session's own expiry cannot see that.
	if !account.CanAuthenticate() {
		return Caller{}, ErrNoSession
	}

	return Caller{
		AccountID: account.ID,
		Email:     account.Email,
		Status:    account.Status,
		SessionID: session.ID,
		ExpiresAt: session.ExpiresAt,
	}, nil
}
