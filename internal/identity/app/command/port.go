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

	CreateToken(ctx context.Context, t domain.Token) error
	TokenByHash(ctx context.Context, hash string) (domain.Token, error)
	ConsumeToken(ctx context.Context, token id.ID, at time.Time) error
	ConsumeLiveTokens(ctx context.Context, account id.ID, kind domain.TokenKind, at time.Time) (int, error)

	AccountByID(ctx context.Context, account id.ID) (domain.Account, error)
	SaveAccount(ctx context.Context, a domain.Account) error

	CredentialsFor(ctx context.Context, account id.ID) ([]domain.Credential, error)

	CreateSession(ctx context.Context, s domain.Session) error
	SessionByHash(ctx context.Context, hash string) (domain.Session, error)
	EndSession(ctx context.Context, session id.ID, at time.Time) error
	// EndSessionsFor answers with the sessions it revoked, not a count. A
	// caller has to emit one session.ended per session or the journal cannot
	// say why a session died — see verify.go.
	EndSessionsFor(ctx context.Context, account id.ID, at time.Time) ([]id.ID, error)
	EndSessionsExcept(ctx context.Context, account, keep id.ID, at time.Time) ([]id.ID, error)
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

// Mailer is the narrowest thing this package needs from pkg/mail: two messages,
// each taking an address and a token. It never learns what a link looks like.
type Mailer interface {
	Verify(ctx context.Context, to, token string) error
	Reset(ctx context.Context, to, token string) error

	// ConfirmEmail goes to the NEW address and carries the link.
	ConfirmEmail(ctx context.Context, to, token string) error

	// EmailChangeRequested goes to the OLD one and carries NO link and no
	// token — decisions/0021. It is the only channel that reaches the real
	// owner when the session is not theirs, and a link in it would itself be a
	// takeover primitive if the mailbox is what was compromised.
	EmailChangeRequested(ctx context.Context, to, proposed string) error
}
