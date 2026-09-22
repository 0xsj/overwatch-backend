package query

import (
	"context"
	"strings"

	"github.com/0xsj/overwatch-backend/internal/researchconnection/domain"
	recorddomain "github.com/0xsj/overwatch-backend/internal/researchentity/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	Page(context.Context, id.ID, id.ID, string, domain.Kind, recorddomain.Kind, domain.State, domain.ReviewFilter, int, string) ([]domain.Connection, error)
	ForRecord(context.Context, id.ID, id.ID, int, string) ([]domain.Connection, error)
	ByID(context.Context, id.ID, id.ID) (domain.Connection, error)
	ByIDVisible(context.Context, id.ID, id.ID, string) (domain.Connection, error)
	Revisions(context.Context, id.ID, id.ID, string) ([]domain.Revision, error)
	RevisionByID(context.Context, id.ID, id.ID, id.ID, string) (domain.Revision, error)
	Summary(context.Context, id.ID, string) (domain.BrowseSummary, error)
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

func (c *Connections) List(ctx context.Context, workspace, before id.ID, search string, kind domain.Kind, recordKind recorddomain.Kind, state domain.State, review domain.ReviewFilter, limit int, maxSensitivity string) (Page, error) {
	if workspace.IsZero() || !validMaxSensitivity(maxSensitivity) {
		return Page{}, domain.ErrInvalid
	}
	search = strings.TrimSpace(search)
	if len(search) > 200 {
		return Page{}, domain.ErrSearchTooLong
	}
	parsedKind := kind
	if kind != "" {
		var err error
		parsedKind, err = domain.ParseKind(kind.String())
		if err != nil {
			return Page{}, err
		}
	}
	parsedRecordKind := recordKind
	if recordKind != "" {
		var err error
		parsedRecordKind, err = recorddomain.ParseKind(recordKind.String())
		if err != nil {
			return Page{}, err
		}
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
	rows, err := c.reader.Page(ctx, workspace, before, search, parsedKind, parsedRecordKind, parsedState, parsedReview, limit+1, maxSensitivity)
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

// ForRecord returns the bounded relationship neighborhood around one authored
// record. It is intentionally a separate read from List: a record detail
// surface must not fetch an unbounded workspace-wide relationship page and
// filter it in memory.
func (c *Connections) ForRecord(ctx context.Context, workspace, record id.ID, limit int, maxSensitivity string) ([]domain.Connection, error) {
	if workspace.IsZero() || record.IsZero() || !validMaxSensitivity(maxSensitivity) {
		return nil, domain.ErrInvalid
	}
	if limit <= 0 {
		limit = DefaultPage
	}
	if limit > MaxPage {
		limit = MaxPage
	}
	rows, err := c.reader.ForRecord(ctx, workspace, record, limit, maxSensitivity)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return []domain.Connection{}, nil
	}
	return rows, nil
}

func (c *Connections) Summary(ctx context.Context, workspace id.ID, maxSensitivity string) (domain.BrowseSummary, error) {
	if workspace.IsZero() || !validMaxSensitivity(maxSensitivity) {
		return domain.BrowseSummary{}, domain.ErrInvalid
	}
	return c.reader.Summary(ctx, workspace, maxSensitivity)
}

func (c *Connections) ByID(ctx context.Context, workspace, want id.ID) (domain.Connection, error) {
	if workspace.IsZero() || want.IsZero() {
		return domain.Connection{}, domain.ErrInvalid
	}
	return c.reader.ByID(ctx, workspace, want)
}

func (c *Connections) ByIDVisible(ctx context.Context, workspace, want id.ID, maxSensitivity string) (domain.Connection, error) {
	if workspace.IsZero() || want.IsZero() || !validMaxSensitivity(maxSensitivity) {
		return domain.Connection{}, domain.ErrInvalid
	}
	return c.reader.ByIDVisible(ctx, workspace, want, maxSensitivity)
}

func (c *Connections) Revisions(ctx context.Context, workspace, want id.ID, maxSensitivity string) ([]domain.Revision, error) {
	if workspace.IsZero() || want.IsZero() || !validMaxSensitivity(maxSensitivity) {
		return nil, domain.ErrInvalid
	}
	rows, err := c.reader.Revisions(ctx, workspace, want, maxSensitivity)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return []domain.Revision{}, nil
	}
	return rows, nil
}

func (c *Connections) RevisionByID(ctx context.Context, workspace, connection, revision id.ID, maxSensitivity string) (domain.Revision, error) {
	if workspace.IsZero() || connection.IsZero() || revision.IsZero() || !validMaxSensitivity(maxSensitivity) {
		return domain.Revision{}, domain.ErrInvalid
	}
	return c.reader.RevisionByID(ctx, workspace, connection, revision, maxSensitivity)
}

func validMaxSensitivity(value string) bool {
	return value == "internal" || value == "restricted"
}
