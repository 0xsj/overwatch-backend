package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/researchentity/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	Page(context.Context, id.ID, id.ID, int) ([]domain.Record, error)
	ByID(context.Context, id.ID, id.ID) (domain.Record, error)
}

type Records struct{ reader Reader }

func NewRecords(reader Reader) *Records {
	if reader == nil {
		panic("researchentity: NewRecords with a nil reader")
	}
	return &Records{reader: reader}
}

const (
	DefaultPage = 50
	MaxPage     = 100
)

type Page struct {
	Items      []domain.Record `json:"items"`
	NextCursor *id.ID          `json:"next_cursor"`
}

func (r *Records) List(ctx context.Context, workspace, before id.ID, limit int) (Page, error) {
	if workspace.IsZero() {
		return Page{}, domain.ErrWorkspaceRequired
	}
	if limit <= 0 {
		limit = DefaultPage
	}
	if limit > MaxPage {
		limit = MaxPage
	}
	rows, err := r.reader.Page(ctx, workspace, before, limit+1)
	if err != nil {
		return Page{}, err
	}
	out := Page{Items: rows}
	if out.Items == nil {
		out.Items = []domain.Record{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

func (r *Records) ByID(ctx context.Context, workspace, want id.ID) (domain.Record, error) {
	if workspace.IsZero() || want.IsZero() {
		return domain.Record{}, domain.ErrIDRequired
	}
	return r.reader.ByID(ctx, workspace, want)
}
