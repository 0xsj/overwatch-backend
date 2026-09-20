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
