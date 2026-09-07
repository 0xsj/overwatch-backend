package postgres

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/tool/domain"
	"github.com/0xsj/overwatch-backend/internal/tool/infra/postgres/tooldb"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

func (s *Store) Create(ctx context.Context, t domain.Tool) error {
	err := s.q(ctx).InsertTool(ctx, tooldb.InsertToolParams{
		ID:               uuid(t.ID),
		OrgID:            uuid(t.OrgID),
		Name:             t.Name,
		Argv:             t.Argv,
		Intensity:        t.Intensity.String(),
		Consumes:         text(t.Consumes.String()),
		Produces:         text(t.Produces.String()),
		SuccessExitCodes: codes(t.SuccessExitCodes),
		Status:           t.Status.String(),
		Version:          int32(t.Version),
		CreatedBy:        uuid(t.CreatedBy),
		CreatedAt:        stamp(t.CreatedAt),
		UpdatedAt:        stamp(t.UpdatedAt),
		ArchivedAt:       stamp(t.ArchivedAt),
	})
	if err != nil {
		translated := postgres.Translate(ctx, err, "tool: insert tool")
		if errors.IsKind(translated, errors.Conflict) {
			return fmt.Errorf("tool: insert tool: %w", domain.ErrNameTaken)
		}
		return translated
	}
	return nil
}

func (s *Store) ByID(ctx context.Context, org, want id.ID) (domain.Tool, error) {
	row, err := s.q(ctx).ToolByID(ctx, tooldb.ToolByIDParams{ID: uuid(want), OrgID: uuid(org)})
	if err != nil {
		translated := postgres.Translate(ctx, err, "tool: read tool")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Tool{}, fmt.Errorf("tool: read tool: %w", domain.ErrNotFound)
		}
		return domain.Tool{}, translated
	}
	return tool(row)
}

// ForOrg takes the flag rather than exposing two methods, because both reads are
// the same question with a different lifecycle filter and the archived set is
// only ever wanted by a settings screen.
func (s *Store) ForOrg(ctx context.Context, org id.ID, includeArchived bool) ([]domain.Tool, error) {
	rows, err := s.q(ctx).ToolsForOrg(ctx, tooldb.ToolsForOrgParams{
		OrgID: uuid(org), IncludeArchived: includeArchived,
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "tool: tools for org")
	}
	out := make([]domain.Tool, 0, len(rows))
	for _, row := range rows {
		t, err := tool(row)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func (s *Store) Save(ctx context.Context, t domain.Tool) error {
	n, err := s.q(ctx).UpdateTool(ctx, tooldb.UpdateToolParams{
		ID:               uuid(t.ID),
		OrgID:            uuid(t.OrgID),
		Argv:             t.Argv,
		Intensity:        t.Intensity.String(),
		Consumes:         text(t.Consumes.String()),
		Produces:         text(t.Produces.String()),
		SuccessExitCodes: codes(t.SuccessExitCodes),
		Status:           t.Status.String(),
		Version:          int32(t.Version),
		UpdatedAt:        stamp(t.UpdatedAt),
		ArchivedAt:       stamp(t.ArchivedAt),
		Version_2:        int32(t.Version - 1),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "tool: save tool")
	}
	if n == 0 {
		return fmt.Errorf("tool: save tool: %w", domain.ErrStaleWrite)
	}
	return nil
}
