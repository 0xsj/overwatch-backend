package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type BriefDraftReader interface {
	PageBriefDrafts(context.Context, id.ID, id.ID, int) ([]domain.BriefDraft, error)
	BriefDraftByID(context.Context, id.ID, id.ID) (domain.BriefDraft, error)
}

type BriefDrafts struct{ reader BriefDraftReader }

func NewBriefDrafts(reader BriefDraftReader) *BriefDrafts {
	if reader == nil {
		panic("assistance: NewBriefDrafts with a nil reader")
	}
	return &BriefDrafts{reader: reader}
}

type BriefDraftPage struct {
	Items      []domain.BriefDraft `json:"items"`
	NextCursor *id.ID              `json:"next_cursor"`
}

func (b *BriefDrafts) List(ctx context.Context, workspace, before id.ID, limit int) (BriefDraftPage, error) {
	if workspace.IsZero() {
		return BriefDraftPage{}, domain.ErrWorkspaceRequired
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	rows, err := b.reader.PageBriefDrafts(ctx, workspace, before, limit+1)
	if err != nil {
		return BriefDraftPage{}, err
	}
	out := BriefDraftPage{Items: rows}
	if out.Items == nil {
		out.Items = []domain.BriefDraft{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

func (b *BriefDrafts) ByID(ctx context.Context, workspace, draft id.ID) (domain.BriefDraft, error) {
	if workspace.IsZero() || draft.IsZero() {
		return domain.BriefDraft{}, domain.ErrIDRequired
	}
	return b.reader.BriefDraftByID(ctx, workspace, draft)
}
