package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/review/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type ClusterCoverageReader interface {
	PageClusterCoverage(context.Context, id.ID, id.ID, int) ([]domain.ClusterCoverage, error)
}

type ClusterCoverage struct{ reader ClusterCoverageReader }

func NewClusterCoverage(reader ClusterCoverageReader) *ClusterCoverage {
	if reader == nil {
		panic("review: NewClusterCoverage with a nil reader")
	}
	return &ClusterCoverage{reader: reader}
}

type ClusterCoveragePage struct {
	Items      []domain.ClusterCoverage `json:"items"`
	NextCursor *id.ID                   `json:"next_cursor"`
}

func (c *ClusterCoverage) List(ctx context.Context, workspace, before id.ID, limit int) (ClusterCoveragePage, error) {
	if workspace.IsZero() {
		return ClusterCoveragePage{}, domain.ErrInvalid
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := c.reader.PageClusterCoverage(ctx, workspace, before, limit+1)
	if err != nil {
		return ClusterCoveragePage{}, err
	}
	out := ClusterCoveragePage{Items: rows}
	if out.Items == nil {
		out.Items = []domain.ClusterCoverage{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ClusterID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}
