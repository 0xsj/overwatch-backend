package command

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/identity/domain"
	"github.com/0xsj/overwatch-backend/pkg/crypto"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Repository interface {
	CreateAccount(ctx context.Context, a domain.Account) error
	CreateCredential(ctx context.Context, c domain.Credential) error

	AccountByEmail(ctx context.Context, email domain.Email) (domain.Account, error)
	LivePasswordFor(ctx context.Context, account id.ID) (domain.Credential, error)
	SaveCredential(ctx context.Context, c domain.Credential) error

	CreateSession(ctx context.Context, s domain.Session) error
	SessionByHash(ctx context.Context, hash string) (domain.Session, error)
	EndSession(ctx context.Context, session id.ID, at time.Time) error
	EndSessionsFor(ctx context.Context, account id.ID, at time.Time) (int, error)
}

type Transactor interface {
	InTx(ctx context.Context, fn func(context.Context) error) error
}

type Hasher interface {
	Hash(password string) (string, error)
	Verify(encoded, password string) (crypto.Verification, error)

	// Dummy is a real hash at current parameters. A caller that finds no account
	// verifies against it anyway, so "no such address" costs what "wrong
	// password" costs — see internal/identity's note on what sign-in must not
	// leak.
	Dummy() string
}

// Tokens mints a session token and its stored hash together, so there is no call
// that yields one without the other.
type Tokens interface {
	New() (crypto.Token, error)
}

type Minter interface {
	NewID() id.ID
}

type Clock interface {
	Now() time.Time
}
