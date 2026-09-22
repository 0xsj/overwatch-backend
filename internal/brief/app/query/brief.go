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
	SnapshotReview(context.Context, id.ID, id.ID) (domain.SnapshotReview, error)
	SnapshotComments(context.Context, id.ID, id.ID) ([]domain.SnapshotComment, error)
	SnapshotHandoffShares(context.Context, id.ID, id.ID) ([]domain.HandoffShare, error)
	SnapshotByHandoffShare(context.Context, id.ID, string) (domain.Snapshot, error)
}

type VisibleReader interface {
	ByWorkspaceVisible(context.Context, id.ID, string) (domain.Brief, error)
	SnapshotPageVisible(context.Context, id.ID, id.ID, int, string) ([]domain.Snapshot, error)
	SnapshotByIDVisible(context.Context, id.ID, id.ID, string) (domain.Snapshot, error)
	SnapshotByHandoffShareVisible(context.Context, id.ID, string, string) (domain.Snapshot, error)
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

func (b *Briefs) SnapshotReview(ctx context.Context, workspace, snapshot id.ID) (domain.SnapshotReview, error) {
	if workspace.IsZero() || snapshot.IsZero() {
		return domain.SnapshotReview{}, domain.ErrIDRequired
	}
	return b.reader.SnapshotReview(ctx, workspace, snapshot)
}

func (b *Briefs) SnapshotComments(ctx context.Context, workspace, snapshot id.ID) ([]domain.SnapshotComment, error) {
	if workspace.IsZero() || snapshot.IsZero() {
		return nil, domain.ErrIDRequired
	}
	return b.reader.SnapshotComments(ctx, workspace, snapshot)
}

func (b *Briefs) SnapshotHandoffShares(ctx context.Context, workspace, snapshot id.ID) ([]domain.HandoffShare, error) {
	if workspace.IsZero() || snapshot.IsZero() {
		return nil, domain.ErrIDRequired
	}
	return b.reader.SnapshotHandoffShares(ctx, workspace, snapshot)
}

func (b *Briefs) SnapshotByHandoffShare(ctx context.Context, workspace id.ID, tokenDigest string) (domain.Snapshot, error) {
	if workspace.IsZero() || tokenDigest == "" {
		return domain.Snapshot{}, domain.ErrIDRequired
	}
	return b.reader.SnapshotByHandoffShare(ctx, workspace, tokenDigest)
}

func (b *Briefs) ByWorkspaceVisible(ctx context.Context, workspace id.ID, maxSensitivity string) (domain.Brief, error) {
	if workspace.IsZero() || !validMaxSensitivity(maxSensitivity) {
		return domain.Brief{}, domain.ErrWorkspaceRequired
	}
	reader, ok := b.reader.(VisibleReader)
	if !ok {
		return domain.Brief{}, domain.ErrWorkspaceRequired
	}
	return reader.ByWorkspaceVisible(ctx, workspace, maxSensitivity)
}

func (b *Briefs) SnapshotsVisible(ctx context.Context, workspace, before id.ID, limit int, maxSensitivity string) (SnapshotPage, error) {
	if workspace.IsZero() || !validMaxSensitivity(maxSensitivity) {
		return SnapshotPage{}, domain.ErrWorkspaceRequired
	}
	if limit <= 0 {
		limit = DefaultSnapshotPage
	}
	if limit > MaxSnapshotPage {
		limit = MaxSnapshotPage
	}
	reader, ok := b.reader.(VisibleReader)
	if !ok {
		return SnapshotPage{}, domain.ErrWorkspaceRequired
	}
	rows, err := reader.SnapshotPageVisible(ctx, workspace, before, limit+1, maxSensitivity)
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

func (b *Briefs) SnapshotByIDVisible(ctx context.Context, workspace, snapshot id.ID, maxSensitivity string) (domain.Snapshot, error) {
	if workspace.IsZero() || snapshot.IsZero() || !validMaxSensitivity(maxSensitivity) {
		return domain.Snapshot{}, domain.ErrIDRequired
	}
	reader, ok := b.reader.(VisibleReader)
	if !ok {
		return domain.Snapshot{}, domain.ErrWorkspaceRequired
	}
	return reader.SnapshotByIDVisible(ctx, workspace, snapshot, maxSensitivity)
}

func (b *Briefs) SnapshotByHandoffShareVisible(ctx context.Context, workspace id.ID, tokenDigest, maxSensitivity string) (domain.Snapshot, error) {
	if workspace.IsZero() || tokenDigest == "" || !validMaxSensitivity(maxSensitivity) {
		return domain.Snapshot{}, domain.ErrIDRequired
	}
	reader, ok := b.reader.(VisibleReader)
	if !ok {
		return domain.Snapshot{}, domain.ErrWorkspaceRequired
	}
	return reader.SnapshotByHandoffShareVisible(ctx, workspace, tokenDigest, maxSensitivity)
}
