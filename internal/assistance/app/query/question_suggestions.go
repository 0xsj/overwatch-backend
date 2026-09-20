package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type QuestionSuggestionReader interface {
	PageQuestionSuggestions(context.Context, id.ID, id.ID, int) ([]domain.QuestionSuggestions, error)
	QuestionSuggestionsByID(context.Context, id.ID, id.ID) (domain.QuestionSuggestions, error)
}

type QuestionSuggestions struct{ reader QuestionSuggestionReader }

func NewQuestionSuggestions(reader QuestionSuggestionReader) *QuestionSuggestions {
	if reader == nil {
		panic("assistance: NewQuestionSuggestions with a nil reader")
	}
	return &QuestionSuggestions{reader: reader}
}

type QuestionSuggestionPage struct {
	Items      []domain.QuestionSuggestions `json:"items"`
	NextCursor *id.ID                       `json:"next_cursor"`
}

func (q *QuestionSuggestions) List(ctx context.Context, workspace, before id.ID, limit int) (QuestionSuggestionPage, error) {
	if workspace.IsZero() {
		return QuestionSuggestionPage{}, domain.ErrWorkspaceRequired
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	rows, err := q.reader.PageQuestionSuggestions(ctx, workspace, before, limit+1)
	if err != nil {
		return QuestionSuggestionPage{}, err
	}
	out := QuestionSuggestionPage{Items: rows}
	if out.Items == nil {
		out.Items = []domain.QuestionSuggestions{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

func (q *QuestionSuggestions) ByID(ctx context.Context, workspace, suggestion id.ID) (domain.QuestionSuggestions, error) {
	if workspace.IsZero() || suggestion.IsZero() {
		return domain.QuestionSuggestions{}, domain.ErrIDRequired
	}
	return q.reader.QuestionSuggestionsByID(ctx, workspace, suggestion)
}
