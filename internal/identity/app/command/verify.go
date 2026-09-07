package command

import (
	"context"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/identity/domain"
	"github.com/0xsj/overwatch-backend/pkg/crypto"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

// ErrLinkRejected is the one answer to a token that is unknown, spent or
// expired. Distinguishing them tells a caller holding a guess which half of the
// guess was wrong.
var ErrLinkRejected = errors.New(errors.Unauthenticated, "that link is not valid")

// ReasonPasswordReset is why a session ended when its owner did not end it. The
// reason is the whole value of the record: a session that simply stops existing
// is indistinguishable from one somebody else revoked.
const (
	ReasonPasswordReset = "password reset"

	// A DIFFERENT reason from a reset, because they are different events to the
	// person reading their own history: one is "I could not get in", the other
	// is "I chose to rotate it". Collapsing them loses which happened.
	ReasonPasswordChanged = "password changed"

	// Ended from the session list rather than by signing out of it. A different
	// reason because it answers a different question: this one means somebody
	// with the account decided that device should stop working.
	ReasonSignedOutRemotely = "ended from another session"

	// The account itself is gone. A different reason from the others because a
	// person reading their own history is not going to read this one — it is
	// for whoever asks later why every session ended at once.
	ReasonAccountClosed = "account closed"
)

type Verifier struct {
	repo      Repository
	mailer    Mailer
	publisher events.Publisher
	hasher    Hasher
	tokens    Tokens
	ids       Minter
	clock     Clock
}

func NewVerifier(repo Repository, mailer Mailer, publisher events.Publisher,
	hasher Hasher, tokens Tokens, ids Minter, clock Clock) *Verifier {
	if repo == nil || mailer == nil || publisher == nil || hasher == nil ||
		tokens == nil || ids == nil || clock == nil {
		panic("identity: NewVerifier with a nil dependency")
	}
	return &Verifier{repo: repo, mailer: mailer, publisher: publisher,
		hasher: hasher, tokens: tokens, ids: ids, clock: clock}
}

// RequestVerification sends a fresh link and reports NOTHING about whether the
// address exists. An endpoint that says "no such account" is a directory anybody
// can read one address at a time, and this one is unauthenticated by necessity.
func (v *Verifier) RequestVerification(ctx context.Context, address string) error {
	email, err := domain.NewEmail(address)
	if err != nil {
		return nil
	}
	account, err := v.repo.AccountByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrAccountNotFound) {
			return nil
		}
		return fmt.Errorf("identity: request verification: %w", err)
	}
	if account.Status == domain.StatusActive {
		// Already verified. Silent for the same reason: a second link would
		// tell somebody that this address is registered AND already in use.
		return nil
	}
	return v.issue(ctx, account, domain.KindVerification)
}

// IssueVerification mails a fresh link to an account named by id. It is what
// the subscriber calls; RequestVerification is what a person calls, and the
// difference is that this one has already been told the account exists and so
// has nothing to stay silent about.
func (v *Verifier) IssueVerification(ctx context.Context, account id.ID) error {
	found, err := v.repo.AccountByID(ctx, account)
	if err != nil {
		return fmt.Errorf("identity: issue verification: %w", err)
	}
	if found.Status == domain.StatusActive {
		return nil
	}
	return v.issue(ctx, found, domain.KindVerification)
}

// Verify consumes a link and activates the account it names.
func (v *Verifier) Verify(ctx context.Context, presented secret.String) (domain.Account, error) {
	at := v.clock.Now()

	token, err := v.live(ctx, presented, domain.KindVerification, at)
	if err != nil {
		return domain.Account{}, err
	}
	account, err := v.repo.AccountByID(ctx, token.AccountID)
	if err != nil {
		return domain.Account{}, fmt.Errorf("identity: verify: %w", err)
	}
	activated, err := account.Activate(at)
	if err != nil {
		if errors.Is(err, domain.ErrAlreadyActive) {
			// The link was good and the work was already done. Consume it so it
			// cannot be replayed, and answer with the account rather than an
			// error a person clicking twice cannot act on.
			_ = v.repo.ConsumeToken(ctx, token.ID, at)
			return account, nil
		}
		return domain.Account{}, fmt.Errorf("identity: verify: %w", err)
	}

	if err := v.repo.ConsumeToken(ctx, token.ID, at); err != nil {
		// Lost the race to another click. The account is not activated twice.
		return domain.Account{}, ErrLinkRejected
	}
	if err := v.repo.SaveAccount(ctx, activated); err != nil {
		return domain.Account{}, fmt.Errorf("identity: verify: %w", err)
	}

	if err := v.emit(ctx, domain.EventAccountActivated, activated.ID,
		domain.AccountActivated{AccountID: activated.ID.String()}); err != nil {
		return domain.Account{}, err
	}
	return activated, nil
}

// RequestReset is the same silence as RequestVerification, and for the same
// reason. It answers identically for an address that exists and one that does
// not.
func (v *Verifier) RequestReset(ctx context.Context, address string) error {
	email, err := domain.NewEmail(address)
	if err != nil {
		return nil
	}
	account, err := v.repo.AccountByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrAccountNotFound) {
			return nil
		}
		return fmt.Errorf("identity: request reset: %w", err)
	}
	if !account.CanAuthenticate() {
		return nil
	}
	return v.issue(ctx, account, domain.KindPasswordReset)
}

// CompleteReset replaces the password and ENDS EVERY SESSION. Whoever asked for
// the reset may not be whoever is currently signed in, and leaving those
// sessions alive means a stolen one survives the recovery meant to stop it.
func (v *Verifier) CompleteReset(ctx context.Context, presented secret.String, password secret.String) error {
	at := v.clock.Now()
	plain := password.Reveal()
	switch {
	case len(plain) < MinPasswordLength:
		return ErrPasswordTooShort
	case len(plain) > MaxPasswordLength:
		return ErrPasswordTooLong
	}

	token, err := v.live(ctx, presented, domain.KindPasswordReset, at)
	if err != nil {
		return err
	}
	current, err := v.repo.LivePasswordFor(ctx, token.AccountID)
	if err != nil {
		return fmt.Errorf("identity: complete reset: %w", err)
	}
	hash, err := v.hasher.Hash(plain)
	if err != nil {
		return fmt.Errorf("identity: complete reset: %w", err)
	}
	next, err := current.Rehash(hash, at)
	if err != nil {
		return fmt.Errorf("identity: complete reset: %w", err)
	}

	if err := v.repo.ConsumeToken(ctx, token.ID, at); err != nil {
		return ErrLinkRejected
	}
	if err := v.repo.SaveCredential(ctx, next); err != nil {
		return fmt.Errorf("identity: complete reset: %w", err)
	}
	revoked, err := v.repo.EndSessionsFor(ctx, token.AccountID, at)
	if err != nil {
		return fmt.Errorf("identity: complete reset: %w", err)
	}

	if err := v.emit(ctx, domain.EventCredentialChanged, token.AccountID,
		domain.CredentialChanged{
			AccountID:    token.AccountID.String(),
			CredentialID: next.ID.String(),
			Kind:         next.Kind.String(),
		}); err != nil {
		return err
	}

	// One event per session, not a count. A person whose session died is owed
	// the reason, and "your sessions were ended because the password was reset"
	// is only answerable if the row saying so names the session. A count in the
	// credential.changed payload cannot be joined to the session it killed.
	for _, session := range revoked {
		if err := v.emit(ctx, domain.EventSessionEnded, token.AccountID,
			domain.SessionEnded{
				AccountID: token.AccountID.String(),
				SessionID: session.String(),
				Reason:    ReasonPasswordReset,
			}); err != nil {
			return err
		}
	}
	return nil
}

// issue invalidates every outstanding link of this kind before minting one. Two
// live links means the older one still works after the newer was requested,
// which is exactly what somebody who got hold of the first is counting on.
func (v *Verifier) issue(ctx context.Context, account domain.Account, kind domain.TokenKind) error {
	at := v.clock.Now()
	if _, err := v.repo.ConsumeLiveTokens(ctx, account.ID, kind, at); err != nil {
		return fmt.Errorf("identity: issue %s: %w", kind, err)
	}
	minted, err := v.tokens.New()
	if err != nil {
		return fmt.Errorf("identity: issue %s: %w", kind, err)
	}
	token, err := domain.NewToken(v.ids.NewID(), account.ID, kind, minted.Hash, at)
	if err != nil {
		return fmt.Errorf("identity: issue %s: %w", kind, err)
	}
	if err := v.repo.CreateToken(ctx, token); err != nil {
		return fmt.Errorf("identity: issue %s: %w", kind, err)
	}

	// The mail goes last. A link that arrives for a token nobody stored is a
	// link that cannot work, and the person clicking it has no way to know why.
	send := v.mailer.Verify
	if kind == domain.KindPasswordReset {
		send = v.mailer.Reset
	}
	if err := send(ctx, account.Email.String(), minted.Plaintext.Reveal()); err != nil {
		return fmt.Errorf("identity: issue %s: %w", kind, err)
	}
	return nil
}

func (v *Verifier) live(ctx context.Context, presented secret.String, kind domain.TokenKind, at time.Time) (domain.Token, error) {
	plain := presented.Reveal()
	if plain == "" {
		return domain.Token{}, ErrLinkRejected
	}
	token, err := v.repo.TokenByHash(ctx, crypto.HashToken(plain))
	if err != nil {
		if errors.Is(err, domain.ErrTokenGone) {
			return domain.Token{}, ErrLinkRejected
		}
		return domain.Token{}, fmt.Errorf("identity: %w", err)
	}
	if token.Kind != kind || !token.Live(at) {
		return domain.Token{}, ErrLinkRejected
	}
	return token, nil
}

func (v *Verifier) emit(ctx context.Context, name string, account interface{ String() string }, payload any) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, v.ids)
	}
	// The act is proven to belong to this account: they held a link only its
	// address receives.
	if actor, err := provenance.User(account.String()); err == nil {
		prov = prov.WithActor(actor)
	}
	e, err := events.NewDecision(v.ids, v.clock, name,
		SubjectKind+":"+account.String(), prov, payload)
	if err != nil {
		return fmt.Errorf("identity: %s: %w", name, err)
	}
	if err := v.publisher.Publish(ctx, e); err != nil {
		return fmt.Errorf("identity: %s: %w", name, err)
	}
	return nil
}
