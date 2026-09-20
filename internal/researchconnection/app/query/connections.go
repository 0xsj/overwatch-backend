package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/researchconnection/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	Page(context.Context, id.ID, id.ID, domain.State, domain.ReviewFilter, int) ([]domain.Connection, error)
	ByID(context.Context, id.ID, id.ID) (domain.Connection, error)
	Revisions(context.Context, id.ID, id.ID) ([]domain.Revision, error)
	RevisionByID(context.Context, id.ID, id.ID, id.ID) (domain.Revision, error)
	Summary(context.Context, id.ID) (domain.BrowseSummary, error)
}

type Connections struct{ reader Reader }

func NewConnections(reader Reader) *Connections {
	if reader == nil {
		panic("researchconnection: NewConnections with a nil reader")
	}
	return &Connections{reader: reader}
}

const (
	DefaultPage = 50
	MaxPage     = 100
)

type Page struct {
	Items      []domain.Connection
	NextCursor *id.ID
}

func (c *Connections) List(ctx context.Context, workspace, before id.ID, state domain.State, review domain.ReviewFilter, limit int) (Page, error) {
	if workspace.IsZero() {
		return Page{}, domain.ErrInvalid
	}
	parsedState := state
	if state != "" {
		var err error
		parsedState, err = domain.ParseState(state.String())
		if err != nil {
			return Page{}, err
		}
	}
	parsedReview, err := domain.ParseReviewFilter(review.String())
	if err != nil {
		return Page{}, err
	}
	if limit <= 0 {
		limit = DefaultPage
	}
	if limit > MaxPage {
		limit = MaxPage
	}
	rows, err := c.reader.Page(ctx, workspace, before, parsedState, parsedReview, limit+1)
	if err != nil {
		return Page{}, err
	}
	out := Page{Items: rows}
	if out.Items == nil {
		out.Items = []domain.Connection{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

func (c *Connections) Summary(ctx context.Context, workspace id.ID) (domain.BrowseSummary, error) {
	if workspace.IsZero() {
		return domain.BrowseSummary{}, domain.ErrInvalid
	}
	return c.reader.Summary(ctx, workspace)
}

func (c *Connections) ByID(ctx context.Context, workspace, want id.ID) (domain.Connection, error) {
	if workspace.IsZero() || want.IsZero() {
		return domain.Connection{}, domain.ErrInvalid
	}
	return c.reader.ByID(ctx, workspace, want)
}

func (c *Connections) Revisions(ctx context.Context, workspace, want id.ID) ([]domain.Revision, error) {
	if workspace.IsZero() || want.IsZero() {
		return nil, domain.ErrInvalid
	}
	rows, err := c.reader.Revisions(ctx, workspace, want)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return []domain.Revision{}, nil
	}
	return rows, nil
}

func (c *Connections) RevisionByID(ctx context.Context, workspace, connection, revision id.ID) (domain.Revision, error) {
	if workspace.IsZero() || connection.IsZero() || revision.IsZero() {
		return domain.Revision{}, domain.ErrInvalid
	}
	return c.reader.RevisionByID(ctx, workspace, connection, revision)
}
