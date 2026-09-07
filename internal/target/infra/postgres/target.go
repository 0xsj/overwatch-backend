package postgres

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/target/domain"
	"github.com/0xsj/overwatch-backend/internal/target/infra/postgres/targetdb"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

func (s *Store) Create(ctx context.Context, t domain.Target) error {
	err := s.q(ctx).InsertTarget(ctx, targetdb.InsertTargetParams{
		ID:          uuid(t.ID),
		WorkspaceID: uuid(t.WorkspaceID),
		Name:        t.Name,
		Kind:        t.Kind.String(),
		Status:      t.Status.String(),
		Version:     int32(t.Version),
		CreatedBy:   uuid(t.CreatedBy),
		CreatedAt:   stamp(t.CreatedAt),
		UpdatedAt:   stamp(t.UpdatedAt),
		ArchivedAt:  stamp(t.ArchivedAt),
	})
	if err == nil {
		return nil
	}
	translated := postgres.Translate(ctx, err, "target: insert")
	if postgres.IsConstraint(translated, "target_live_name") {
		return fmt.Errorf("target: insert: %w", domain.ErrNameTaken)
	}
	return translated
}

// ByID takes BOTH ids, and the workspace is not a filter — it is the tenancy
// check. A lookup by id alone would return another engagement's target to a
// caller who guessed one, and the signature is what stops that being possible.
func (s *Store) ByID(ctx context.Context, workspace, want id.ID) (domain.Target, error) {
	row, err := s.q(ctx).TargetByID(ctx, targetdb.TargetByIDParams{
		ID: uuid(want), WorkspaceID: uuid(workspace),
	})
	if err != nil {
		return domain.Target{}, read(ctx, err, "target: read", domain.ErrNotFound)
	}
	return target(row)
}

// Live is the list a screen shows.
func (s *Store) Live(ctx context.Context, workspace id.ID) ([]domain.Target, error) {
	rows, err := s.q(ctx).LiveTargetsForWorkspace(ctx, uuid(workspace))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "target: live")
	}
	return targets(rows)
}

// All includes archived targets, and is what makes one reachable — and
// therefore reopenable. The same pair workspace uses for ForOrg and AllForOrg,
// and for the same reason: a boolean parameter would be one query pretending to
// be two.
func (s *Store) All(ctx context.Context, workspace id.ID) ([]domain.Target, error) {
	rows, err := s.q(ctx).AllTargetsForWorkspace(ctx, uuid(workspace))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "target: all")
	}
	return targets(rows)
}

// SetRoot is written by a subscriber on `entity.root.created` — decisions/0036,
// filling in the column 0029 declared. It is IDEMPOTENT: the query's `is null`
// predicate means a redelivery affects no rows, so zero is success rather than
// a missing target.
func (s *Store) SetRoot(ctx context.Context, target, root id.ID) error {
	_, err := s.q(ctx).SetRootEntity(ctx, targetdb.SetRootEntityParams{
		ID: uuid(target), RootEntityID: maybe(root),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "target: set root entity")
	}
	return nil
}

func (s *Store) Save(ctx context.Context, t domain.Target) error {
	n, err := s.q(ctx).UpdateTarget(ctx, targetdb.UpdateTargetParams{
		ID:          uuid(t.ID),
		WorkspaceID: uuid(t.WorkspaceID),
		Name:        t.Name,
		Status:      t.Status.String(),
		Version:     int32(t.Version),
		UpdatedAt:   stamp(t.UpdatedAt),
		ArchivedAt:  stamp(t.ArchivedAt),
		Version_2:   int32(t.Version - 1),
	})
	if err != nil {
		translated := postgres.Translate(ctx, err, "target: update")
		if postgres.IsConstraint(translated, "target_live_name") {
			return fmt.Errorf("target: update: %w", domain.ErrNameTaken)
		}
		return translated
	}
	if n == 0 {
		// Absent or stale, and the caller retries one and not the other. The
		// second read is what tells them apart — the same trade identity makes.
		if _, err := s.ByID(ctx, t.WorkspaceID, t.ID); err != nil {
			return fmt.Errorf("target: update: %w", domain.ErrNotFound)
		}
		return fmt.Errorf("target: update: %w", domain.ErrStaleWrite)
	}
	return nil
}
