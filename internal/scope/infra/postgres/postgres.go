package postgres

import (
	"context"
	"embed"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/scope/domain"
	"github.com/0xsj/overwatch-backend/internal/scope/infra/postgres/scopedb"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const Schema = "scope"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("scope: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("scope: NewStore with a nil pool")
	}
	return &Store{db: db}
}

func (s *Store) q(ctx context.Context) *scopedb.Queries { return scopedb.New(s.db.DB(ctx)) }

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

// names writes the enums out as text. They are stored as their SPELLING and not
// as an ordinal, so inserting a value into the Go enum later cannot silently
// reinterpret every stored row — the trap `iota` sets for anything persisted.
func kindNames(kinds []domain.Kind) []string {
	out := make([]string, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, k.String())
	}
	return out
}

func intensityNames(tools []domain.Intensity) []string {
	if len(tools) == 0 {
		return nil
	}
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.String())
	}
	return out
}

func rule(row scopedb.ScopeRule) (domain.Rule, error) {
	polarity, err := domain.ParsePolarity(row.Polarity)
	if err != nil {
		return domain.Rule{}, err
	}
	gate, err := domain.ParseGate(row.Gate)
	if err != nil {
		return domain.Rule{}, err
	}
	kinds := make([]domain.Kind, 0, len(row.Kinds))
	for _, name := range row.Kinds {
		k, err := domain.ParseKind(name)
		if err != nil {
			return domain.Rule{}, err
		}
		kinds = append(kinds, k)
	}
	var tools []domain.Intensity
	for _, name := range row.Tools {
		t, err := domain.ParseIntensity(name)
		if err != nil {
			return domain.Rule{}, err
		}
		tools = append(tools, t)
	}
	return domain.Rule{
		ID:           ident(row.ID),
		WorkspaceID:  ident(row.WorkspaceID),
		TargetID:     ident(row.TargetID),
		Pattern:      row.Pattern,
		Polarity:     polarity,
		Gate:         gate,
		Kinds:        kinds,
		Tools:        tools,
		CreatedBy:    ident(row.CreatedBy),
		CreatedAt:    instant(row.CreatedAt),
		SupersededAt: instant(row.SupersededAt),
		SupersededBy: ident(row.SupersededBy),
	}, nil
}

func rules(rows []scopedb.ScopeRule) ([]domain.Rule, error) {
	out := make([]domain.Rule, 0, len(rows))
	for _, row := range rows {
		r, err := rule(row)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
