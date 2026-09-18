package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/review/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	PageEvidence(context.Context, id.ID, id.ID, int) ([]domain.Evidence, error)
	EvidenceByID(context.Context, id.ID, id.ID) (domain.Evidence, error)
	PageRelations(context.Context, id.ID, id.ID, int) ([]domain.Relation, error)
}

type Relations struct{ reader Reader }

func NewRelations(reader Reader) *Relations {
	if reader == nil {
		panic("review: NewRelations with a nil reader")
	}
	return &Relations{reader: reader}
}

type EvidencePage struct {
	Items      []domain.Evidence `json:"items"`
	NextCursor *id.ID            `json:"next_cursor"`
}

type RelationPage struct {
	Items      []domain.Relation `json:"items"`
	NextCursor *id.ID            `json:"next_cursor"`
}

func (r *Relations) Evidence(ctx context.Context, workspace, before id.ID, limit int) (EvidencePage, error) {
	if workspace.IsZero() {
		return EvidencePage{}, domain.ErrInvalid
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := r.reader.PageEvidence(ctx, workspace, before, limit+1)
	if err != nil {
		return EvidencePage{}, err
	}
	out := EvidencePage{Items: rows}
	if out.Items == nil {
		out.Items = []domain.Evidence{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

// EvidenceByID resolves a cited observation directly. The list is intentionally
// paged, but a frozen handoff must not lose its citation details just because a
// cited record falls beyond the first page.
func (r *Relations) EvidenceByID(ctx context.Context, workspace, want id.ID) (domain.Evidence, error) {
	if workspace.IsZero() || want.IsZero() {
		return domain.Evidence{}, domain.ErrInvalid
	}
	return r.reader.EvidenceByID(ctx, workspace, want)
}

func (r *Relations) Relations(ctx context.Context, workspace, before id.ID, limit int) (RelationPage, error) {
	if workspace.IsZero() {
		return RelationPage{}, domain.ErrInvalid
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := r.reader.PageRelations(ctx, workspace, before, limit+1)
	if err != nil {
		return RelationPage{}, err
	}
	out := RelationPage{Items: rows}
	if out.Items == nil {
		out.Items = []domain.Relation{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}
