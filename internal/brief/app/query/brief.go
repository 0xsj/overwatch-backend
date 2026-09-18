package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/brief/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	ByWorkspace(context.Context, id.ID) (domain.Brief, error)
	SnapshotPage(context.Context, id.ID, id.ID, int) ([]domain.Snapshot, error)
	SnapshotByID(context.Context, id.ID, id.ID) (domain.Snapshot, error)
}
type Briefs struct{ reader Reader }

func NewBriefs(reader Reader) *Briefs {
	if reader == nil {
		panic("brief: NewBriefs with a nil reader")
	}
	return &Briefs{reader: reader}
}

func (b *Briefs) ByWorkspace(ctx context.Context, workspace id.ID) (domain.Brief, error) {
	if workspace.IsZero() {
		return domain.Brief{}, domain.ErrWorkspaceRequired
	}
	return b.reader.ByWorkspace(ctx, workspace)
}

const (
	DefaultSnapshotPage = 50
	MaxSnapshotPage     = 100
)

type SnapshotPage struct {
	Items      []domain.Snapshot `json:"items"`
	NextCursor *id.ID            `json:"next_cursor"`
}

func (b *Briefs) Snapshots(ctx context.Context, workspace, before id.ID, limit int) (SnapshotPage, error) {
	if workspace.IsZero() {
		return SnapshotPage{}, domain.ErrWorkspaceRequired
	}
	if limit <= 0 {
		limit = DefaultSnapshotPage
	}
	if limit > MaxSnapshotPage {
		limit = MaxSnapshotPage
	}
	rows, err := b.reader.SnapshotPage(ctx, workspace, before, limit+1)
	if err != nil {
		return SnapshotPage{}, err
	}
	out := SnapshotPage{Items: rows}
	if out.Items == nil {
		out.Items = []domain.Snapshot{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

func (b *Briefs) SnapshotByID(ctx context.Context, workspace, snapshot id.ID) (domain.Snapshot, error) {
	if workspace.IsZero() || snapshot.IsZero() {
		return domain.Snapshot{}, domain.ErrIDRequired
	}
	return b.reader.SnapshotByID(ctx, workspace, snapshot)
}
