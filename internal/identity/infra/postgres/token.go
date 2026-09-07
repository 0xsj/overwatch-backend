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

func (s *Store) CreateToken(ctx context.Context, t domain.Token) error {
	err := s.q(ctx).InsertToken(ctx, identitydb.InsertTokenParams{
		ID:            uuid(t.ID),
		AccountID:     uuid(t.AccountID),
		Kind:          t.Kind.String(),
		Hash:          t.Hash,
		CreatedAt:     stamp(t.CreatedAt),
		ExpiresAt:     stamp(t.ExpiresAt),
		ConsumedAt:    stamp(t.ConsumedAt),
		ProposedEmail: text(t.ProposedEmail.String()),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "identity: insert token")
	}
	return nil
}

func (s *Store) TokenByHash(ctx context.Context, hash string) (domain.Token, error) {
	row, err := s.q(ctx).TokenByHash(ctx, hash)
	if err != nil {
		return domain.Token{}, read(ctx, err, "identity: read token", domain.ErrTokenGone)
	}
	return token(row)
}

func (s *Store) ConsumeToken(ctx context.Context, want id.ID, at time.Time) error {
	n, err := s.q(ctx).ConsumeToken(ctx, identitydb.ConsumeTokenParams{
		ID: uuid(want), ConsumedAt: stamp(at),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "identity: consume token")
	}
	if n == 0 {
		return fmt.Errorf("identity: consume token: %w", domain.ErrTokenSpent)
	}
	return nil
}

// ConsumeLiveTokens is what issuing a new one calls first. An old link that
// still works is a link somebody can replay out of a mailbox.
func (s *Store) ConsumeLiveTokens(ctx context.Context, account id.ID, kind domain.TokenKind, at time.Time) (int, error) {
	n, err := s.q(ctx).ConsumeLiveTokens(ctx, identitydb.ConsumeLiveTokensParams{
		AccountID: uuid(account), Kind: kind.String(), ConsumedAt: stamp(at),
	})
	if err != nil {
		return 0, postgres.Translate(ctx, err, "identity: consume live tokens")
	}
	return int(n), nil
}
