package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/observation/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type ManualReader interface {
	ByIDManual(ctx context.Context, workspace, source, want id.ID) (domain.Manual, error)
	PageManual(ctx context.Context, workspace, source, before id.ID, limit int) ([]domain.Manual, error)
}
type ManualObservations struct{ reader ManualReader }

func NewManualObservations(reader ManualReader) *ManualObservations {
	if reader == nil {
		panic("observation: NewManualObservations with a nil reader")
	}
	return &ManualObservations{reader}
}

type ManualPage struct {
	Items      []domain.Manual `json:"items"`
	NextCursor *id.ID          `json:"next_cursor"`
}

func (m *ManualObservations) ForSource(ctx context.Context, workspace, source, before id.ID, limit int) (ManualPage, error) {
	if workspace.IsZero() || source.IsZero() {
		return ManualPage{}, domain.ErrIDRequired
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := m.reader.PageManual(ctx, workspace, source, before, limit+1)
	if err != nil {
		return ManualPage{}, err
	}
	out := ManualPage{Items: rows}
	if out.Items == nil {
		out.Items = []domain.Manual{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

// ByID opens one exact citation independently of the currently loaded page.
func (m *ManualObservations) ByID(ctx context.Context, workspace, source, want id.ID) (domain.Manual, error) {
	if workspace.IsZero() || source.IsZero() || want.IsZero() {
		return domain.Manual{}, domain.ErrIDRequired
	}
	return m.reader.ByIDManual(ctx, workspace, source, want)
}
