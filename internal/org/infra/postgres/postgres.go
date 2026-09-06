package postgres

import (
	"context"
	"embed"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/internal/org/infra/postgres/orgdb"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const Schema = "org"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("org: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("org: NewStore with a nil Pool")
	}
	return &Store{db: db}
}

func (s *Store) q(ctx context.Context) *orgdb.Queries { return orgdb.New(s.db.DB(ctx)) }

func uuid(i id.ID) pgtype.UUID { return pgtype.UUID{Bytes: i, Valid: true} }

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

func org(row orgdb.OrgOrg) domain.Org {
	return domain.Org{
		ID:        ident(row.ID),
		Name:      row.Name,
		Version:   int(row.Version),
		CreatedAt: instant(row.CreatedAt),
		UpdatedAt: instant(row.UpdatedAt),
	}
}

func member(row orgdb.OrgMember) (domain.Member, error) {
	role, err := domain.ParseRole(row.Role)
	if err != nil {
		return domain.Member{}, err
	}
	status, err := domain.ParseStatus(row.Status)
	if err != nil {
		return domain.Member{}, err
	}
	return domain.Member{
		ID:         ident(row.ID),
		OrgID:      ident(row.OrgID),
		AccountID:  ident(row.AccountID),
		Role:       role,
		Status:     status,
		Version:    int(row.Version),
		CreatedAt:  instant(row.CreatedAt),
		UpdatedAt:  instant(row.UpdatedAt),
		ArchivedAt: instant(row.ArchivedAt),
	}, nil
}
