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
	Impact(context.Context, id.ID, id.ID) (domain.Impact, error)
}

type SetReader interface {
	PageSets(context.Context, id.ID, id.ID, int) ([]domain.ResolutionSet, error)
	BySetID(context.Context, id.ID, id.ID) (domain.ResolutionSet, error)
	ImpactSet(context.Context, id.ID, id.ID) (domain.Impact, error)
}

type Resolutions struct {
	reader Reader
	sets   SetReader
}

func NewResolutions(reader Reader, setReader ...SetReader) *Resolutions {
	if reader == nil {
		panic("researchresolution: NewResolutions with a nil reader")
	}
	var sets SetReader
	if len(setReader) > 0 {
		sets = setReader[0]
	}
	return &Resolutions{reader: reader, sets: sets}
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

func (r *Resolutions) Impact(ctx context.Context, workspace, resolution id.ID) (domain.Impact, error) {
	if workspace.IsZero() || resolution.IsZero() {
		return domain.Impact{}, domain.ErrIDRequired
	}
	return r.reader.Impact(ctx, workspace, resolution)
}

type SetPage struct {
	Items      []domain.ResolutionSet `json:"items"`
	NextCursor *id.ID                 `json:"next_cursor"`
}

func (r *Resolutions) ListSets(ctx context.Context, workspace, before id.ID, limit int) (SetPage, error) {
	if r.sets == nil {
		return SetPage{}, domain.ErrNotFound
	}
	if workspace.IsZero() {
		return SetPage{}, domain.ErrWorkspaceRequired
	}
	if limit <= 0 {
		limit = DefaultPage
	}
	if limit > MaxPage {
		limit = MaxPage
	}
	rows, err := r.sets.PageSets(ctx, workspace, before, limit+1)
	if err != nil {
		return SetPage{}, err
	}
	out := SetPage{Items: rows}
	if out.Items == nil {
		out.Items = []domain.ResolutionSet{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor, out.Items = &cursor, rows[:limit]
	}
	return out, nil
}

func (r *Resolutions) SetByID(ctx context.Context, workspace, want id.ID) (domain.ResolutionSet, error) {
	if r.sets == nil {
		return domain.ResolutionSet{}, domain.ErrNotFound
	}
	if workspace.IsZero() || want.IsZero() {
		return domain.ResolutionSet{}, domain.ErrIDRequired
	}
	return r.sets.BySetID(ctx, workspace, want)
}

func (r *Resolutions) SetImpact(ctx context.Context, workspace, want id.ID) (domain.Impact, error) {
	if r.sets == nil {
		return domain.Impact{}, domain.ErrNotFound
	}
	if workspace.IsZero() || want.IsZero() {
		return domain.Impact{}, domain.ErrIDRequired
	}
	return r.sets.ImpactSet(ctx, workspace, want)
}
