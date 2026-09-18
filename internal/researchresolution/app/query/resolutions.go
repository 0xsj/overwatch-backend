package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/researchresolution/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	Page(context.Context, id.ID, id.ID, int) ([]domain.Resolution, error)
	ByID(context.Context, id.ID, id.ID) (domain.Resolution, error)
	ActiveByAlias(context.Context, id.ID, id.ID) (domain.Resolution, error)
}

type Resolutions struct{ reader Reader }

func NewResolutions(reader Reader) *Resolutions {
	if reader == nil {
		panic("researchresolution: NewResolutions with a nil reader")
	}
	return &Resolutions{reader: reader}
}

const (
	DefaultPage = 50
	MaxPage     = 100
)

type Page struct {
	Items      []domain.Resolution `json:"items"`
	NextCursor *id.ID              `json:"next_cursor"`
}

func (r *Resolutions) List(ctx context.Context, workspace, before id.ID, limit int) (Page, error) {
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
		out.Items = []domain.Resolution{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor, out.Items = &cursor, rows[:limit]
	}
	return out, nil
}

func (r *Resolutions) ByID(ctx context.Context, workspace, want id.ID) (domain.Resolution, error) {
	if workspace.IsZero() || want.IsZero() {
		return domain.Resolution{}, domain.ErrIDRequired
	}
	return r.reader.ByID(ctx, workspace, want)
}

func (r *Resolutions) ActiveByAlias(ctx context.Context, workspace, alias id.ID) (domain.Resolution, error) {
	if workspace.IsZero() || alias.IsZero() {
		return domain.Resolution{}, domain.ErrIDRequired
	}
	return r.reader.ActiveByAlias(ctx, workspace, alias)
}
