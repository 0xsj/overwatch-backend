package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/observation/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type ManualReader interface {
	ByIDManual(ctx context.Context, workspace, source, want id.ID) (domain.Manual, error)
	PageManual(ctx context.Context, workspace, source, before id.ID, limit int) ([]domain.Manual, error)
}
type citationShareReader interface {
	CitationShareByDigest(ctx context.Context, workspace id.ID, digest string) (domain.CitationShare, error)
	CitationSharesForObservation(ctx context.Context, workspace, source, observation id.ID) ([]domain.CitationShare, error)
}
type ManualObservations struct{ reader ManualReader }

func NewManualObservations(reader ManualReader) *ManualObservations {
	if reader == nil {
		panic("observation: NewManualObservations with a nil reader")
	}
	return &ManualObservations{reader}
}

type ManualPage struct {
	Items      []domain.Manual `json:"items"`
	NextCursor *id.ID          `json:"next_cursor"`
}

func (m *ManualObservations) ForSource(ctx context.Context, workspace, source, before id.ID, limit int) (ManualPage, error) {
	if workspace.IsZero() || source.IsZero() {
		return ManualPage{}, domain.ErrIDRequired
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := m.reader.PageManual(ctx, workspace, source, before, limit+1)
	if err != nil {
		return ManualPage{}, err
	}
	out := ManualPage{Items: rows}
	if out.Items == nil {
		out.Items = []domain.Manual{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

// ByID opens one exact citation independently of the currently loaded page.
func (m *ManualObservations) ByID(ctx context.Context, workspace, source, want id.ID) (domain.Manual, error) {
	if workspace.IsZero() || source.IsZero() || want.IsZero() {
		return domain.Manual{}, domain.ErrIDRequired
	}
	return m.reader.ByIDManual(ctx, workspace, source, want)
}

func (m *ManualObservations) CitationShareByDigest(ctx context.Context, workspace id.ID, digest string) (domain.CitationShare, error) {
	if workspace.IsZero() || digest == "" {
		return domain.CitationShare{}, domain.ErrIDRequired
	}
	reader, ok := m.reader.(citationShareReader)
	if !ok {
		return domain.CitationShare{}, domain.ErrNotFound
	}
	return reader.CitationShareByDigest(ctx, workspace, digest)
}

func (m *ManualObservations) CitationShares(ctx context.Context, workspace, source, observation id.ID) ([]domain.CitationShare, error) {
	if workspace.IsZero() || source.IsZero() || observation.IsZero() {
		return nil, domain.ErrIDRequired
	}
	reader, ok := m.reader.(citationShareReader)
	if !ok {
		return nil, domain.ErrNotFound
	}
	rows, err := reader.CitationSharesForObservation(ctx, workspace, source, observation)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return []domain.CitationShare{}, nil
	}
	return rows, nil
}
