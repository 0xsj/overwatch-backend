package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type ConnectionReviewReader interface {
	PageConnectionReviews(context.Context, id.ID, id.ID, id.ID, int) ([]domain.ConnectionReview, error)
	ConnectionReviewByID(context.Context, id.ID, id.ID, id.ID) (domain.ConnectionReview, error)
}

type ConnectionReviews struct{ reader ConnectionReviewReader }

func NewConnectionReviews(reader ConnectionReviewReader) *ConnectionReviews {
	if reader == nil {
		panic("assistance: NewConnectionReviews with a nil reader")
	}
	return &ConnectionReviews{reader: reader}
}

type ConnectionReviewPage struct {
	Items      []domain.ConnectionReview `json:"items"`
	NextCursor *id.ID                    `json:"next_cursor"`
}

func (c *ConnectionReviews) List(ctx context.Context, workspace, connection, before id.ID, limit int) (ConnectionReviewPage, error) {
	if workspace.IsZero() || connection.IsZero() {
		return ConnectionReviewPage{}, domain.ErrIDRequired
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	rows, err := c.reader.PageConnectionReviews(ctx, workspace, connection, before, limit+1)
	if err != nil {
		return ConnectionReviewPage{}, err
	}
	out := ConnectionReviewPage{Items: rows}
	if out.Items == nil {
		out.Items = []domain.ConnectionReview{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

func (c *ConnectionReviews) ByID(ctx context.Context, workspace, connection, review id.ID) (domain.ConnectionReview, error) {
	if workspace.IsZero() || connection.IsZero() || review.IsZero() {
		return domain.ConnectionReview{}, domain.ErrIDRequired
	}
	return c.reader.ConnectionReviewByID(ctx, workspace, connection, review)
}
