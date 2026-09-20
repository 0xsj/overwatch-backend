package postgres

import (
	"context"
	"embed"
	"time"

	"github.com/0xsj/overwatch-backend/internal/seen/domain"
	"github.com/0xsj/overwatch-backend/internal/seen/infra/postgres/seendb"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	pg "github.com/0xsj/overwatch-backend/pkg/postgres"
	"github.com/jackc/pgx/v5/pgtype"
)

const Schema = "seen"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []pg.Migration {
	ms, err := pg.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("seen: " + err.Error())
	}
	return ms
}

type Store struct{ db *pg.Pool }

func NewStore(db *pg.Pool) *Store {
	if db == nil {
		panic("seen: NewStore with a nil pool")
	}
	return &Store{db: db}
}

func (s *Store) q(ctx context.Context) *seendb.Queries { return seendb.New(s.db.DB(ctx)) }

func uuid(i id.ID) pgtype.UUID { return pgtype.UUID{Bytes: i, Valid: true} }

func ident(u pgtype.UUID) id.ID {
	if !u.Valid {
		return id.ID{}
	}
	return u.Bytes
}

func stamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: !t.IsZero()}
}

func instant(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time
}

func marker(row seendb.SeenMarker) domain.Marker {
	return domain.Marker{AccountID: ident(row.AccountID), WorkspaceID: ident(row.WorkspaceID), SeenAt: instant(row.SeenAt)}
}

func (s *Store) ByAccountWorkspace(ctx context.Context, account, workspace id.ID) (domain.Marker, error) {
	row, err := s.q(ctx).MarkerByAccountWorkspace(ctx, seendb.MarkerByAccountWorkspaceParams{
		AccountID: uuid(account), WorkspaceID: uuid(workspace),
	})
	if err != nil {
		translated := pg.Translate(ctx, err, "seen: read marker")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Marker{}, domain.ErrNotFound
		}
		return domain.Marker{}, translated
	}
	return marker(row), nil
}

func (s *Store) Upsert(ctx context.Context, want domain.Marker) error {
	if err := s.q(ctx).UpsertMarker(ctx, seendb.UpsertMarkerParams{
		AccountID: uuid(want.AccountID), WorkspaceID: uuid(want.WorkspaceID), SeenAt: stamp(want.SeenAt),
	}); err != nil {
		return pg.Translate(ctx, err, "seen: upsert marker")
	}
	return nil
}
