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

func (s *Store) CreateMapping(ctx context.Context, m domain.Mapping) error {
	err := s.q(ctx).InsertMapping(ctx, tooldb.InsertMappingParams{
		ID:         uuid(m.ID),
		OrgID:      uuid(m.OrgID),
		ToolID:     uuid(m.ToolID),
		Field:      m.Field,
		Expression: m.Expression,
		Version:    int32(m.Version),
		State:      m.State.String(),
		CreatedBy:  uuid(m.CreatedBy),
		CreatedAt:  stamp(m.CreatedAt),
		PromotedAt: stamp(m.PromotedAt),
		RetiredAt:  stamp(m.RetiredAt),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "tool: insert mapping")
	}
	return nil
}

func (s *Store) MappingByID(ctx context.Context, org, want id.ID) (domain.Mapping, error) {
	row, err := s.q(ctx).MappingByID(ctx, tooldb.MappingByIDParams{ID: uuid(want), OrgID: uuid(org)})
	if err != nil {
		translated := postgres.Translate(ctx, err, "tool: read mapping")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Mapping{}, fmt.Errorf("tool: read mapping: %w", domain.ErrMappingGone)
		}
		return domain.Mapping{}, translated
	}
	return mapping(row)
}

// MappingsForTool answers with EVERY version, newest first within each field.
// Retired ones are the history the citations point at — the same reason scope's
// All keeps superseded rules.
func (s *Store) MappingsForTool(ctx context.Context, org, tool id.ID) ([]domain.Mapping, error) {
	rows, err := s.q(ctx).MappingsForTool(ctx, tooldb.MappingsForToolParams{
		ToolID: uuid(tool), OrgID: uuid(org),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "tool: mappings for tool")
	}
	out := make([]domain.Mapping, 0, len(rows))
	for _, row := range rows {
		m, err := mapping(row)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// LiveMapping is the reader's question — "what turns this tool's output into
// this field right now". A field with no live version answers ErrMappingGone,
// which is the SILENCE a promotion crash leaves behind rather than an ambiguity.
func (s *Store) LiveMapping(ctx context.Context, tool id.ID, field string) (domain.Mapping, error) {
	row, err := s.q(ctx).LiveMappingFor(ctx, tooldb.LiveMappingForParams{
		ToolID: uuid(tool), Field: field,
	})
	if err != nil {
		translated := postgres.Translate(ctx, err, "tool: live mapping")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Mapping{}, fmt.Errorf("tool: live mapping: %w", domain.ErrMappingGone)
		}
		return domain.Mapping{}, translated
	}
	return mapping(row)
}

// NextVersion is read inside the caller's transaction and the unique index is
// what makes it safe: two concurrent authors can both read 4, and the second
// insert is refused rather than reusing a number a citation already carries.
func (s *Store) NextVersion(ctx context.Context, tool id.ID, field string) (int, error) {
	n, err := s.q(ctx).NextMappingVersion(ctx, tooldb.NextMappingVersionParams{
		ToolID: uuid(tool), Field: field,
	})
	if err != nil {
		return 0, postgres.Translate(ctx, err, "tool: next mapping version")
	}
	return int(n), nil
}

func (s *Store) SaveMapping(ctx context.Context, m domain.Mapping) error {
	n, err := s.q(ctx).SetMappingState(ctx, tooldb.SetMappingStateParams{
		ID:         uuid(m.ID),
		OrgID:      uuid(m.OrgID),
		State:      m.State.String(),
		PromotedAt: stamp(m.PromotedAt),
		RetiredAt:  stamp(m.RetiredAt),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "tool: save mapping")
	}
	if n == 0 {
		return fmt.Errorf("tool: save mapping: %w", domain.ErrMappingGone)
	}
	return nil
}
