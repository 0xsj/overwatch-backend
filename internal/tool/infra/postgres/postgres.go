package postgres

import (
	"context"
	"embed"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/tool/domain"
	"github.com/0xsj/overwatch-backend/internal/tool/infra/postgres/tooldb"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const Schema = "tool"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("tool: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("tool: NewStore with a nil pool")
	}
	return &Store{db: db}
}

func (s *Store) q(ctx context.Context) *tooldb.Queries { return tooldb.New(s.db.DB(ctx)) }

func uuid(i id.ID) pgtype.UUID { return pgtype.UUID{Bytes: i, Valid: true} }

func ident(u pgtype.UUID) id.ID {
	if !u.Valid {
		return id.ID{}
	}
	return u.Bytes
}

// text maps "" to NULL, because a source tool consumes NOTHING and an empty
// string in that column would be a seventh feed kind nobody declared.
func text(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

// codes widens to the int32 the column holds. An exit code is a byte in
// practice and int32 in the schema, and the conversion is here rather than in
// the domain because a domain that knows the column width knows too much.
func codes(in []int) []int32 {
	out := make([]int32, 0, len(in))
	for _, c := range in {
		out = append(out, int32(c))
	}
	return out
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

// The enums are stored as their SPELLING, not as an ordinal, so inserting a
// value into the Go enum later cannot silently reinterpret every stored row.
func tool(row tooldb.ToolTool) (domain.Tool, error) {
	intensity, err := domain.ParseIntensity(row.Intensity)
	if err != nil {
		return domain.Tool{}, err
	}
	status, err := domain.ParseStatus(row.Status)
	if err != nil {
		return domain.Tool{}, err
	}
	consumes, err := domain.ParseFeed(row.Consumes.String)
	if err != nil {
		return domain.Tool{}, err
	}
	produces, err := domain.ParseFeed(row.Produces.String)
	if err != nil {
		return domain.Tool{}, err
	}
	return domain.Tool{
		ID:               ident(row.ID),
		OrgID:            ident(row.OrgID),
		Name:             row.Name,
		Argv:             row.Argv,
		Intensity:        intensity,
		Consumes:         consumes,
		Produces:         produces,
		SuccessExitCodes: narrow(row.SuccessExitCodes),
		Status:           status,
		Version:          int(row.Version),
		CreatedBy:        ident(row.CreatedBy),
		CreatedAt:        instant(row.CreatedAt),
		UpdatedAt:        instant(row.UpdatedAt),
		ArchivedAt:       instant(row.ArchivedAt),
	}, nil
}

func mapping(row tooldb.ToolMapping) (domain.Mapping, error) {
	state, err := domain.ParseState(row.State)
	if err != nil {
		return domain.Mapping{}, err
	}
	return domain.Mapping{
		ID:         ident(row.ID),
		OrgID:      ident(row.OrgID),
		ToolID:     ident(row.ToolID),
		Field:      row.Field,
		Expression: row.Expression,
		Version:    int(row.Version),
		State:      state,
		CreatedBy:  ident(row.CreatedBy),
		CreatedAt:  instant(row.CreatedAt),
		PromotedAt: instant(row.PromotedAt),
		RetiredAt:  instant(row.RetiredAt),
	}, nil
}

func narrow(in []int32) []int {
	out := make([]int, 0, len(in))
	for _, c := range in {
		out = append(out, int(c))
	}
	return out
}
