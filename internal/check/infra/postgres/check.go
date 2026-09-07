package postgres

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/check/domain"
	"github.com/0xsj/overwatch-backend/internal/check/infra/postgres/checkdb"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

func (s *Store) Create(ctx context.Context, c domain.Check) error {
	err := s.q(ctx).InsertCheck(ctx, checkdb.InsertCheckParams{
		ID:              uuid(c.ID),
		OrgID:           uuid(c.OrgID),
		Name:            c.Name,
		Question:        c.Question,
		AppliesTo:       subjects(c.AppliesTo),
		IntervalSeconds: seconds(c.Interval),
		Enabled:         c.Enabled,
		Human:           c.Human,
		Status:          c.Status.String(),
		Version:         int32(c.Version),
		CreatedBy:       uuid(c.CreatedBy),
		CreatedAt:       stamp(c.CreatedAt),
		UpdatedAt:       stamp(c.UpdatedAt),
		ArchivedAt:      stamp(c.ArchivedAt),
	})
	if err != nil {
		translated := postgres.Translate(ctx, err, "check: insert check")
		if errors.IsKind(translated, errors.Conflict) {
			return fmt.Errorf("check: insert check: %w", domain.ErrNameTaken)
		}
		return translated
	}
	return nil
}

func (s *Store) ByID(ctx context.Context, org, want id.ID) (domain.Check, error) {
	row, err := s.q(ctx).CheckByID(ctx, checkdb.CheckByIDParams{ID: uuid(want), OrgID: uuid(org)})
	if err != nil {
		translated := postgres.Translate(ctx, err, "check: read check")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Check{}, fmt.Errorf("check: read check: %w", domain.ErrNotFound)
		}
		return domain.Check{}, translated
	}
	return check(row)
}

func (s *Store) ForOrg(ctx context.Context, org id.ID, includeArchived bool) ([]domain.Check, error) {
	rows, err := s.q(ctx).ChecksForOrg(ctx, checkdb.ChecksForOrgParams{
		OrgID: uuid(org), IncludeArchived: includeArchived,
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "check: checks for org")
	}
	out := make([]domain.Check, 0, len(rows))
	for _, row := range rows {
		c, err := check(row)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// Schedulable is a GLOBAL read, with no org — the scheduler is one lifecycle for
// the whole process. Every other read in this store takes an org and checks it;
// this one deliberately does not, and the caller is a background loop rather
// than a request.
func (s *Store) Schedulable(ctx context.Context) ([]domain.Check, error) {
	rows, err := s.q(ctx).SchedulableChecks(ctx)
	if err != nil {
		return nil, postgres.Translate(ctx, err, "check: schedulable")
	}
	out := make([]domain.Check, 0, len(rows))
	for _, row := range rows {
		c, err := check(row)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *Store) Save(ctx context.Context, c domain.Check) error {
	n, err := s.q(ctx).UpdateCheck(ctx, checkdb.UpdateCheckParams{
		ID:              uuid(c.ID),
		OrgID:           uuid(c.OrgID),
		Name:            c.Name,
		Question:        c.Question,
		AppliesTo:       subjects(c.AppliesTo),
		IntervalSeconds: seconds(c.Interval),
		Enabled:         c.Enabled,
		Human:           c.Human,
		Status:          c.Status.String(),
		Version:         int32(c.Version),
		UpdatedAt:       stamp(c.UpdatedAt),
		ArchivedAt:      stamp(c.ArchivedAt),
		Version_2:       int32(c.Version - 1),
	})
	if err != nil {
		translated := postgres.Translate(ctx, err, "check: save check")
		if errors.IsKind(translated, errors.Conflict) {
			return fmt.Errorf("check: save check: %w", domain.ErrNameTaken)
		}
		return translated
	}
	if n == 0 {
		return fmt.Errorf("check: save check: %w", domain.ErrStaleWrite)
	}
	return nil
}

// UsingTool is what archiving a tool has to ask, and the reason the chain is
// rows rather than a jsonb column — decisions/0032.
func (s *Store) UsingTool(ctx context.Context, org, tool id.ID) ([]domain.Usage, error) {
	rows, err := s.q(ctx).ChecksUsingTool(ctx, checkdb.ChecksUsingToolParams{
		OrgID: uuid(org), ToolID: uuid(tool),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "check: checks using tool")
	}
	out := make([]domain.Usage, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Usage{CheckID: ident(row.ID), Name: row.Name})
	}
	return out, nil
}
