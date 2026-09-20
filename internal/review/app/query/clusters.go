package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/review/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type ClusterReader interface {
	PageClusters(context.Context, id.ID, id.ID, int) ([]domain.Cluster, error)
	ByID(context.Context, id.ID, id.ID) (domain.Cluster, error)
}

type Clusters struct{ reader ClusterReader }

func NewClusters(reader ClusterReader) *Clusters {
	if reader == nil {
		panic("review: NewClusters with a nil reader")
	}
	return &Clusters{reader: reader}
}

type ClusterPage struct {
	Items      []domain.Cluster `json:"items"`
	NextCursor *id.ID           `json:"next_cursor"`
}

func (c *Clusters) List(ctx context.Context, workspace, before id.ID, limit int) (ClusterPage, error) {
	if workspace.IsZero() {
		return ClusterPage{}, domain.ErrInvalid
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := c.reader.PageClusters(ctx, workspace, before, limit+1)
	if err != nil {
		return ClusterPage{}, err
	}
	out := ClusterPage{Items: rows}
	if out.Items == nil {
		out.Items = []domain.Cluster{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

func (c *Clusters) ByID(ctx context.Context, workspace, want id.ID) (domain.Cluster, error) {
	if workspace.IsZero() || want.IsZero() {
		return domain.Cluster{}, domain.ErrInvalid
	}
	return c.reader.ByID(ctx, workspace, want)
}
