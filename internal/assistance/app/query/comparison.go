package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type ComparisonReader interface {
	PageComparison(context.Context, id.ID, id.ID, int) ([]domain.Comparison, error)
	ComparisonByID(context.Context, id.ID, id.ID) (domain.Comparison, error)
}

type VisibleComparisonReader interface {
	PageComparisonVisible(context.Context, id.ID, id.ID, int, string) ([]domain.Comparison, error)
	ComparisonByIDVisible(context.Context, id.ID, id.ID, string) (domain.Comparison, error)
}

type Comparisons struct{ reader ComparisonReader }

func NewComparisons(reader ComparisonReader) *Comparisons {
	if reader == nil {
		panic("assistance: NewComparisons with a nil reader")
	}
	return &Comparisons{reader: reader}
}

type ComparisonPage struct {
	Items      []domain.Comparison `json:"items"`
	NextCursor *id.ID              `json:"next_cursor"`
}

func (c *Comparisons) List(ctx context.Context, workspace, before id.ID, limit int) (ComparisonPage, error) {
	if workspace.IsZero() {
		return ComparisonPage{}, domain.ErrWorkspaceRequired
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	rows, err := c.reader.PageComparison(ctx, workspace, before, limit+1)
	if err != nil {
		return ComparisonPage{}, err
	}
	out := ComparisonPage{Items: rows}
	if out.Items == nil {
		out.Items = []domain.Comparison{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

func (c *Comparisons) ByID(ctx context.Context, workspace, comparison id.ID) (domain.Comparison, error) {
	if workspace.IsZero() || comparison.IsZero() {
		return domain.Comparison{}, domain.ErrIDRequired
	}
	return c.reader.ComparisonByID(ctx, workspace, comparison)
}

func (c *Comparisons) ListVisible(ctx context.Context, workspace, before id.ID, limit int, maxSensitivity string) (ComparisonPage, error) {
	if workspace.IsZero() || !validMaxSensitivity(maxSensitivity) {
		return ComparisonPage{}, domain.ErrWorkspaceRequired
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	reader, ok := c.reader.(VisibleComparisonReader)
	if !ok {
		return ComparisonPage{}, domain.ErrWorkspaceRequired
	}
	rows, err := reader.PageComparisonVisible(ctx, workspace, before, limit+1, maxSensitivity)
	if err != nil {
		return ComparisonPage{}, err
	}
	out := ComparisonPage{Items: rows}
	if out.Items == nil {
		out.Items = []domain.Comparison{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

func (c *Comparisons) ByIDVisible(ctx context.Context, workspace, comparison id.ID, maxSensitivity string) (domain.Comparison, error) {
	if workspace.IsZero() || comparison.IsZero() || !validMaxSensitivity(maxSensitivity) {
		return domain.Comparison{}, domain.ErrIDRequired
	}
	reader, ok := c.reader.(VisibleComparisonReader)
	if !ok {
		return domain.Comparison{}, domain.ErrWorkspaceRequired
	}
	return reader.ComparisonByIDVisible(ctx, workspace, comparison, maxSensitivity)
}
