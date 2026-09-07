package postgres

import (
	"context"
	"embed"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/target/domain"
	"github.com/0xsj/overwatch-backend/internal/target/infra/postgres/targetdb"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
	"github.com/jackc/pgx/v5/pgtype"
)

// Schema is this domain's own, with its own migration ledger. No foreign key
// leaves it — decisions/0017.
const Schema = "target"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("target: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("target: NewStore with a nil pool")
	}
	return &Store{db: db}
}

func (s *Store) q(ctx context.Context) *targetdb.Queries { return targetdb.New(s.db.DB(ctx)) }

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
	return u.Bytes
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

func target(row targetdb.TargetTarget) (domain.Target, error) {
	kind, err := domain.ParseKind(row.Kind)
	if err != nil {
		return domain.Target{}, err
	}
	status, err := domain.ParseStatus(row.Status)
	if err != nil {
		return domain.Target{}, err
	}
	return domain.Target{
		ID:          ident(row.ID),
		WorkspaceID: ident(row.WorkspaceID),
		Name:        row.Name,
		Kind:        kind,
		Status:      status,
		Version:     int(row.Version),
		CreatedBy:   ident(row.CreatedBy),
		CreatedAt:   instant(row.CreatedAt),
		UpdatedAt:   instant(row.UpdatedAt),
		ArchivedAt:  instant(row.ArchivedAt),
		RootEntity:  ident(row.RootEntityID),
	}, nil
}

func targets(rows []targetdb.TargetTarget) ([]domain.Target, error) {
	out := make([]domain.Target, 0, len(rows))
	for _, row := range rows {
		t, err := target(row)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func read(ctx context.Context, err error, op string, missing error) error {
	translated := postgres.Translate(ctx, err, op)
	if errors.IsKind(translated, errors.NotFound) {
		return fmt.Errorf("%s: %w", op, missing)
	}
	return translated
}
