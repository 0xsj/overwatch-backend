package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/event/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	Page(context.Context, id.ID, id.ID, int) ([]domain.Event, error)
	ByID(context.Context, id.ID, id.ID) (domain.Event, error)
}

type Events struct{ reader Reader }

func NewEvents(reader Reader) *Events {
	if reader == nil {
		panic("event: NewEvents with a nil reader")
	}
	return &Events{reader: reader}
}

const (
	DefaultPage = 100
	MaxPage     = 100
)

type Page struct {
	Items      []domain.Event `json:"items"`
	NextCursor *id.ID         `json:"next_cursor"`
}

func (e *Events) List(ctx context.Context, workspace, before id.ID, limit int) (Page, error) {
	if workspace.IsZero() {
		return Page{}, domain.ErrWorkspaceRequired
	}
	if limit <= 0 {
		limit = DefaultPage
	}
	if limit > MaxPage {
		limit = MaxPage
	}
	rows, err := e.reader.Page(ctx, workspace, before, limit+1)
	if err != nil {
		return Page{}, err
	}
	out := Page{Items: rows}
	if out.Items == nil {
		out.Items = []domain.Event{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

func (e *Events) ByID(ctx context.Context, workspace, want id.ID) (domain.Event, error) {
	if workspace.IsZero() || want.IsZero() {
		return domain.Event{}, domain.ErrIDRequired
	}
	return e.reader.ByID(ctx, workspace, want)
}
