package postgres

import (
	"context"
	"embed"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/observation/domain"
	"github.com/0xsj/overwatch-backend/internal/observation/infra/postgres/observationdb"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const Schema = "observation"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("observation: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("observation: NewStore with a nil pool")
	}
	return &Store{db: db}
}

func (s *Store) q(ctx context.Context) *observationdb.Queries {
	return observationdb.New(s.db.DB(ctx))
}

func uuid(i id.ID) pgtype.UUID { return pgtype.UUID{Bytes: i, Valid: true} }

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

func observation(row observationdb.ObservationObservation) domain.Observation {
	return domain.Observation{
		ID:             ident(row.ID),
		WorkspaceID:    ident(row.WorkspaceID),
		SubjectKind:    row.SubjectKind,
		SubjectValue:   row.SubjectValue,
		Field:          row.Field,
		Value:          row.Value,
		InvocationID:   ident(row.InvocationID),
		ArtifactID:     ident(row.ArtifactID),
		MappingID:      ident(row.MappingID),
		MappingVersion: int(row.MappingVersion),
		ObservedAt:     instant(row.ObservedAt),
		RecordedAt:     instant(row.RecordedAt),
	}
}

func unmapped(row observationdb.ObservationUnmapped) domain.Unmapped {
	return domain.Unmapped{
		ID:           ident(row.ID),
		WorkspaceID:  ident(row.WorkspaceID),
		InvocationID: ident(row.InvocationID),
		ArtifactID:   ident(row.ArtifactID),
		Path:         row.Path,
		Seen:         int(row.Seen),
		Sample:       row.Sample.String,
		RecordedAt:   instant(row.RecordedAt),
	}
}

func (s *Store) Create(ctx context.Context, o domain.Observation) error {
	err := s.q(ctx).InsertObservation(ctx, observationdb.InsertObservationParams{
		ID: uuid(o.ID), WorkspaceID: uuid(o.WorkspaceID),
		SubjectKind: o.SubjectKind, SubjectValue: o.SubjectValue,
		Field: o.Field, Value: o.Value,
		InvocationID: uuid(o.InvocationID), ArtifactID: uuid(o.ArtifactID),
		MappingID: uuid(o.MappingID), MappingVersion: int32(o.MappingVersion),
		ObservedAt: stamp(o.ObservedAt), RecordedAt: stamp(o.RecordedAt),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "observation: insert")
	}
	return nil
}

func (s *Store) ByID(ctx context.Context, workspace, want id.ID) (domain.Observation, error) {
	row, err := s.q(ctx).ObservationByID(ctx, observationdb.ObservationByIDParams{
		ID: uuid(want), WorkspaceID: uuid(workspace),
	})
	if err != nil {
		translated := postgres.Translate(ctx, err, "observation: read")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Observation{}, fmt.Errorf("observation: read: %w", domain.ErrNotFound)
		}
		return domain.Observation{}, translated
	}
	return observation(row), nil
}

func (s *Store) ForInvocation(ctx context.Context, workspace, invocation id.ID, limit int) ([]domain.Observation, error) {
	rows, err := s.q(ctx).ObservationsForInvocation(ctx, observationdb.ObservationsForInvocationParams{
		InvocationID: uuid(invocation), WorkspaceID: uuid(workspace), Page: int32(limit),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "observation: for invocation")
	}
	return observations(rows), nil
}

func (s *Store) ForSubject(ctx context.Context, workspace id.ID, kind, value string, limit int) ([]domain.Observation, error) {
	rows, err := s.q(ctx).ObservationsForSubject(ctx, observationdb.ObservationsForSubjectParams{
		WorkspaceID: uuid(workspace), SubjectKind: kind, SubjectValue: value, Page: int32(limit),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "observation: for subject")
	}
	return observations(rows), nil
}

// Subjects is a GROUP BY over observations and NOT a table, deliberately: it is
// what `fragment` will be a dedup of, and building the table before `entity`,
// `attribution` and `derivation` are designed would fix the shape early.
func (s *Store) Subjects(ctx context.Context, workspace id.ID, limit int) ([]domain.Subject, error) {
	rows, err := s.q(ctx).SubjectsForWorkspace(ctx, observationdb.SubjectsForWorkspaceParams{
		WorkspaceID: uuid(workspace), Page: int32(limit),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "observation: subjects")
	}
	out := make([]domain.Subject, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Subject{
			Kind: row.SubjectKind, Value: row.SubjectValue,
			Observations: int(row.Observations), Fields: int(row.Fields),
			LastSeen: instant(row.LastSeen),
		})
	}
	return out, nil
}

func (s *Store) RecordUnmapped(ctx context.Context, u domain.Unmapped) error {
	err := s.q(ctx).InsertUnmapped(ctx, observationdb.InsertUnmappedParams{
		ID: uuid(u.ID), WorkspaceID: uuid(u.WorkspaceID),
		InvocationID: uuid(u.InvocationID), ArtifactID: uuid(u.ArtifactID),
		Path: u.Path, Seen: int32(u.Seen), Sample: text(u.Sample),
		RecordedAt: stamp(u.RecordedAt),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "observation: insert unmapped")
	}
	return nil
}

func (s *Store) UnmappedFor(ctx context.Context, workspace, invocation id.ID) ([]domain.Unmapped, error) {
	rows, err := s.q(ctx).UnmappedForInvocation(ctx, observationdb.UnmappedForInvocationParams{
		InvocationID: uuid(invocation), WorkspaceID: uuid(workspace),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "observation: unmapped")
	}
	out := make([]domain.Unmapped, 0, len(rows))
	for _, row := range rows {
		out = append(out, unmapped(row))
	}
	return out, nil
}

func (s *Store) Quality(ctx context.Context, workspace, invocation id.ID) (domain.Quality, error) {
	row, err := s.q(ctx).ExtractionQuality(ctx, observationdb.ExtractionQualityParams{
		Workspace: uuid(workspace), Invocation: uuid(invocation),
	})
	if err != nil {
		return domain.Quality{}, postgres.Translate(ctx, err, "observation: quality")
	}
	return domain.Quality{
		Mapped: int(row.Mapped), LeftAlone: int(row.LeftAlone),
		Observations: int(row.Observations), Fields: int(row.Fields),
	}, nil
}

func observations(rows []observationdb.ObservationObservation) []domain.Observation {
	out := make([]domain.Observation, 0, len(rows))
	for _, row := range rows {
		out = append(out, observation(row))
	}
	return out
}

// SubjectsPerInvocation is coverage's second source — what each invocation
// actually said something about. It is a group-by in the database because the
// alternative is pulling every observation an engagement holds to compute one
// grid.
func (s *Store) SubjectsPerInvocation(ctx context.Context, workspace id.ID) ([]domain.SubjectSeen, error) {
	rows, err := s.q(ctx).SubjectsPerInvocation(ctx, uuid(workspace))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "observation: subjects per invocation")
	}
	out := make([]domain.SubjectSeen, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.SubjectSeen{
			InvocationID: ident(row.InvocationID), Kind: row.SubjectKind,
			Value: row.SubjectValue, At: instant(row.LastSeen),
		})
	}
	return out, nil
}
