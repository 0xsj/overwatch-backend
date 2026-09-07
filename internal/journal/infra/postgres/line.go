package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/journal/domain"
	"github.com/0xsj/overwatch-backend/internal/journal/infra/postgres/journaldb"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

func (s *Store) Append(ctx context.Context, l domain.Line) (bool, error) {
	n, err := s.q(ctx).InsertLine(ctx, journaldb.InsertLineParams{
		ID:            uuid(l.ID),
		EventID:       uuid(l.EventID),
		Action:        l.Action,
		Subject:       l.Subject,
		Origin:        l.Origin,
		Actor:         l.Actor,
		OnBehalfOf:    l.OnBehalfOf,
		WorkspaceID:   l.WorkspaceID,
		Depth:         int32(l.Depth),
		Attempt:       int32(l.Attempt),
		Decision:      l.Decision,
		CorrelationID: maybe(l.Correlation),
		CausationID:   maybe(l.Causation),
		Detail:        l.Detail,
		OccurredAt:    stamp(l.OccurredAt),
		RecordedAt:    stamp(l.RecordedAt),
	})
	if err != nil {
		return false, postgres.Translate(ctx, err, "journal: append line")
	}
	return n == 1, nil
}

func (s *Store) ByID(ctx context.Context, want id.ID) (domain.Line, error) {
	row, err := s.q(ctx).LineByID(ctx, uuid(want))
	if err != nil {
		translated := postgres.Translate(ctx, err, "journal: read line")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Line{}, fmt.Errorf("journal: read line: %w", domain.ErrLineGone)
		}
		return domain.Line{}, translated
	}
	return line(row), nil
}

func (s *Store) Recent(ctx context.Context, limit int32) ([]domain.Line, error) {
	rows, err := s.q(ctx).RecentLines(ctx, limit)
	return collect(ctx, rows, err, "journal: recent")
}

func (s *Store) ForCorrelation(ctx context.Context, correlation id.ID) ([]domain.Line, error) {
	rows, err := s.q(ctx).LinesForCorrelation(ctx, maybe(correlation))
	return collect(ctx, rows, err, "journal: chain")
}

func (s *Store) ForOrigin(ctx context.Context, origin string, limit int32) ([]domain.Line, error) {
	rows, err := s.q(ctx).LinesForOrigin(ctx, journaldb.LinesForOriginParams{Origin: origin, Limit: limit})
	return collect(ctx, rows, err, "journal: by origin")
}

// ExpireBefore deletes at most `batch` work lines older than the cutoff and
// reports how many went. It NEVER deletes a decision — decisions/0022 — and the
// caller repeats until a pass deletes fewer than a full batch.
func (s *Store) ExpireBefore(ctx context.Context, cutoff time.Time, batch int) (int, error) {
	n, err := s.q(ctx).ExpireLinesBefore(ctx, journaldb.ExpireLinesBeforeParams{
		OccurredAt: stamp(cutoff),
		Limit:      int32(batch),
	})
	if err != nil {
		return 0, postgres.Translate(ctx, err, "journal: expire")
	}
	return int(n), nil
}

func collect(ctx context.Context, rows []journaldb.JournalLine, err error, op string) ([]domain.Line, error) {
	if err != nil {
		return nil, postgres.Translate(ctx, err, op)
	}
	out := make([]domain.Line, 0, len(rows))
	for _, row := range rows {
		out = append(out, line(row))
	}
	return out, nil
}
