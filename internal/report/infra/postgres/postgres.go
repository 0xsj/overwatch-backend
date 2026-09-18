package postgres

import (
	"context"
	"embed"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/report/domain"
	"github.com/0xsj/overwatch-backend/internal/report/infra/postgres/reportdb"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const Schema = "report"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("report: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("report: NewStore with a nil pool")
	}
	return &Store{db: db}
}

func (s *Store) q(ctx context.Context) *reportdb.Queries { return reportdb.New(s.db.DB(ctx)) }

func uuid(i id.ID) pgtype.UUID { return pgtype.UUID{Bytes: i, Valid: true} }

func maybe(i id.ID) pgtype.UUID {
	if i.IsZero() {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: i, Valid: true}
}

func ident(u pgtype.UUID) id.ID {
	if !u.Valid {
		return id.ID{}
	}
	return id.ID(u.Bytes)
}

func text(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func stamp(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func instant(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time
}

// day maps a `date` column. It is a separate helper from `stamp` because the
// column is a DATE: a period is what a contract says, and storing an instant
// would invite a timezone to move somebody's Q3 by a day.
func day(t time.Time) pgtype.Date {
	if t.IsZero() {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: t, Valid: true}
}

func dayOf(d pgtype.Date) time.Time {
	if !d.Valid {
		return time.Time{}
	}
	return d.Time
}

// report maps a row WITHOUT its sections — they are a second table, and the
// store joins them in [Store.ByID] rather than here, so this stays a pure
// mapping.
func report(row reportdb.ReportReport) domain.Report {
	return domain.Report{
		ID: ident(row.ID), WorkspaceID: ident(row.WorkspaceID),
		TargetID: ident(row.TargetID), Title: row.Title,
		PreparedBy:  row.PreparedBy.String,
		PeriodStart: dayOf(row.PeriodStart), PeriodEnd: dayOf(row.PeriodEnd),
		Revisions: int(row.Revisions),
		CreatedBy: ident(row.CreatedBy),
		CreatedAt: instant(row.CreatedAt), UpdatedAt: instant(row.UpdatedAt),
		Sections: map[domain.Section]bool{},
	}
}

func revision(row reportdb.ReportRevision) (domain.Revision, error) {
	sections := make([]domain.Section, 0, len(row.Sections))
	for _, name := range row.Sections {
		s, err := domain.ParseSection(name)
		if err != nil {
			// A section this build no longer knows. It is REFUSED rather than
			// skipped: a revision is a claim about what a client received, and
			// silently dropping one from that list would make the claim false.
			return domain.Revision{}, fmt.Errorf("report: revision %s: %w", row.ID, err)
		}
		sections = append(sections, s)
	}
	return domain.Revision{
		ID: ident(row.ID), ReportID: ident(row.ReportID),
		WorkspaceID: ident(row.WorkspaceID), Number: int(row.Number),
		Hash: row.Hash, Bytes: row.Bytes, MediaType: row.MediaType,
		Sections: sections, IssuedBy: ident(row.IssuedBy),
		IssuedAt: instant(row.IssuedAt),
	}, nil
}

func notFound(ctx context.Context, err error, what string, as error) error {
	translated := postgres.Translate(ctx, err, what)
	if errors.IsKind(translated, errors.NotFound) {
		return fmt.Errorf("%s: %w", what, as)
	}
	return translated
}
