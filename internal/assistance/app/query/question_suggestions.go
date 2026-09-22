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

type VisibleQuestionSuggestionReader interface {
	PageQuestionSuggestionsVisible(context.Context, id.ID, id.ID, int, string) ([]domain.QuestionSuggestions, error)
	QuestionSuggestionsByIDVisible(context.Context, id.ID, id.ID, string) (domain.QuestionSuggestions, error)
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

func (q *QuestionSuggestions) ListVisible(ctx context.Context, workspace, before id.ID, limit int, maxSensitivity string) (QuestionSuggestionPage, error) {
	if workspace.IsZero() || !validMaxSensitivity(maxSensitivity) {
		return QuestionSuggestionPage{}, domain.ErrWorkspaceRequired
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	reader, ok := q.reader.(VisibleQuestionSuggestionReader)
	if !ok {
		return QuestionSuggestionPage{}, domain.ErrWorkspaceRequired
	}
	rows, err := reader.PageQuestionSuggestionsVisible(ctx, workspace, before, limit+1, maxSensitivity)
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

func (q *QuestionSuggestions) ByIDVisible(ctx context.Context, workspace, suggestion id.ID, maxSensitivity string) (domain.QuestionSuggestions, error) {
	if workspace.IsZero() || suggestion.IsZero() || !validMaxSensitivity(maxSensitivity) {
		return domain.QuestionSuggestions{}, domain.ErrIDRequired
	}
	reader, ok := q.reader.(VisibleQuestionSuggestionReader)
	if !ok {
		return domain.QuestionSuggestions{}, domain.ErrWorkspaceRequired
	}
	return reader.QuestionSuggestionsByIDVisible(ctx, workspace, suggestion, maxSensitivity)
}
