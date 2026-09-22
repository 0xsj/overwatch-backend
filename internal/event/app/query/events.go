package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/event/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	Page(context.Context, id.ID, id.ID, int) ([]domain.Event, error)
	ForRecord(context.Context, id.ID, id.ID, int, string) ([]domain.Event, error)
	ByID(context.Context, id.ID, id.ID) (domain.Event, error)
	Revisions(context.Context, id.ID, id.ID) ([]domain.Revision, error)
	RevisionByID(context.Context, id.ID, id.ID, id.ID) (domain.Revision, error)
}

type Events struct{ reader Reader }

func NewEvents(reader Reader) *Events {
	if reader == nil {
		panic("event: NewEvents with a nil reader")
	}
	return &Events{reader: reader}
}

const (
	DefaultPage = 100
	MaxPage     = 100
)

type Page struct {
	Items      []domain.Event `json:"items"`
	NextCursor *id.ID         `json:"next_cursor"`
}

func (e *Events) List(ctx context.Context, workspace, before id.ID, limit int) (Page, error) {
	if workspace.IsZero() {
		return Page{}, domain.ErrWorkspaceRequired
	}
	if limit <= 0 {
		limit = DefaultPage
	}
	if limit > MaxPage {
		limit = MaxPage
	}
	rows, err := e.reader.Page(ctx, workspace, before, limit+1)
	if err != nil {
		return Page{}, err
	}
	out := Page{Items: rows}
	if out.Items == nil {
		out.Items = []domain.Event{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

// ForRecord returns the bounded timeline neighborhood for one authored
// record. Timeline rows remain workspace-scoped and are hidden when their
// citations or linked records depend on restricted sources.
func (e *Events) ForRecord(ctx context.Context, workspace, record id.ID, limit int, maxSensitivity string) ([]domain.Event, error) {
	if workspace.IsZero() || record.IsZero() || !validMaxSensitivity(maxSensitivity) {
		return nil, domain.ErrWorkspaceRequired
	}
	if limit <= 0 {
		limit = DefaultPage
	}
	if limit > MaxPage {
		limit = MaxPage
	}
	rows, err := e.reader.ForRecord(ctx, workspace, record, limit, maxSensitivity)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return []domain.Event{}, nil
	}
	return rows, nil
}

func validMaxSensitivity(value string) bool {
	return value == "internal" || value == "restricted"
}

func (e *Events) ByID(ctx context.Context, workspace, want id.ID) (domain.Event, error) {
	if workspace.IsZero() || want.IsZero() {
		return domain.Event{}, domain.ErrIDRequired
	}
	return e.reader.ByID(ctx, workspace, want)
}

func (e *Events) Revisions(ctx context.Context, workspace, want id.ID) ([]domain.Revision, error) {
	if workspace.IsZero() || want.IsZero() {
		return nil, domain.ErrIDRequired
	}
	rows, err := e.reader.Revisions(ctx, workspace, want)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return []domain.Revision{}, nil
	}
	return rows, nil
}

func (e *Events) RevisionByID(ctx context.Context, workspace, event, revision id.ID) (domain.Revision, error) {
	if workspace.IsZero() || event.IsZero() || revision.IsZero() {
		return domain.Revision{}, domain.ErrIDRequired
	}
	return e.reader.RevisionByID(ctx, workspace, event, revision)
}
