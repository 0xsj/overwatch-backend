package query

import (
	"context"
	"strings"

	"github.com/0xsj/overwatch-backend/internal/review/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type BoardReader interface {
	PageBoard(context.Context, id.ID, id.ID, domain.BoardFilters, int) ([]domain.BoardItem, error)
}

type Board struct{ reader BoardReader }

func NewBoard(reader BoardReader) *Board {
	if reader == nil {
		panic("review: NewBoard with a nil reader")
	}
	return &Board{reader: reader}
}

type BoardPage struct {
	Items      []domain.BoardItem `json:"items"`
	NextCursor *id.ID             `json:"next_cursor"`
}

func (b *Board) List(ctx context.Context, workspace, before id.ID, filters domain.BoardFilters, limit int) (BoardPage, error) {
	if workspace.IsZero() {
		return BoardPage{}, domain.ErrInvalid
	}
	if err := filters.Validate(); err != nil {
		return BoardPage{}, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	filters.Query = strings.TrimSpace(filters.Query)
	rows, err := b.reader.PageBoard(ctx, workspace, before, filters, limit+1)
	if err != nil {
		return BoardPage{}, err
	}
	out := BoardPage{Items: rows}
	if out.Items == nil {
		out.Items = []domain.BoardItem{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}
