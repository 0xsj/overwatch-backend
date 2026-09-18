package query

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/source/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type RetentionReader interface {
	RetentionPage(context.Context, id.ID, id.ID, int, string, time.Time) ([]domain.RetentionQueueItem, error)
}

type RetentionPage struct {
	Items      []domain.RetentionQueueItem `json:"items"`
	NextCursor *id.ID                      `json:"next_cursor"`
}

func (s *Sources) RetentionQueue(ctx context.Context, workspace, before id.ID, limit int, state string) (RetentionPage, error) {
	if workspace.IsZero() {
		return RetentionPage{}, domain.ErrInvalid
	}
	if !validRetentionState(state) {
		return RetentionPage{}, domain.ErrRetentionStateInvalid
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	reader, ok := s.reader.(RetentionReader)
	if !ok {
		return RetentionPage{}, domain.ErrInvalid
	}
	rows, err := reader.RetentionPage(ctx, workspace, before, limit+1, state, s.clock.Now())
	if err != nil {
		return RetentionPage{}, err
	}
	out := RetentionPage{Items: rows}
	if out.Items == nil {
		out.Items = []domain.RetentionQueueItem{}
	}
	if len(out.Items) > limit {
		cursor := out.Items[limit-1].Source.ID
		out.NextCursor = &cursor
		out.Items = out.Items[:limit]
	}
	return out, nil
}

func validRetentionState(state string) bool {
	switch state {
	case "", domain.RetentionUnscheduled, domain.RetentionScheduled, domain.RetentionDue, domain.RetentionBlocked, domain.RetentionHeld, domain.RetentionPurged:
		return true
	default:
		return false
	}
}
