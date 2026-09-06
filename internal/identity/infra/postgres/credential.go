package postgres

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/identity/domain"
	"github.com/0xsj/overwatch-backend/internal/identity/infra/postgres/identitydb"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

func (s *Store) CreateCredential(ctx context.Context, c domain.Credential) error {
	err := s.q(ctx).InsertCredential(ctx, identitydb.InsertCredentialParams{
		ID:        uuid(c.ID),
		AccountID: uuid(c.AccountID),
		Kind:      c.Kind.String(),
		Hash:      c.Hash,
		Name:      c.Name,
		ExpiresAt: stamp(c.ExpiresAt),
		RevokedAt: stamp(c.RevokedAt),
		Version:   int32(c.Version),
		CreatedAt: stamp(c.CreatedAt),
		UpdatedAt: stamp(c.UpdatedAt),
	})
	if err == nil {
		return nil
	}
	translated := postgres.Translate(ctx, err, "identity: insert credential")
	if postgres.IsConstraint(translated, "credential_one_live_password") {
		return fmt.Errorf("identity: insert credential: %w", domain.ErrAlreadyRevoked)
	}
	return translated
}

func (s *Store) CredentialByID(ctx context.Context, want id.ID) (domain.Credential, error) {
	row, err := s.q(ctx).CredentialByID(ctx, uuid(want))
	if err != nil {
		return domain.Credential{}, read(ctx, err, "identity: read credential", domain.ErrCredentialGone)
	}
	return credential(row)
}

func (s *Store) CredentialByHash(ctx context.Context, hash string) (domain.Credential, error) {
	row, err := s.q(ctx).CredentialByHash(ctx, hash)
	if err != nil {
		return domain.Credential{}, read(ctx, err, "identity: read credential by hash", domain.ErrCredentialGone)
	}
	return credential(row)
}

func (s *Store) LivePasswordFor(ctx context.Context, account id.ID) (domain.Credential, error) {
	row, err := s.q(ctx).LivePasswordForAccount(ctx, uuid(account))
	if err != nil {
		return domain.Credential{}, read(ctx, err, "identity: read password", domain.ErrCredentialGone)
	}
	return credential(row)
}

func (s *Store) CredentialsFor(ctx context.Context, account id.ID) ([]domain.Credential, error) {
	rows, err := s.q(ctx).CredentialsForAccount(ctx, uuid(account))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "identity: list credentials")
	}
	out := make([]domain.Credential, 0, len(rows))
	for _, row := range rows {
		c, err := credential(row)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *Store) SaveCredential(ctx context.Context, c domain.Credential) error {
	n, err := s.q(ctx).UpdateCredential(ctx, identitydb.UpdateCredentialParams{
		ID:        uuid(c.ID),
		Hash:      c.Hash,
		Name:      c.Name,
		ExpiresAt: stamp(c.ExpiresAt),
		RevokedAt: stamp(c.RevokedAt),
		Version:   int32(c.Version),
		UpdatedAt: stamp(c.UpdatedAt),
		Version_2: int32(c.Version - 1),
	})
	return saved(ctx, n, err, "identity: update credential", func(ctx context.Context) error {
		_, err := s.CredentialByID(ctx, c.ID)
		return err
	})
}
