package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/event/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type RelationshipReader interface {
	PageRelationships(context.Context, id.ID, id.ID, int) ([]domain.Relationship, error)
	RelationshipByID(context.Context, id.ID, id.ID) (domain.Relationship, error)
}

type Relationships struct{ reader RelationshipReader }

func NewRelationships(reader RelationshipReader) *Relationships {
	if reader == nil {
		panic("event: NewRelationships with a nil reader")
	}
	return &Relationships{reader: reader}
}

type RelationshipPage struct {
	Items      []domain.Relationship `json:"items"`
	NextCursor *id.ID                `json:"next_cursor"`
}

func (r *Relationships) List(ctx context.Context, workspace, before id.ID, limit int) (RelationshipPage, error) {
	if workspace.IsZero() {
		return RelationshipPage{}, domain.ErrWorkspaceRequired
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := r.reader.PageRelationships(ctx, workspace, before, limit+1)
	if err != nil {
		return RelationshipPage{}, err
	}
	out := RelationshipPage{Items: rows}
	if out.Items == nil {
		out.Items = []domain.Relationship{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

func (r *Relationships) ByID(ctx context.Context, workspace, relationship id.ID) (domain.Relationship, error) {
	if workspace.IsZero() || relationship.IsZero() {
		return domain.Relationship{}, domain.ErrIDRequired
	}
	return r.reader.RelationshipByID(ctx, workspace, relationship)
}
