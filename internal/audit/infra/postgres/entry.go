package postgres

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/audit/domain"
	"github.com/0xsj/overwatch-backend/internal/audit/infra/postgres/auditdb"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

func (s *Store) Append(ctx context.Context, e domain.Entry) (bool, error) {
	n, err := s.q(ctx).InsertEntry(ctx, auditdb.InsertEntryParams{
		ID:            uuid(e.ID),
		EventID:       uuid(e.EventID),
		Scope:         e.Scope.String(),
		Action:        e.Action,
		Subject:       e.Subject,
		Actor:         e.Actor,
		OnBehalfOf:    e.OnBehalfOf,
		WorkspaceID:   e.WorkspaceID,
		CorrelationID: maybe(e.Correlation),
		CausationID:   maybe(e.Causation),
		Detail:        e.Detail,
		OccurredAt:    stamp(e.OccurredAt),
		RecordedAt:    stamp(e.RecordedAt),
	})
	if err != nil {
		return false, postgres.Translate(ctx, err, "audit: append entry")
	}
	return n == 1, nil
}

func (s *Store) ByID(ctx context.Context, want id.ID) (domain.Entry, error) {
	row, err := s.q(ctx).EntryByID(ctx, uuid(want))
	if err != nil {
		translated := postgres.Translate(ctx, err, "audit: read entry")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Entry{}, fmt.Errorf("audit: read entry: %w", domain.ErrEntryGone)
		}
		return domain.Entry{}, translated
	}
	return entry(row)
}

func (s *Store) ForSubject(ctx context.Context, subject string, limit int32) ([]domain.Entry, error) {
	rows, err := s.q(ctx).EntriesForSubject(ctx, auditdb.EntriesForSubjectParams{Subject: subject, Limit: limit})
	return collect(ctx, rows, err, "audit: list by subject")
}

func (s *Store) ForActor(ctx context.Context, actor string, limit int32) ([]domain.Entry, error) {
	rows, err := s.q(ctx).EntriesForActor(ctx, auditdb.EntriesForActorParams{Actor: actor, Limit: limit})
	return collect(ctx, rows, err, "audit: list by actor")
}

func (s *Store) ForWorkspace(ctx context.Context, workspace string, limit int32) ([]domain.Entry, error) {
	rows, err := s.q(ctx).EntriesForWorkspace(ctx, auditdb.EntriesForWorkspaceParams{WorkspaceID: workspace, Limit: limit})
	return collect(ctx, rows, err, "audit: list by workspace")
}

func collect(ctx context.Context, rows []auditdb.AuditEntry, err error, op string) ([]domain.Entry, error) {
	if err != nil {
		return nil, postgres.Translate(ctx, err, op)
	}
	out := make([]domain.Entry, 0, len(rows))
	for _, row := range rows {
		e, err := entry(row)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}
