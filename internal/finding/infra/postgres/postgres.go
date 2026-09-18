package postgres

import (
	"context"
	"embed"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/finding/domain"
	"github.com/0xsj/overwatch-backend/internal/finding/infra/postgres/findingdb"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const Schema = "finding"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("finding: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("finding: NewStore with a nil pool")
	}
	return &Store{db: db}
}

func (s *Store) q(ctx context.Context) *findingdb.Queries { return findingdb.New(s.db.DB(ctx)) }

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

// confidence is the one nullable float here, and NULL means "this claimant does
// not carry one" rather than zero — 0004's whole point. A pointer would let a
// nil travel as a valid-looking 0.0 into code that never checked.
func confidence(v float64, has bool) pgtype.Float8 {
	if !has {
		return pgtype.Float8{}
	}
	return pgtype.Float8{Float64: v, Valid: true}
}

// finding maps a row. It is the one place a stored enum becomes a domain one, so
// a bad column names the rule rather than travelling as a zero value.
func finding(row findingdb.FindingFinding) (domain.Finding, error) {
	state, err := domain.ParseState(row.State)
	if err != nil {
		return domain.Finding{}, err
	}
	severity, err := domain.ParseSeverity(row.Severity)
	if err != nil {
		return domain.Finding{}, err
	}
	claimant, err := domain.ParseClaimant(row.Claimant)
	if err != nil {
		return domain.Finding{}, err
	}
	out := domain.Finding{
		ID: ident(row.ID), WorkspaceID: ident(row.WorkspaceID),
		ToolID: ident(row.ToolID), Signature: row.Signature,
		FragmentID:    ident(row.FragmentID),
		FragmentKind:  row.FragmentKind,
		FragmentValue: row.FragmentValue,
		State:         state,
		Reason:        row.Reason.String,
		DecidedBy:     ident(row.DecidedBy),
		DecidedAt:     instant(row.DecidedAt),
		Severity:      severity,
		Assessment: domain.Assessment{
			Claimant: claimant, Actor: ident(row.Actor),
			Confidence: row.Confidence.Float64, HasConfidence: row.Confidence.Valid,
			Basis: row.Basis.String, At: instant(row.AssessedAt),
		},
		FirstSeen: instant(row.FirstSeen), LastSeen: instant(row.LastSeen),
		Sightings:  int(row.Sightings),
		Invocation: ident(row.InvocationID), Artifact: ident(row.ArtifactID),
		MappingID: ident(row.MappingID),
		CreatedAt: instant(row.CreatedAt),
	}
	if row.SupersededClaimant.Valid {
		prior, err := domain.ParseClaimant(row.SupersededClaimant.String)
		if err != nil {
			return domain.Finding{}, err
		}
		priorSeverity, err := domain.ParseSeverity(row.SupersededSeverity.String)
		if err != nil {
			return domain.Finding{}, err
		}
		out.SupersededSeverity = priorSeverity
		out.Superseded = &domain.Assessment{
			Claimant: prior, Actor: ident(row.SupersededActor),
			Confidence:    row.SupersededConfidence.Float64,
			HasConfidence: row.SupersededConfidence.Valid,
			Basis:         row.SupersededBasis.String,
			At:            instant(row.SupersededAt),
		}
	}
	return out, nil
}

func detail(row findingdb.FindingDetail) domain.Detail {
	return domain.Detail{
		ID: ident(row.ID), FindingID: ident(row.FindingID),
		Field: row.Field, Value: row.Value,
		MappingID: ident(row.MappingID), ArtifactID: ident(row.ArtifactID),
		SeenAt: instant(row.SeenAt),
	}
}

var _ = context.Background
