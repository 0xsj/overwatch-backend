package postgres

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/identity/domain"
	"github.com/0xsj/overwatch-backend/internal/identity/infra/postgres/identitydb"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

func (s *Store) q(ctx context.Context) *identitydb.Queries {
	return identitydb.New(s.db.DB(ctx))
}

func (s *Store) CreateAccount(ctx context.Context, a domain.Account) error {
	err := s.q(ctx).InsertAccount(ctx, identitydb.InsertAccountParams{
		ID:        uuid(a.ID),
		Email:     a.Email.String(),
		Name:      a.Name,
		Status:    a.Status.String(),
		Version:   int32(a.Version),
		CreatedAt: stamp(a.CreatedAt),
		UpdatedAt: stamp(a.UpdatedAt),
	})
	if err == nil {
		return nil
	}
	translated := postgres.Translate(ctx, err, "identity: insert account")
	if postgres.IsConstraint(translated, "account_live_email") {
		return fmt.Errorf("identity: insert account: %w", domain.ErrAccountExists)
	}
	return translated
}

func (s *Store) AccountByID(ctx context.Context, want id.ID) (domain.Account, error) {
	row, err := s.q(ctx).AccountByID(ctx, uuid(want))
	if err != nil {
		return domain.Account{}, read(ctx, err, "identity: read account", domain.ErrAccountNotFound)
	}
	return account(row)
}

func (s *Store) AccountByEmail(ctx context.Context, want domain.Email) (domain.Account, error) {
	row, err := s.q(ctx).AccountByEmail(ctx, want.String())
	if err != nil {
		return domain.Account{}, read(ctx, err, "identity: read account by email", domain.ErrAccountNotFound)
	}
	return account(row)
}

func (s *Store) SaveAccount(ctx context.Context, a domain.Account) error {
	n, err := s.q(ctx).UpdateAccount(ctx, identitydb.UpdateAccountParams{
		ID: uuid(a.ID),
		// Every mutable column, and the list must match what the domain can
		// change. A column omitted here is a transition the domain permits, the
		// store accepts, and the database silently discards — which is how
		// ChangeEmail returned a moved account and moved nothing.
		Email:     a.Email.String(),
		Name:      a.Name,
		Status:    a.Status.String(),
		Version:   int32(a.Version),
		UpdatedAt: stamp(a.UpdatedAt),
		Version_2: int32(a.Version - 1),
	})
	if err != nil {
		translated := postgres.Translate(ctx, err, "identity: update account")
		if postgres.IsConstraint(translated, "account_live_email") {
			return fmt.Errorf("identity: update account: %w", domain.ErrAccountExists)
		}
		return translated
	}
	return saved(ctx, n, nil, "identity: update account", func(ctx context.Context) error {
		_, err := s.AccountByID(ctx, a.ID)
		return err
	})
}
