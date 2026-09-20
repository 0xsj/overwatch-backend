package query

import (
	"context"
	"strings"

	"github.com/0xsj/overwatch-backend/internal/lead/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	Page(context.Context, id.ID, id.ID, int) ([]domain.Question, error)
	PageByState(context.Context, id.ID, id.ID, domain.State, int) ([]domain.Question, error)
	ByID(context.Context, id.ID, id.ID) (domain.Question, error)
}

type Questions struct{ reader Reader }

func NewQuestions(reader Reader) *Questions {
	if reader == nil {
		panic("lead: NewQuestions with a nil reader")
	}
	return &Questions{reader: reader}
}

const (
	DefaultPage = 50
	MaxPage     = 100
)

type Page struct {
	Items      []domain.Question `json:"items"`
	NextCursor *id.ID            `json:"next_cursor"`
}

func (q *Questions) List(ctx context.Context, workspace, before id.ID, limit int) (Page, error) {
	if workspace.IsZero() {
		return Page{}, domain.ErrWorkspaceRequired
	}
	if limit <= 0 {
		limit = DefaultPage
	}
	if limit > MaxPage {
		limit = MaxPage
	}
	rows, err := q.reader.Page(ctx, workspace, before, limit+1)
	if err != nil {
		return Page{}, err
	}
	out := Page{Items: rows}
	if out.Items == nil {
		out.Items = []domain.Question{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

// Filtered is the status queue read. Empty state deliberately falls back to
// the complete list; a supplied state must be vocabulary-checked before it
// reaches persistence.
func (q *Questions) Filtered(ctx context.Context, workspace, before id.ID, state string, limit int) (Page, error) {
	if workspace.IsZero() {
		return Page{}, domain.ErrWorkspaceRequired
	}
	parsed, err := domain.ParseState(strings.TrimSpace(state))
	if err != nil {
		return Page{}, err
	}
	if limit <= 0 {
		limit = DefaultPage
	}
	if limit > MaxPage {
		limit = MaxPage
	}
	rows, err := q.reader.PageByState(ctx, workspace, before, parsed, limit+1)
	if err != nil {
		return Page{}, err
	}
	out := Page{Items: rows}
	if out.Items == nil {
		out.Items = []domain.Question{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

func (q *Questions) ByID(ctx context.Context, workspace, want id.ID) (domain.Question, error) {
	if workspace.IsZero() || want.IsZero() {
		return domain.Question{}, domain.ErrIDRequired
	}
	return q.reader.ByID(ctx, workspace, want)
}
