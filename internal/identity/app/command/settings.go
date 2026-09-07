package command

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/identity/domain"
	"github.com/0xsj/overwatch-backend/pkg/crypto"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

// ErrWrongPassword is what a failed re-authentication answers. It is deliberately
// distinct from sign-in's error: the caller is already authenticated, so there is
// no account to enumerate and nothing to protect by being vague — what they need
// is to know which field was wrong.
var ErrWrongPassword = errors.New(errors.Unauthenticated, "that is not your current password")

// Settings is what a person changes about themselves. Every method here takes an
// account id that the transport has already authenticated; none of them takes an
// email, because none of them is a way to act on somebody else.
type Settings struct {
	repo      Repository
	mailer    Mailer
	publisher events.Publisher
	hasher    Hasher
	tokens    Tokens
	ids       Minter
	clock     Clock
}

func NewSettings(repo Repository, mailer Mailer, publisher events.Publisher,
	hasher Hasher, tokens Tokens, ids Minter, clock Clock) *Settings {
	if repo == nil || mailer == nil || publisher == nil || hasher == nil ||
		tokens == nil || ids == nil || clock == nil {
		panic("identity: NewSettings with a nil dependency")
	}
	return &Settings{repo: repo, mailer: mailer, publisher: publisher,
		hasher: hasher, tokens: tokens, ids: ids, clock: clock}
}

// Rename changes a display name and asks for no password — decisions/0021. A
// name is not a credential and nothing recovers through it; requiring a password
// would train people to type it into any form that asks.
func (s *Settings) Rename(ctx context.Context, account id.ID, name string) (domain.Account, error) {
	at := s.clock.Now()
	found, err := s.repo.AccountByID(ctx, account)
	if err != nil {
		return domain.Account{}, fmt.Errorf("identity: rename: %w", err)
	}
	renamed, err := found.Rename(name, at)
	if err != nil {
		return domain.Account{}, fmt.Errorf("identity: rename: %w", err)
	}
	if err := s.repo.SaveAccount(ctx, renamed); err != nil {
		return domain.Account{}, fmt.Errorf("identity: rename: %w", err)
	}
	return renamed, nil
}

// ChangePassword re-authenticates first — decisions/0021. A live session proves
// somebody who held the password once is here, not that the owner is; a borrowed
// laptop and a lifted token both yield a session, and this is the control in
// front of them.
func (s *Settings) ChangePassword(ctx context.Context, account, session id.ID,
	current, next secret.String) error {
	at := s.clock.Now()

	plain := next.Reveal()
	switch {
	case len(plain) < MinPasswordLength:
		return ErrPasswordTooShort
	case len(plain) > MaxPasswordLength:
		return ErrPasswordTooLong
	}

	held, err := s.repo.LivePasswordFor(ctx, account)
	if err != nil {
		return fmt.Errorf("identity: change password: %w", err)
	}
	verdict, err := s.hasher.Verify(held.Hash, current.Reveal())
	if err != nil {
		return fmt.Errorf("identity: change password: %w", err)
	}
	if !verdict.Valid {
		return ErrWrongPassword
	}

	hash, err := s.hasher.Hash(plain)
	if err != nil {
		return fmt.Errorf("identity: change password: %w", err)
	}
	rotated, err := held.Rehash(hash, at)
	if err != nil {
		return fmt.Errorf("identity: change password: %w", err)
	}
	if err := s.repo.SaveCredential(ctx, rotated); err != nil {
		return fmt.Errorf("identity: change password: %w", err)
	}

	// A pending email change requested by whoever held a stolen session must not
	// survive the owner's recovery — decisions/0021. Same moment, same reason as
	// evicting the other sessions.
	if _, err := s.repo.ConsumeLiveTokens(ctx, account, domain.KindEmailChange, at); err != nil {
		return fmt.Errorf("identity: change password: %w", err)
	}

	// Everyone else goes; the caller stays. "Change my password" is what
	// somebody does when they think a session is not theirs.
	revoked, err := s.repo.EndSessionsExcept(ctx, account, session, at)
	if err != nil {
		return fmt.Errorf("identity: change password: %w", err)
	}

	if err := s.emit(ctx, account, domain.EventCredentialChanged,
		domain.CredentialChanged{
			AccountID:    account.String(),
			CredentialID: rotated.ID.String(),
			Kind:         rotated.Kind.String(),
		}); err != nil {
		return err
	}
	for _, ended := range revoked {
		if err := s.emit(ctx, account, domain.EventSessionEnded, domain.SessionEnded{
			AccountID: account.String(),
			SessionID: ended.String(),
			Reason:    ReasonPasswordChanged,
		}); err != nil {
			return err
		}
	}
	return nil
}

// RequestEmailChange is prove-then-switch — decisions/0021. Nothing about the
// account changes here: the old address is still the login, still where a reset
// goes, and still what /v1/me reports, until the new one is confirmed.
func (s *Settings) RequestEmailChange(ctx context.Context, account id.ID,
	proposed string, current secret.String) error {
	at := s.clock.Now()

	email, err := domain.NewEmail(proposed)
	if err != nil {
		return err
	}
	found, err := s.repo.AccountByID(ctx, account)
	if err != nil {
		return fmt.Errorf("identity: request email change: %w", err)
	}
	if found.Email == email {
		return errors.New(errors.Invalid, "that is already your address")
	}

	held, err := s.repo.LivePasswordFor(ctx, account)
	if err != nil {
		return fmt.Errorf("identity: request email change: %w", err)
	}
	verdict, err := s.hasher.Verify(held.Hash, current.Reveal())
	if err != nil {
		return fmt.Errorf("identity: request email change: %w", err)
	}
	if !verdict.Valid {
		return ErrWrongPassword
	}

	// One live proposal at a time, for the same reason a verification link
	// invalidates the last: two live links means the older still works after
	// somebody asked for a newer.
	if _, err := s.repo.ConsumeLiveTokens(ctx, account, domain.KindEmailChange, at); err != nil {
		return fmt.Errorf("identity: request email change: %w", err)
	}
	minted, err := s.tokens.New()
	if err != nil {
		return fmt.Errorf("identity: request email change: %w", err)
	}
	token, err := domain.NewEmailChange(s.ids.NewID(), account, email, minted.Hash, at)
	if err != nil {
		return fmt.Errorf("identity: request email change: %w", err)
	}
	if err := s.repo.CreateToken(ctx, token); err != nil {
		return fmt.Errorf("identity: request email change: %w", err)
	}

	if err := s.mailer.ConfirmEmail(ctx, email.String(), minted.Plaintext.Reveal()); err != nil {
		return fmt.Errorf("identity: request email change: %w", err)
	}
	// The notice is the security control and not a courtesy — it is the only
	// channel that reaches the real owner when the session is not theirs. It is
	// sent BEFORE confirmation, because afterwards it would be going to an
	// address the account no longer has.
	if err := s.mailer.EmailChangeRequested(ctx, found.Email.String(), email.String()); err != nil {
		return fmt.Errorf("identity: request email change: %w", err)
	}
	return nil
}

// ConfirmEmailChange moves the account to the address the TOKEN names, never one
// the caller supplies — decisions/0021. It is unauthenticated by necessity: the
// link is clicked in whatever browser reads the new mailbox.
func (s *Settings) ConfirmEmailChange(ctx context.Context, presented secret.String) (domain.Account, error) {
	at := s.clock.Now()
	plain := presented.Reveal()
	if plain == "" {
		return domain.Account{}, ErrLinkRejected
	}

	token, err := s.repo.TokenByHash(ctx, crypto.HashToken(plain))
	if err != nil {
		if errors.Is(err, domain.ErrTokenGone) {
			return domain.Account{}, ErrLinkRejected
		}
		return domain.Account{}, fmt.Errorf("identity: confirm email: %w", err)
	}
	if token.Kind != domain.KindEmailChange || !token.Live(at) {
		return domain.Account{}, ErrLinkRejected
	}
	if token.ProposedEmail.String() == "" {
		// A hand-edited row, or one whose address no longer parses. It must not
		// authorise a change to nothing.
		return domain.Account{}, ErrLinkRejected
	}

	found, err := s.repo.AccountByID(ctx, token.AccountID)
	if err != nil {
		return domain.Account{}, fmt.Errorf("identity: confirm email: %w", err)
	}
	moved, err := found.ChangeEmail(token.ProposedEmail, at)
	if err != nil {
		return domain.Account{}, fmt.Errorf("identity: confirm email: %w", err)
	}

	if err := s.repo.ConsumeToken(ctx, token.ID, at); err != nil {
		return domain.Account{}, ErrLinkRejected
	}
	if err := s.repo.SaveAccount(ctx, moved); err != nil {
		// The unique index on a live email is what refuses an address taken
		// since the proposal. Refusing at CONFIRMATION rather than at proposal
		// is deliberate: refusing earlier tells an authenticated caller that an
		// arbitrary address is registered.
		return domain.Account{}, fmt.Errorf("identity: confirm email: %w", err)
	}

	if err := s.emit(ctx, moved.ID, domain.EventEmailChanged, domain.EmailChanged{
		AccountID: moved.ID.String(),
		Email:     moved.Email.String(),
	}); err != nil {
		return domain.Account{}, err
	}
	return moved, nil
}

// Close archives the account, revokes every credential and ends every session —
// decisions/0028. It is the most destructive self-service act in the product, so
// it re-authenticates like the other two.
//
// **It does NOT check whether the caller is somebody's last owner.** That is
// org's question and this package may not ask it; the composition root does,
// before calling this. The contract is that the check has already happened.
//
// Ending every session includes the one making the request: the caller is
// signed out by the response.
func (s *Settings) Close(ctx context.Context, account id.ID, current secret.String) error {
	at := s.clock.Now()

	held, err := s.repo.LivePasswordFor(ctx, account)
	if err != nil {
		return fmt.Errorf("identity: close account: %w", err)
	}
	verdict, err := s.hasher.Verify(held.Hash, current.Reveal())
	if err != nil {
		return fmt.Errorf("identity: close account: %w", err)
	}
	if !verdict.Valid {
		return ErrWrongPassword
	}

	found, err := s.repo.AccountByID(ctx, account)
	if err != nil {
		return fmt.Errorf("identity: close account: %w", err)
	}
	gone, err := found.Archive(at)
	if err != nil {
		return err
	}
	if err := s.repo.SaveAccount(ctx, gone); err != nil {
		return fmt.Errorf("identity: close account: %w", err)
	}

	// Every credential, not only the password: an api_key that outlived its
	// owner is a way into an account nobody is watching.
	credentials, err := s.repo.CredentialsFor(ctx, account)
	if err != nil {
		return fmt.Errorf("identity: close account: %w", err)
	}
	for _, c := range credentials {
		if c.Revoked() {
			continue
		}
		revoked, err := c.Revoke(at)
		if err != nil {
			return fmt.Errorf("identity: close account: %w", err)
		}
		if err := s.repo.SaveCredential(ctx, revoked); err != nil {
			return fmt.Errorf("identity: close account: %w", err)
		}
	}

	// Any pending email change dies with it, for the same reason a password
	// change kills one — decisions/0021.
	if _, err := s.repo.ConsumeLiveTokens(ctx, account, domain.KindEmailChange, at); err != nil {
		return fmt.Errorf("identity: close account: %w", err)
	}
	ended, err := s.repo.EndSessionsFor(ctx, account, at)
	if err != nil {
		return fmt.Errorf("identity: close account: %w", err)
	}

	if err := s.emit(ctx, account, domain.EventAccountArchived,
		domain.AccountArchived{AccountID: account.String()}); err != nil {
		return err
	}
	for _, session := range ended {
		if err := s.emit(ctx, account, domain.EventSessionEnded, domain.SessionEnded{
			AccountID: account.String(), SessionID: session.String(),
			Reason: ReasonAccountClosed,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Settings) emit(ctx context.Context, account id.ID, name string, payload any) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, s.ids)
	}
	if actor, err := provenance.User(account.String()); err == nil {
		prov = prov.WithActor(actor)
	}
	e, err := events.NewDecision(s.ids, s.clock, name,
		SubjectKind+":"+account.String(), prov, payload)
	if err != nil {
		return fmt.Errorf("identity: %s: %w", name, err)
	}
	if err := s.publisher.Publish(ctx, e); err != nil {
		return fmt.Errorf("identity: %s: %w", name, err)
	}
	return nil
}
