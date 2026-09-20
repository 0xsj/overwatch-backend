package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/review/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type SourceLinkReader interface {
	PageSourceLinks(context.Context, id.ID, id.ID, int) ([]domain.SourceLinkItem, error)
}

type SourceLinks struct{ reader SourceLinkReader }

func NewSourceLinks(reader SourceLinkReader) *SourceLinks {
	if reader == nil {
		panic("review: NewSourceLinks with a nil reader")
	}
	return &SourceLinks{reader: reader}
}

type SourceLinkPage struct {
	Items      []domain.SourceLinkItem `json:"items"`
	NextCursor *id.ID                  `json:"next_cursor"`
}

func (s *SourceLinks) List(ctx context.Context, workspace, before id.ID, limit int) (SourceLinkPage, error) {
	if workspace.IsZero() {
		return SourceLinkPage{}, domain.ErrInvalid
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.reader.PageSourceLinks(ctx, workspace, before, limit+1)
	if err != nil {
		return SourceLinkPage{}, err
	}
	out := SourceLinkPage{Items: rows}
	if out.Items == nil {
		out.Items = []domain.SourceLinkItem{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}
