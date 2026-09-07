package postgres

import (
	"context"
	"embed"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/entity/domain"
	"github.com/0xsj/overwatch-backend/internal/entity/infra/postgres/entitydb"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const Schema = "entity"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("entity: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("entity: NewStore with a nil pool")
	}
	return &Store{db: db}
}

func (s *Store) q(ctx context.Context) *entitydb.Queries { return entitydb.New(s.db.DB(ctx)) }

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

// confidence maps the two-field Go shape onto one nullable column. Present iff
// the claimant is a model — decisions/0003, and the column constraint says the
// same thing so the two cannot disagree.
func confidence(v float64, has bool) pgtype.Float8 {
	if !has {
		return pgtype.Float8{}
	}
	return pgtype.Float8{Float64: v, Valid: true}
}

func judgement(state string, by pgtype.UUID, at pgtype.Timestamptz, reason pgtype.Text) (domain.Judgement, error) {
	parsed, err := domain.ParseJudgement(state)
	if err != nil {
		return domain.Judgement{}, err
	}
	return domain.Judgement{
		State: parsed, By: ident(by), At: instant(at), Reason: reason.String,
	}, nil
}

func entityOf(row entitydb.EntityEntity) (domain.Entity, error) {
	j, err := judgement(row.JudgementState, row.JudgementBy, row.JudgementAt, row.JudgementReason)
	if err != nil {
		return domain.Entity{}, err
	}
	return domain.Entity{
		ID: ident(row.ID), WorkspaceID: ident(row.WorkspaceID),
		Kind: row.Kind, Label: row.Label, TargetID: ident(row.TargetID),
		Judgement: j, CreatedAt: instant(row.CreatedAt),
	}, nil
}

func fragmentOf(row entitydb.EntityFragment) (domain.Fragment, error) {
	origin, err := domain.ParseOrigin(row.Origin)
	if err != nil {
		return domain.Fragment{}, err
	}
	j, err := judgement(row.JudgementState, row.JudgementBy, row.JudgementAt, row.JudgementReason)
	if err != nil {
		return domain.Fragment{}, err
	}
	return domain.Fragment{
		ID: ident(row.ID), WorkspaceID: ident(row.WorkspaceID),
		Kind: row.Kind, Value: row.Value, Origin: origin,
		FirstSeen: instant(row.FirstSeen), LastSeen: instant(row.LastSeen),
		Observations: int(row.Observations), Judgement: j,
		ReadAt: instant(row.ReadAt), ReadBy: ident(row.ReadBy),
		CreatedAt: instant(row.CreatedAt),
	}, nil
}

func attributionOf(row entitydb.EntityAttribution) (domain.Attribution, error) {
	claimant, err := domain.ParseClaimant(row.Claimant)
	if err != nil {
		return domain.Attribution{}, err
	}
	state, err := domain.ParseClaimState(row.State)
	if err != nil {
		return domain.Attribution{}, err
	}
	return domain.Attribution{
		ID: ident(row.ID), WorkspaceID: ident(row.WorkspaceID),
		EntityID: ident(row.EntityID), FragmentID: ident(row.FragmentID),
		Claimant: claimant, ClaimantRef: ident(row.ClaimantRef),
		Confidence: row.Confidence.Float64, HasConfidence: row.Confidence.Valid,
		Basis: row.Basis, State: state,
		DecidedAt: instant(row.DecidedAt), DecidedBy: ident(row.DecidedBy),
		DecidedNote: row.DecidedNote.String, CreatedAt: instant(row.CreatedAt),
	}, nil
}
