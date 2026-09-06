package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/identity/domain"
	"github.com/0xsj/overwatch-backend/internal/identity/infra/postgres/identitydb"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

func (s *Store) CreateSession(ctx context.Context, sn domain.Session) error {
	err := s.q(ctx).InsertSession(ctx, identitydb.InsertSessionParams{
		ID:        uuid(sn.ID),
		AccountID: uuid(sn.AccountID),
		Hash:      sn.Hash,
		UserAgent: sn.UserAgent,
		Address:   sn.Address,
		IssuedAt:  stamp(sn.IssuedAt),
		ExpiresAt: stamp(sn.ExpiresAt),
		RevokedAt: stamp(sn.RevokedAt),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "identity: insert session")
	}
	return nil
}

func (s *Store) SessionByHash(ctx context.Context, hash string) (domain.Session, error) {
	row, err := s.q(ctx).SessionByHash(ctx, hash)
	if err != nil {
		return domain.Session{}, read(ctx, err, "identity: read session", domain.ErrSessionGone)
	}
	return session(row), nil
}

func (s *Store) SessionsFor(ctx context.Context, account id.ID) ([]domain.Session, error) {
	rows, err := s.q(ctx).SessionsForAccount(ctx, uuid(account))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "identity: list sessions")
	}
	out := make([]domain.Session, 0, len(rows))
	for _, row := range rows {
		out = append(out, session(row))
	}
	return out, nil
}

func (s *Store) EndSession(ctx context.Context, want id.ID, at time.Time) error {
	n, err := s.q(ctx).RevokeSession(ctx, identitydb.RevokeSessionParams{
		ID:        uuid(want),
		RevokedAt: stamp(at),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "identity: revoke session")
	}
	if n == 0 {
		return fmt.Errorf("identity: revoke session: %w", domain.ErrSessionGone)
	}
	return nil
}

func (s *Store) EndSessionsFor(ctx context.Context, account id.ID, at time.Time) (int, error) {
	n, err := s.q(ctx).RevokeSessionsForAccount(ctx, identitydb.RevokeSessionsForAccountParams{
		AccountID: uuid(account),
		RevokedAt: stamp(at),
	})
	if err != nil {
		return 0, postgres.Translate(ctx, err, "identity: revoke sessions")
	}
	return int(n), nil
}
