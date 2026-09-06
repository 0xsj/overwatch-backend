package postgres

import (
	"context"
	"embed"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/journal/domain"
	"github.com/0xsj/overwatch-backend/internal/journal/infra/postgres/journaldb"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const Schema = "journal"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("journal: " + err.Error())
	}
	return ms
}

type Store struct {
	db *postgres.Pool
}

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("journal: NewStore with a nil Pool")
	}
	return &Store{db: db}
}

func (s *Store) q(ctx context.Context) *journaldb.Queries {
	return journaldb.New(s.db.DB(ctx))
}

func uuid(i id.ID) pgtype.UUID { return pgtype.UUID{Bytes: i, Valid: true} }

func maybe(i id.ID) pgtype.UUID {
	if i.IsZero() {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: i, Valid: true}
}

func ident(u pgtype.UUID) id.ID {
	if !u.Valid {
		return id.Nil
	}
	return id.ID(u.Bytes)
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

func line(row journaldb.JournalLine) domain.Line {
	return domain.Line{
		ID:          ident(row.ID),
		EventID:     ident(row.EventID),
		Action:      row.Action,
		Subject:     row.Subject,
		Origin:      row.Origin,
		Actor:       row.Actor,
		OnBehalfOf:  row.OnBehalfOf,
		WorkspaceID: row.WorkspaceID,
		Depth:       int(row.Depth),
		Attempt:     int(row.Attempt),
		Decision:    row.Decision,
		Correlation: ident(row.CorrelationID),
		Causation:   ident(row.CausationID),
		Detail:      row.Detail,
		OccurredAt:  instant(row.OccurredAt),
		RecordedAt:  instant(row.RecordedAt),
	}
}
