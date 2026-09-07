package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

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
		OrgID:         e.OrgID,
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

// Cursor names the last entry a reader saw. Zero means "from the head", which is
// why it is a value rather than a pointer: a caller that forgets to set it gets
// the first page rather than a nil dereference.
type Cursor struct {
	OccurredAt time.Time
	ID         id.ID
}

// IsZero means "from the head". It is exported because the read side branches on
// it to decide whether this is the first page, which is when facets are counted.
func (c Cursor) IsZero() bool { return c.OccurredAt.IsZero() }

func (s *Store) PageForSubject(ctx context.Context, subject string, after Cursor, limit int32) ([]domain.Entry, error) {
	rows, err := s.q(ctx).EntriesForSubjectPage(ctx, auditdb.EntriesForSubjectPageParams{
		Subject: subject,
		Column2: bound(after),
		Column3: cursorID(after),
		Limit:   limit,
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "audit: page for subject")
	}
	return entries(rows)
}

func (s *Store) PageForWorkspace(ctx context.Context, workspace string, after Cursor, limit int32) ([]domain.Entry, error) {
	rows, err := s.q(ctx).EntriesForWorkspacePage(ctx, auditdb.EntriesForWorkspacePageParams{
		WorkspaceID: workspace,
		Column2:     bound(after),
		Column3:     cursorID(after),
		Limit:       limit,
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "audit: page for workspace")
	}
	return entries(rows)
}

// bound is NULL for the first page. The query reads `$2 is null or ...`, so a
// null timestamp means no lower bound rather than a bound at the zero time —
// which would match nothing and return an empty first page.
func bound(c Cursor) pgtype.Timestamptz {
	if c.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: c.OccurredAt, Valid: true}
}

func cursorID(c Cursor) pgtype.UUID {
	if c.IsZero() {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: c.ID, Valid: true}
}

func entries(rows []auditdb.AuditEntry) ([]domain.Entry, error) {
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

// Facet is one bucket of the action's first segment, with how many entries fall
// in it.
type Facet struct {
	Name  string
	Total int
}

func (s *Store) FacetsForSubject(ctx context.Context, subject string) ([]Facet, error) {
	rows, err := s.q(ctx).FacetsForSubject(ctx, subject)
	if err != nil {
		return nil, postgres.Translate(ctx, err, "audit: facets for subject")
	}
	out := make([]Facet, 0, len(rows))
	for _, row := range rows {
		out = append(out, Facet{Name: row.Facet, Total: int(row.Total)})
	}
	return out, nil
}

func (s *Store) FacetsForWorkspace(ctx context.Context, workspace string) ([]Facet, error) {
	rows, err := s.q(ctx).FacetsForWorkspace(ctx, workspace)
	if err != nil {
		return nil, postgres.Translate(ctx, err, "audit: facets for workspace")
	}
	out := make([]Facet, 0, len(rows))
	for _, row := range rows {
		out = append(out, Facet{Name: row.Facet, Total: int(row.Total)})
	}
	return out, nil
}

func (s *Store) PageForSubjectFacet(ctx context.Context, subject, facet string, after Cursor, limit int32) ([]domain.Entry, error) {
	rows, err := s.q(ctx).EntriesForSubjectFacetPage(ctx, auditdb.EntriesForSubjectFacetPageParams{
		Subject: subject,
		Action:  facet,
		Column3: bound(after),
		Column4: cursorID(after),
		Limit:   limit,
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "audit: page for subject facet")
	}
	return entries(rows)
}

func (s *Store) PageForWorkspaceFacet(ctx context.Context, workspace, facet string, after Cursor, limit int32) ([]domain.Entry, error) {
	rows, err := s.q(ctx).EntriesForWorkspaceFacetPage(ctx, auditdb.EntriesForWorkspaceFacetPageParams{
		WorkspaceID: workspace,
		Action:      facet,
		Column3:     bound(after),
		Column4:     cursorID(after),
		Limit:       limit,
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "audit: page for workspace facet")
	}
	return entries(rows)
}

func (s *Store) PageForOrg(ctx context.Context, org string, after Cursor, limit int32) ([]domain.Entry, error) {
	rows, err := s.q(ctx).EntriesForOrgPage(ctx, auditdb.EntriesForOrgPageParams{
		OrgID:   org,
		Column2: bound(after),
		Column3: cursorID(after),
		Limit:   limit,
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "audit: page for org")
	}
	return entries(rows)
}

func (s *Store) FacetsForOrg(ctx context.Context, org string) ([]Facet, error) {
	rows, err := s.q(ctx).FacetsForOrg(ctx, org)
	if err != nil {
		return nil, postgres.Translate(ctx, err, "audit: facets for org")
	}
	out := make([]Facet, 0, len(rows))
	for _, row := range rows {
		out = append(out, Facet{Name: row.Facet, Total: int(row.Total)})
	}
	return out, nil
}
