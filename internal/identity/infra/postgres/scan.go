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
