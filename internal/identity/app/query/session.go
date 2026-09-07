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
	LiveSessionsFor(ctx context.Context, account id.ID, at time.Time) ([]domain.Session, error)
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

// Device is one row of "where am I signed in". The user agent and the address
// are shown VERBATIM and are never parsed into a friendly device name: both are
// written by the client, so a tidied "Chrome on macOS" is a claim this server
// cannot stand behind, and the whole purpose of the screen is that a person
// recognises a session they do not recognise.
type Device struct {
	SessionID id.ID
	UserAgent string
	Address   string
	IssuedAt  time.Time
	ExpiresAt time.Time

	// Current marks the session that asked. Without it the screen has a "sign
	// out everywhere" button and no way to say which row is you.
	Current bool
}

// Mine lists the caller's live sessions, newest first. It takes the CURRENT
// session id rather than deriving it, because this package must not see a token.
func (s *Sessions) Mine(ctx context.Context, account, current id.ID) ([]Device, error) {
	if account.IsZero() {
		return nil, domain.ErrIDRequired
	}
	found, err := s.reader.LiveSessionsFor(ctx, account, s.clock.Now())
	if err != nil {
		return nil, fmt.Errorf("identity: my sessions: %w", err)
	}
	out := make([]Device, 0, len(found))
	for _, sn := range found {
		out = append(out, Device{
			SessionID: sn.ID,
			UserAgent: sn.UserAgent,
			Address:   sn.Address,
			IssuedAt:  sn.IssuedAt,
			ExpiresAt: sn.ExpiresAt,
			Current:   sn.ID == current,
		})
	}
	return out, nil
}
