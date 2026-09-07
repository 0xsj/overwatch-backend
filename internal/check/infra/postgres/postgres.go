package postgres

import (
	"context"
	"embed"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/check/domain"
	"github.com/0xsj/overwatch-backend/internal/check/infra/postgres/checkdb"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

// Schema is "checks" and the package is `check`, which is the only place in this
// tree where the two differ.
//
// **`check` is a reserved word in SQL** — it is the constraint keyword — so
// `create schema check` is a syntax error and every hand-written query would
// need `"check".check` forever. Quoting works and the tax lands on a human in
// every future query file, failing at deploy rather than at compile.
// pkg/postgres refuses the reserved name outright so the next domain called
// `user` or `order` is told this at the door.
const Schema = "checks"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("check: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("check: NewStore with a nil pool")
	}
	return &Store{db: db}
}

func (s *Store) q(ctx context.Context) *checkdb.Queries { return checkdb.New(s.db.DB(ctx)) }

func uuid(i id.ID) pgtype.UUID { return pgtype.UUID{Bytes: i, Valid: true} }

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

// seconds maps a zero interval to NULL. Zero is "when somebody asks" in the
// domain and NULL is "when somebody asks" in the column, and the constraint
// refuses a stored zero so the two spellings cannot both exist.
func seconds(d time.Duration) pgtype.Int4 {
	if d <= 0 {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(d / time.Second), Valid: true}
}

func interval(i pgtype.Int4) time.Duration {
	if !i.Valid {
		return 0
	}
	return time.Duration(i.Int32) * time.Second
}

func subjects(of []domain.Subject) []string { return domain.Names(of) }

func check(row checkdb.ChecksCheck) (domain.Check, error) {
	status, err := domain.ParseStatus(row.Status)
	if err != nil {
		return domain.Check{}, err
	}
	applies := make([]domain.Subject, 0, len(row.AppliesTo))
	for _, name := range row.AppliesTo {
		s, err := domain.ParseSubject(name)
		if err != nil {
			return domain.Check{}, err
		}
		applies = append(applies, s)
	}
	return domain.Check{
		ID:         ident(row.ID),
		OrgID:      ident(row.OrgID),
		Name:       row.Name,
		Question:   row.Question,
		AppliesTo:  applies,
		Interval:   interval(row.IntervalSeconds),
		Enabled:    row.Enabled,
		Human:      row.Human,
		Status:     status,
		Version:    int(row.Version),
		CreatedBy:  ident(row.CreatedBy),
		CreatedAt:  instant(row.CreatedAt),
		UpdatedAt:  instant(row.UpdatedAt),
		ArchivedAt: instant(row.ArchivedAt),
	}, nil
}
