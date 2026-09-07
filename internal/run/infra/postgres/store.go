package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/run/domain"
	"github.com/0xsj/overwatch-backend/internal/run/infra/postgres/rundb"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

func (s *Store) Create(ctx context.Context, r domain.Run) error {
	err := s.q(ctx).InsertRun(ctx, rundb.InsertRunParams{
		ID: uuid(r.ID), WorkspaceID: uuid(r.WorkspaceID), TargetID: uuid(r.TargetID),
		CheckID: uuid(r.CheckID), State: r.State.String(), StartedBy: maybe(r.StartedBy),
		StartedAt: stamp(r.StartedAt), FinishedAt: stamp(r.FinishedAt),
		Version: int32(r.Version),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "run: insert run")
	}
	return nil
}

func (s *Store) ByID(ctx context.Context, workspace, want id.ID) (domain.Run, error) {
	row, err := s.q(ctx).RunByID(ctx, rundb.RunByIDParams{ID: uuid(want), WorkspaceID: uuid(workspace)})
	if err != nil {
		translated := postgres.Translate(ctx, err, "run: read run")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Run{}, fmt.Errorf("run: read run: %w", domain.ErrNotFound)
		}
		return domain.Run{}, translated
	}
	return run(row)
}

// Page is keyset, not offset — the same shape the audit ledger uses, because
// this list only grows and an offset walks past rows inserted since.
func (s *Store) Page(ctx context.Context, workspace, target id.ID,
	before time.Time, beforeID id.ID, limit int) ([]domain.Run, error) {
	rows, err := s.q(ctx).RunsForWorkspace(ctx, rundb.RunsForWorkspaceParams{
		WorkspaceID: uuid(workspace),
		Target:      maybe(target),
		Before:      stamp(before),
		BeforeID:    maybe(beforeID),
		Page:        int32(limit),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "run: runs for workspace")
	}
	out := make([]domain.Run, 0, len(rows))
	for _, row := range rows {
		r, err := run(row)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Store) Finish(ctx context.Context, r domain.Run) error {
	n, err := s.q(ctx).FinishRun(ctx, rundb.FinishRunParams{
		ID: uuid(r.ID), WorkspaceID: uuid(r.WorkspaceID), State: r.State.String(),
		FinishedAt: stamp(r.FinishedAt), Version: int32(r.Version),
		Version_2: int32(r.Version - 1),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "run: finish run")
	}
	if n == 0 {
		return fmt.Errorf("run: finish run: %w", domain.ErrStaleWrite)
	}
	return nil
}

// Claim is the worker's read. FOR UPDATE SKIP LOCKED, so two workers never take
// one run and a worker that dies mid-run leaves the row for the next boot rather
// than losing it — decisions/0033.
//
// It must be called INSIDE a transaction, and the lock lives until that
// transaction ends. Called outside one, every row is unlocked the instant it is
// read and the skip-locked guarantee is nothing.
func (s *Store) Claim(ctx context.Context, batch int) ([]domain.Run, error) {
	rows, err := s.q(ctx).ClaimRun(ctx, int32(batch))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "run: claim")
	}
	out := make([]domain.Run, 0, len(rows))
	for _, row := range rows {
		r, err := run(row)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Store) PlanInvocation(ctx context.Context, i domain.Invocation) error {
	if err := i.Valid(); err != nil {
		return err
	}
	err := s.q(ctx).InsertInvocation(ctx, rundb.InsertInvocationParams{
		ID: uuid(i.ID), RunID: uuid(i.RunID), WorkspaceID: uuid(i.WorkspaceID),
		StepID: uuid(i.StepID), ToolID: uuid(i.ToolID), Sequence: int32(i.Sequence),
		Phase: i.Phase.String(), Argv: i.Argv, BinaryPath: text(i.Binary),
		ExitCode: exit(i.ExitCode, i.HasExitCode), Signal: text(i.Signal),
		RefusalRule: maybe(i.RefusalRule), RefusalReason: text(i.RefusalReason),
		SkippedBecause: text(i.SkippedBecause), Unavailable: text(i.Unavailable),
		StartedAt: stamp(i.StartedAt), FinishedAt: stamp(i.FinishedAt),
		DurationMs: i.DurationMS, PermitRule: maybe(i.PermitRule),
		SubjectKind: text(i.SubjectKind), SubjectValue: text(i.SubjectValue),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "run: insert invocation")
	}
	return nil
}

// SaveInvocation checks the domain's invariant before writing it. The schema
// holds the same rule, so this is belt and braces — and it is here so the error
// a caller sees names the rule rather than a constraint.
func (s *Store) SaveInvocation(ctx context.Context, i domain.Invocation) error {
	if err := i.Valid(); err != nil {
		return err
	}
	n, err := s.q(ctx).SaveInvocation(ctx, rundb.SaveInvocationParams{
		ID: uuid(i.ID), Phase: i.Phase.String(), Argv: i.Argv, BinaryPath: text(i.Binary),
		ExitCode: exit(i.ExitCode, i.HasExitCode), Signal: text(i.Signal),
		RefusalRule: maybe(i.RefusalRule), RefusalReason: text(i.RefusalReason),
		SkippedBecause: text(i.SkippedBecause), Unavailable: text(i.Unavailable),
		StartedAt: stamp(i.StartedAt), FinishedAt: stamp(i.FinishedAt),
		DurationMs: i.DurationMS, PermitRule: maybe(i.PermitRule),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "run: save invocation")
	}
	if n == 0 {
		return fmt.Errorf("run: save invocation: %w", domain.ErrInvocationNotFound)
	}
	return nil
}

func (s *Store) Invocations(ctx context.Context, r id.ID) ([]domain.Invocation, error) {
	rows, err := s.q(ctx).InvocationsForRun(ctx, uuid(r))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "run: invocations")
	}
	return invocations(rows)
}

func (s *Store) InvocationByID(ctx context.Context, workspace, want id.ID) (domain.Invocation, error) {
	row, err := s.q(ctx).InvocationByID(ctx, rundb.InvocationByIDParams{
		ID: uuid(want), WorkspaceID: uuid(workspace),
	})
	if err != nil {
		translated := postgres.Translate(ctx, err, "run: read invocation")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Invocation{}, fmt.Errorf("run: read invocation: %w", domain.ErrInvocationNotFound)
		}
		return domain.Invocation{}, translated
	}
	return invocation(row)
}

// Refusals answers which spawns a scope rule refused. It is the query that makes
// an append-only rule ledger worth keeping — decisions/0030 says three surfaces
// cite a rule id, and this is the one that reads them back.
func (s *Store) Refusals(ctx context.Context, workspace, rule id.ID, limit int) ([]domain.Invocation, error) {
	rows, err := s.q(ctx).RefusalsForRule(ctx, rundb.RefusalsForRuleParams{
		WorkspaceID: uuid(workspace), RefusalRule: uuid(rule), Page: int32(limit),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "run: refusals")
	}
	return invocations(rows)
}

func (s *Store) AddArtifact(ctx context.Context, a domain.Artifact) error {
	err := s.q(ctx).InsertArtifact(ctx, rundb.InsertArtifactParams{
		ID: uuid(a.ID), WorkspaceID: uuid(a.WorkspaceID), InvocationID: uuid(a.InvocationID),
		Stream: a.Stream.String(), Hash: a.Hash, Bytes: a.Bytes, Truncated: a.Truncated,
		MediaType: text(a.MediaType), CreatedAt: stamp(a.CreatedAt),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "run: insert artifact")
	}
	return nil
}

func (s *Store) Artifacts(ctx context.Context, invocation id.ID) ([]domain.Artifact, error) {
	rows, err := s.q(ctx).ArtifactsForInvocation(ctx, uuid(invocation))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "run: artifacts")
	}
	out := make([]domain.Artifact, 0, len(rows))
	for _, row := range rows {
		a, err := artifact(row)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func (s *Store) ArtifactByID(ctx context.Context, workspace, want id.ID) (domain.Artifact, error) {
	row, err := s.q(ctx).ArtifactByID(ctx, rundb.ArtifactByIDParams{
		ID: uuid(want), WorkspaceID: uuid(workspace),
	})
	if err != nil {
		translated := postgres.Translate(ctx, err, "run: read artifact")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Artifact{}, fmt.Errorf("run: read artifact: %w", domain.ErrArtifactNotFound)
		}
		return domain.Artifact{}, translated
	}
	return artifact(row)
}

// LatestCheckedPerSubject is what COVERAGE reads — decisions/0037. It is a
// group-by in the database rather than a walk in Go, because the alternative is
// pulling every invocation an engagement has ever run to compute one grid.
func (s *Store) LatestCheckedPerSubject(ctx context.Context, workspace, target id.ID) ([]domain.Checked, error) {
	rows, err := s.q(ctx).LatestCheckedPerSubject(ctx, rundb.LatestCheckedPerSubjectParams{
		WorkspaceID: uuid(workspace), Target: maybe(target),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "run: latest checked")
	}
	out := make([]domain.Checked, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Checked{
			CheckID: ident(row.CheckID), Kind: row.SubjectKind.String,
			Value: row.SubjectValue.String, At: instant(row.LastChecked),
		})
	}
	return out, nil
}

// InvocationChecks is the other half of coverage's join — which check each
// invocation belonged to.
func (s *Store) InvocationChecks(ctx context.Context, workspace, target id.ID) ([]domain.InvocationCheck, error) {
	rows, err := s.q(ctx).InvocationChecks(ctx, rundb.InvocationChecksParams{
		WorkspaceID: uuid(workspace), Target: maybe(target),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "run: invocation checks")
	}
	out := make([]domain.InvocationCheck, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.InvocationCheck{
			InvocationID: ident(row.InvocationID), CheckID: ident(row.CheckID),
		})
	}
	return out, nil
}

// LastStartedPerPair is the scheduler's only read of its own table.
func (s *Store) LastStartedPerPair(ctx context.Context) (map[domain.PairKey]time.Time, error) {
	rows, err := s.q(ctx).LastStartedPerPair(ctx)
	if err != nil {
		return nil, postgres.Translate(ctx, err, "run: last started")
	}
	out := make(map[domain.PairKey]time.Time, len(rows))
	for _, row := range rows {
		out[domain.PairKey{
			TargetID: ident(row.TargetID), CheckID: ident(row.CheckID),
		}] = instant(row.LastStarted)
	}
	return out, nil
}

func invocations(rows []rundb.RunInvocation) ([]domain.Invocation, error) {
	out := make([]domain.Invocation, 0, len(rows))
	for _, row := range rows {
		i, err := invocation(row)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, nil
}
