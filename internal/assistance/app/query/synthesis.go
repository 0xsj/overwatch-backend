package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type SynthesisReader interface {
	PageSynthesis(context.Context, id.ID, id.ID, int) ([]domain.Synthesis, error)
	SynthesisByID(context.Context, id.ID, id.ID) (domain.Synthesis, error)
}

type Syntheses struct{ reader SynthesisReader }

func NewSyntheses(reader SynthesisReader) *Syntheses {
	if reader == nil {
		panic("assistance: NewSyntheses with a nil reader")
	}
	return &Syntheses{reader: reader}
}

type SynthesisPage struct {
	Items      []domain.Synthesis `json:"items"`
	NextCursor *id.ID             `json:"next_cursor"`
}

func (s *Syntheses) List(ctx context.Context, workspace, before id.ID, limit int) (SynthesisPage, error) {
	if workspace.IsZero() {
		return SynthesisPage{}, domain.ErrWorkspaceRequired
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	rows, err := s.reader.PageSynthesis(ctx, workspace, before, limit+1)
	if err != nil {
		return SynthesisPage{}, err
	}
	out := SynthesisPage{Items: rows}
	if out.Items == nil {
		out.Items = []domain.Synthesis{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

func (s *Syntheses) ByID(ctx context.Context, workspace, synthesis id.ID) (domain.Synthesis, error) {
	if workspace.IsZero() || synthesis.IsZero() {
		return domain.Synthesis{}, domain.ErrIDRequired
	}
	return s.reader.SynthesisByID(ctx, workspace, synthesis)
}
