package postgres

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/identity/domain"
	"github.com/0xsj/overwatch-backend/internal/identity/infra/postgres/identitydb"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func uuid(i id.ID) pgtype.UUID { return pgtype.UUID{Bytes: i, Valid: true} }

func ident(u pgtype.UUID) id.ID {
	if !u.Valid {
		return id.Nil
	}
	return id.ID(u.Bytes)
}

func stamp(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func instant(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time
}

func account(row identitydb.IdentityAccount) (domain.Account, error) {
	status, err := domain.ParseStatus(row.Status)
	if err != nil {
		return domain.Account{}, err
	}
	email, err := domain.NewEmail(row.Email)
	if err != nil {
		return domain.Account{}, err
	}
	return domain.Account{
		ID:        ident(row.ID),
		Email:     email,
		Name:      row.Name,
		Status:    status,
		Version:   int(row.Version),
		CreatedAt: instant(row.CreatedAt),
		UpdatedAt: instant(row.UpdatedAt),
	}, nil
}

func credential(row identitydb.IdentityCredential) (domain.Credential, error) {
	kind, err := domain.ParseKind(row.Kind)
	if err != nil {
		return domain.Credential{}, err
	}
	return domain.Credential{
		ID:        ident(row.ID),
		AccountID: ident(row.AccountID),
		Kind:      kind,
		Hash:      row.Hash,
		Name:      row.Name,
		ExpiresAt: instant(row.ExpiresAt),
		RevokedAt: instant(row.RevokedAt),
		Version:   int(row.Version),
		CreatedAt: instant(row.CreatedAt),
		UpdatedAt: instant(row.UpdatedAt),
	}, nil
}

func session(row identitydb.IdentitySession) domain.Session {
	return domain.Session{
		ID:        ident(row.ID),
		AccountID: ident(row.AccountID),
		Hash:      row.Hash,
		UserAgent: row.UserAgent,
		Address:   row.Address,
		IssuedAt:  instant(row.IssuedAt),
		ExpiresAt: instant(row.ExpiresAt),
		RevokedAt: instant(row.RevokedAt),
	}
}

func token(row identitydb.IdentityToken) (domain.Token, error) {
	kind, err := domain.ParseTokenKind(row.Kind)
	if err != nil {
		return domain.Token{}, err
	}
	// A malformed proposed_email is dropped rather than failing the read. The
	// column is constrained on the way in, so this can only happen to a
	// hand-edited row — and a token whose address will not parse must not
	// authorise a change, which a zero Email guarantees at the call site.
	var proposed domain.Email
	if row.ProposedEmail.Valid {
		proposed, _ = domain.NewEmail(row.ProposedEmail.String)
	}
	return domain.Token{
		ID:            ident(row.ID),
		AccountID:     ident(row.AccountID),
		Kind:          kind,
		Hash:          row.Hash,
		ProposedEmail: proposed,
		CreatedAt:     instant(row.CreatedAt),
		ExpiresAt:     instant(row.ExpiresAt),
		ConsumedAt:    instant(row.ConsumedAt),
	}, nil
}

// text is the pgtype spelling of "a string that may be absent". An empty string
// becomes NULL rather than ”, because the check constraint on proposed_email
// refuses the empty string and the two would otherwise be a silent pair.
func text(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}
