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

func (s *Store) CreateOrg(ctx context.Context, o domain.Org) error {
	err := s.q(ctx).InsertOrg(ctx, orgdb.InsertOrgParams{
		ID:            uuid(o.ID),
		Name:          o.Name,
		Version:       int32(o.Version),
		CreatedAt:     stamp(o.CreatedAt),
		UpdatedAt:     stamp(o.UpdatedAt),
		SourceEventID: maybe(o.SourceEvent),
	})
	if err == nil {
		return nil
	}
	translated := postgres.Translate(ctx, err, "org: insert org")
	if postgres.IsConstraint(translated, "org_source_event") {
		return fmt.Errorf("org: insert org: %w", domain.ErrAlreadyProvisioned)
	}
	return translated
}

func (s *Store) OrgBySourceEvent(ctx context.Context, event id.ID) (domain.Org, error) {
	row, err := s.q(ctx).OrgBySourceEvent(ctx, maybe(event))
	if err != nil {
		translated := postgres.Translate(ctx, err, "org: read by source event")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Org{}, fmt.Errorf("org: read by source event: %w", domain.ErrOrgNotFound)
		}
		return domain.Org{}, translated
	}
	return org(row), nil
}

func (s *Store) OrgByID(ctx context.Context, want id.ID) (domain.Org, error) {
	row, err := s.q(ctx).OrgByID(ctx, uuid(want))
	if err != nil {
		translated := postgres.Translate(ctx, err, "org: read org")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Org{}, fmt.Errorf("org: read org: %w", domain.ErrOrgNotFound)
		}
		return domain.Org{}, translated
	}
	return org(row), nil
}

func (s *Store) SaveOrg(ctx context.Context, o domain.Org) error {
	n, err := s.q(ctx).UpdateOrg(ctx, orgdb.UpdateOrgParams{
		ID:        uuid(o.ID),
		Name:      o.Name,
		Version:   int32(o.Version),
		UpdatedAt: stamp(o.UpdatedAt),
		Version_2: int32(o.Version - 1),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "org: update org")
	}
	if n == 0 {
		if _, err := s.OrgByID(ctx, o.ID); err != nil {
			return err
		}
		return fmt.Errorf("org: update org: %w", domain.ErrStaleWrite)
	}
	return nil
}

func (s *Store) OrgsForAccount(ctx context.Context, account id.ID) ([]domain.Org, error) {
	rows, err := s.q(ctx).OrgsForAccount(ctx, uuid(account))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "org: list orgs")
	}
	out := make([]domain.Org, 0, len(rows))
	for _, row := range rows {
		out = append(out, org(orgdb.OrgOrg(row)))
	}
	return out, nil
}
