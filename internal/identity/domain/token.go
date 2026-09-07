package domain

import (
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	VerificationTTL  = 24 * time.Hour
	PasswordResetTTL = time.Hour

	// Shorter than a verification and longer than a reset. It is a change the
	// owner just asked for and is expected to confirm from the same sitting, so
	// a day is generous; and unlike a reset there is no locked-out person
	// racing a clock, so an hour is stingy.
	EmailChangeTTL = 4 * time.Hour
)

type TokenKind uint8

const (
	KindVerification TokenKind = iota
	KindPasswordReset
	KindEmailChange
)

func (k TokenKind) String() string {
	switch k {
	case KindPasswordReset:
		return "password_reset"
	case KindEmailChange:
		return "email_change"
	default:
		return "verification"
	}
}

func ParseTokenKind(s string) (TokenKind, error) {
	switch s {
	case "verification":
		return KindVerification, nil
	case "password_reset":
		return KindPasswordReset, nil
	case "email_change":
		return KindEmailChange, nil
	default:
		return KindVerification, ErrTokenKindUnknown
	}
}

func (k TokenKind) TTL() time.Duration {
	switch k {
	case KindPasswordReset:
		return PasswordResetTTL
	case KindEmailChange:
		return EmailChangeTTL
	default:
		return VerificationTTL
	}
}

type Token struct {
	ID        id.ID
	AccountID id.ID
	Kind      TokenKind

	// ProposedEmail is the address this token would move the account TO, and is
	// zero for every other kind — decisions/0021. It is stored rather than sent
	// with the confirmation because a token that carries its own target lets
	// somebody confirm an address the server never agreed to.
	ProposedEmail Email

	Hash       string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	ConsumedAt time.Time
}

func NewToken(newID, account id.ID, kind TokenKind, hash string, at time.Time) (Token, error) {
	if newID.IsZero() || account.IsZero() {
		return Token{}, ErrIDRequired
	}
	if at.IsZero() {
		return Token{}, ErrTimeRequired
	}
	if hash == "" {
		return Token{}, ErrHashRequired
	}
	if kind == KindEmailChange {
		// An email_change with no address is a token that cannot do its job,
		// and the migration refuses the row anyway. Refusing here names the
		// mistake at the call site instead of as a constraint violation.
		return Token{}, ErrProposedEmailRequired
	}
	return Token{
		ID: newID, AccountID: account, Kind: kind, Hash: hash,
		CreatedAt: at, ExpiresAt: at.Add(kind.TTL()),
	}, nil
}

// NewEmailChange is a separate constructor rather than a parameter on NewToken,
// because the address is required for exactly one kind and meaningless for the
// others. A nullable parameter would let every caller pass nothing and only the
// database would notice.
func NewEmailChange(newID, account id.ID, proposed Email, hash string, at time.Time) (Token, error) {
	if newID.IsZero() || account.IsZero() {
		return Token{}, ErrIDRequired
	}
	if at.IsZero() {
		return Token{}, ErrTimeRequired
	}
	if hash == "" {
		return Token{}, ErrHashRequired
	}
	if proposed.String() == "" {
		return Token{}, ErrProposedEmailRequired
	}
	return Token{
		ID: newID, AccountID: account, Kind: KindEmailChange, Hash: hash,
		ProposedEmail: proposed,
		CreatedAt:     at, ExpiresAt: at.Add(KindEmailChange.TTL()),
	}, nil
}

func (t Token) Consumed() bool { return !t.ConsumedAt.IsZero() }

func (t Token) Live(at time.Time) bool {
	return !t.Consumed() && t.ExpiresAt.After(at)
}

// Consume is single-use and says so by refusing a second time. A link that keeps
// working is a link somebody can replay out of a mailbox months later.
func (t Token) Consume(at time.Time) (Token, error) {
	if at.IsZero() {
		return t, ErrTimeRequired
	}
	if t.Consumed() {
		return t, ErrTokenSpent
	}
	if !t.ExpiresAt.After(at) {
		return t, ErrTokenExpired
	}
	next := t
	next.ConsumedAt = at
	return next, nil
}
