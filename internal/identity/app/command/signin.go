package command

import (
	"context"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/identity/domain"
	"github.com/0xsj/overwatch-backend/pkg/crypto"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

const DefaultSessionTTL = 14 * 24 * time.Hour

// ErrRejected is the ONE answer to a wrong password, an unknown address and an
// archived account. Three different truths, one response, because any endpoint
// that distinguishes them is a user directory.
var ErrRejected = errors.New(errors.Unauthenticated, "that email and password do not match an account")

type Authenticator struct {
	repo      Repository
	publisher events.Publisher
	hasher    Hasher
	tokens    Tokens
	ids       Minter
	clock     Clock
	ttl       time.Duration
}

func NewAuthenticator(
	repo Repository, publisher events.Publisher, hasher Hasher,
	tokens Tokens, ids Minter, clock Clock, ttl time.Duration,
) *Authenticator {
	if repo == nil || publisher == nil || hasher == nil || tokens == nil || ids == nil || clock == nil {
		panic("identity: NewAuthenticator with a nil dependency")
	}
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	return &Authenticator{repo: repo, publisher: publisher, hasher: hasher,
		tokens: tokens, ids: ids, clock: clock, ttl: ttl}
}

type Credentials struct {
	Email     string
	Password  secret.String
	UserAgent string
	Address   string
}

type SignedIn struct {
	Account domain.Account

	// Token is returned ONCE. Only its hash is stored, so this value cannot be
	// recovered from anywhere after this struct is discarded.
	Token   secret.String
	Session domain.Session
}

func (a *Authenticator) SignIn(ctx context.Context, in Credentials) (SignedIn, error) {
	at := a.clock.Now()
	password := in.Password.Reveal()

	email, err := domain.NewEmail(in.Email)
	if err != nil {
		// An unparseable address costs the same as a wrong password, or the
		// shape of the input leaks through the response time.
		a.burn(password)
		return SignedIn{}, ErrRejected
	}

	account, err := a.repo.AccountByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrAccountNotFound) {
			a.burn(password)
			return SignedIn{}, ErrRejected
		}
		return SignedIn{}, fmt.Errorf("identity: sign in: %w", err)
	}

	credential, err := a.repo.LivePasswordFor(ctx, account.ID)
	if err != nil {
		if errors.Is(err, domain.ErrCredentialGone) {
			a.burn(password)
			return SignedIn{}, ErrRejected
		}
		return SignedIn{}, fmt.Errorf("identity: sign in: %w", err)
	}

	verification, err := a.hasher.Verify(credential.Hash, password)
	if err != nil {
		return SignedIn{}, fmt.Errorf("identity: sign in: %w", err)
	}
	if !verification.Valid {
		return SignedIn{}, ErrRejected
	}

	// decisions/0018: pending signs in, archived does not. Checked AFTER the
	// password, so an archived account is indistinguishable from a wrong one.
	if !account.CanAuthenticate() {
		return SignedIn{}, ErrRejected
	}

	// The one moment the plaintext is in hand. A failure here does not fail the
	// sign-in: the credential was correct.
	if verification.NeedsRehash {
		a.rehash(ctx, credential, password, at)
	}

	token, err := a.tokens.New()
	if err != nil {
		return SignedIn{}, fmt.Errorf("identity: sign in: %w", err)
	}
	session, err := domain.NewSession(a.ids.NewID(), account.ID, token.Hash,
		in.UserAgent, in.Address, at, at.Add(a.ttl))
	if err != nil {
		return SignedIn{}, fmt.Errorf("identity: sign in: %w", err)
	}
	if err := a.repo.CreateSession(ctx, session); err != nil {
		return SignedIn{}, fmt.Errorf("identity: sign in: %w", err)
	}

	if err := a.emit(ctx, domain.EventSessionStarted, account.ID, session.ID,
		domain.SessionStarted{AccountID: account.ID.String(), SessionID: session.ID.String()}); err != nil {
		return SignedIn{}, err
	}
	return SignedIn{Account: account, Token: token.Plaintext, Session: session}, nil
}

func (a *Authenticator) SignOut(ctx context.Context, presented string) error {
	session, err := a.repo.SessionByHash(ctx, crypto.HashToken(presented))
	if err != nil {
		// Signing out of a session that is already gone is the outcome the
		// caller wanted. Reporting it invites a client to retry a no-op.
		if errors.Is(err, domain.ErrSessionGone) {
			return nil
		}
		return fmt.Errorf("identity: sign out: %w", err)
	}
	at := a.clock.Now()
	if err := a.repo.EndSession(ctx, session.ID, at); err != nil {
		if errors.Is(err, domain.ErrSessionGone) {
			return nil
		}
		return fmt.Errorf("identity: sign out: %w", err)
	}
	return a.emit(ctx, domain.EventSessionEnded, session.AccountID, session.ID,
		domain.SessionEnded{AccountID: session.AccountID.String(),
			SessionID: session.ID.String(), Reason: "signed out"})
}

// burn spends the same work a real verification would. Without it, "no such
// address" returns in a fraction of the time "wrong password" does, and the
// endpoint answers a question nobody asked it.
func (a *Authenticator) burn(password string) {
	_, _ = a.hasher.Verify(a.hasher.Dummy(), password)
}

func (a *Authenticator) rehash(ctx context.Context, c domain.Credential, password string, at time.Time) {
	hash, err := a.hasher.Hash(password)
	if err != nil {
		return
	}
	next, err := c.Rehash(hash, at)
	if err != nil {
		return
	}
	_ = a.repo.SaveCredential(ctx, next)
}

// emit attributes the act to the account it just proved.
//
// The request arrived unauthenticated — there was no session to identify it with
// — so the middleware could only install the anonymous actor. By the time this
// runs the identity has been PROVEN, and the record should say who rather than
// leaving `anonymous` on the one act whose whole purpose was establishing who.
//
// It is not a fiction: the actor field means who caused this, and they did. The
// same argument does NOT apply to registration, where nobody has proved
// anything and the subject is the only honest answer.
func (a *Authenticator) emit(ctx context.Context, name string, account, session interface{ String() string }, payload any) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, a.ids)
	}
	if actor, err := provenance.User(account.String()); err == nil {
		prov = prov.WithActor(actor)
	}
	e, err := events.NewDecision(a.ids, a.clock, name,
		SubjectKind+":"+account.String(), prov, payload)
	if err != nil {
		return fmt.Errorf("identity: %s: %w", name, err)
	}
	if err := a.publisher.Publish(ctx, e); err != nil {
		return fmt.Errorf("identity: %s: %w", name, err)
	}
	return nil
}
