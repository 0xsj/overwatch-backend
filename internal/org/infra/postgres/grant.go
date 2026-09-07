package postgres

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/internal/org/infra/postgres/orgdb"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

func (s *Store) AddGrant(ctx context.Context, g domain.Grant) error {
	err := s.q(ctx).InsertGrant(ctx, orgdb.InsertGrantParams{
		ID:          uuid(g.ID),
		OrgID:       uuid(g.OrgID),
		AccountID:   uuid(g.AccountID),
		WorkspaceID: uuid(g.WorkspaceID),
		Level:       g.Level.String(),
		Version:     int32(g.Version),
		CreatedAt:   stamp(g.CreatedAt),
		UpdatedAt:   stamp(g.UpdatedAt),
	})
	if err == nil {
		return nil
	}
	translated := postgres.Translate(ctx, err, "org: insert grant")
	if postgres.IsConstraint(translated, "grant_member_workspace") {
		return fmt.Errorf("org: insert grant: %w", domain.ErrGrantExists)
	}
	return translated
}

func (s *Store) GrantFor(ctx context.Context, org, account, workspace id.ID) (domain.Grant, error) {
	row, err := s.q(ctx).GrantFor(ctx, orgdb.GrantForParams{
		OrgID:       uuid(org),
		AccountID:   uuid(account),
		WorkspaceID: uuid(workspace),
	})
	if err != nil {
		translated := postgres.Translate(ctx, err, "org: read grant")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Grant{}, fmt.Errorf("org: read grant: %w", domain.ErrGrantNotFound)
		}
		return domain.Grant{}, translated
	}
	return grant(row)
}

func (s *Store) GrantsForAccount(ctx context.Context, account, org id.ID) ([]domain.Grant, error) {
	rows, err := s.q(ctx).GrantsForAccount(ctx, orgdb.GrantsForAccountParams{
		AccountID: uuid(account),
		OrgID:     uuid(org),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "org: grants for account")
	}
	return grants(rows)
}

func (s *Store) GrantsOnWorkspace(ctx context.Context, workspace id.ID) ([]domain.Grant, error) {
	rows, err := s.q(ctx).GrantsOnWorkspace(ctx, uuid(workspace))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "org: grants on workspace")
	}
	return grants(rows)
}

func (s *Store) SaveGrant(ctx context.Context, g domain.Grant) error {
	n, err := s.q(ctx).UpdateGrant(ctx, orgdb.UpdateGrantParams{
		ID:        uuid(g.ID),
		Level:     g.Level.String(),
		Version:   int32(g.Version),
		UpdatedAt: stamp(g.UpdatedAt),
		Version_2: int32(g.Version - 1),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "org: update grant")
	}
	if n == 0 {
		return fmt.Errorf("org: update grant: %w", domain.ErrStaleWrite)
	}
	return nil
}

// RevokeGrant deletes the row. A stored `none` would give absence two spellings,
// and the two would disagree the first time a query remembered only one.
func (s *Store) RevokeGrant(ctx context.Context, org, account, workspace id.ID) error {
	n, err := s.q(ctx).DeleteGrant(ctx, orgdb.DeleteGrantParams{
		OrgID:       uuid(org),
		AccountID:   uuid(account),
		WorkspaceID: uuid(workspace),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "org: revoke grant")
	}
	if n == 0 {
		return fmt.Errorf("org: revoke grant: %w", domain.ErrGrantNotFound)
	}
	return nil
}

func grants(rows []orgdb.OrgGrant) ([]domain.Grant, error) {
	out := make([]domain.Grant, 0, len(rows))
	for _, row := range rows {
		g, err := grant(row)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}
