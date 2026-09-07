package postgres

import (
	"context"
	"embed"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/run/domain"
	"github.com/0xsj/overwatch-backend/internal/run/infra/postgres/rundb"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const Schema = "run"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("run: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("run: NewStore with a nil pool")
	}
	return &Store{db: db}
}

func (s *Store) q(ctx context.Context) *rundb.Queries { return rundb.New(s.db.DB(ctx)) }

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

// exit is the one conversion in this file that carries meaning rather than a
// type. NULL is "no process ever existed" and the domain says so with a second
// boolean, because a Go int cannot hold the absence — decisions/0033.
func exit(code int, has bool) pgtype.Int4 {
	if !has {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(code), Valid: true}
}

func run(row rundb.RunRun) (domain.Run, error) {
	state, err := domain.ParseState(row.State)
	if err != nil {
		return domain.Run{}, err
	}
	return domain.Run{
		ID:          ident(row.ID),
		WorkspaceID: ident(row.WorkspaceID),
		TargetID:    ident(row.TargetID),
		CheckID:     ident(row.CheckID),
		State:       state,
		StartedBy:   ident(row.StartedBy),
		StartedAt:   instant(row.StartedAt),
		FinishedAt:  instant(row.FinishedAt),
		Version:     int(row.Version),
	}, nil
}

func invocation(row rundb.RunInvocation) (domain.Invocation, error) {
	phase, err := domain.ParsePhase(row.Phase)
	if err != nil {
		return domain.Invocation{}, err
	}
	return domain.Invocation{
		ID:             ident(row.ID),
		RunID:          ident(row.RunID),
		WorkspaceID:    ident(row.WorkspaceID),
		StepID:         ident(row.StepID),
		ToolID:         ident(row.ToolID),
		Sequence:       int(row.Sequence),
		Phase:          phase,
		Argv:           row.Argv,
		Binary:         row.BinaryPath.String,
		ExitCode:       int(row.ExitCode.Int32),
		HasExitCode:    row.ExitCode.Valid,
		Signal:         row.Signal.String,
		RefusalRule:    ident(row.RefusalRule),
		PermitRule:     ident(row.PermitRule),
		SubjectKind:    row.SubjectKind.String,
		SubjectValue:   row.SubjectValue.String,
		RefusalReason:  row.RefusalReason.String,
		SkippedBecause: row.SkippedBecause.String,
		Unavailable:    row.Unavailable.String,
		StartedAt:      instant(row.StartedAt),
		FinishedAt:     instant(row.FinishedAt),
		DurationMS:     row.DurationMs,
	}, nil
}

func artifact(row rundb.RunArtifact) (domain.Artifact, error) {
	stream, err := domain.ParseStream(row.Stream)
	if err != nil {
		return domain.Artifact{}, err
	}
	return domain.Artifact{
		ID:           ident(row.ID),
		WorkspaceID:  ident(row.WorkspaceID),
		InvocationID: ident(row.InvocationID),
		Stream:       stream,
		Hash:         row.Hash,
		Bytes:        row.Bytes,
		Truncated:    row.Truncated,
		MediaType:    row.MediaType.String,
		CreatedAt:    instant(row.CreatedAt),
	}, nil
}
