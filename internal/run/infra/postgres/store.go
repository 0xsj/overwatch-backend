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
		Feeds: uuids(i.Upstream),
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

// AddCandidate records one thing a step was aimed at — decisions/0039. It is
// idempotent on (invocation, kind, value): a resolution that runs twice must not
// double the coverage denominator, and the second write is identical to the
// first because both come from the same gate answer.
func (s *Store) AddCandidate(ctx context.Context, c domain.Candidate) error {
	err := s.q(ctx).InsertCandidate(ctx, rundb.InsertCandidateParams{
		ID: uuid(c.ID), WorkspaceID: uuid(c.WorkspaceID), RunID: uuid(c.RunID),
		InvocationID: uuid(c.InvocationID), Kind: c.Kind, Value: c.Value,
		Permitted: c.Permitted, RefusalRule: maybe(c.RefusalRule),
		RefusalReason: text(c.RefusalReason), CreatedAt: stamp(c.CreatedAt),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "run: insert candidate")
	}
	return nil
}

// Candidates is every thing a run's steps were aimed at, permitted and refused
// together. The refused ones are the scope proof and are the reason this is a
// table rather than a column.
func (s *Store) Candidates(ctx context.Context, run id.ID) ([]domain.Candidate, error) {
	rows, err := s.q(ctx).CandidatesForRun(ctx, uuid(run))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "run: candidates for run")
	}
	out := make([]domain.Candidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, candidate(row))
	}
	return out, nil
}

// RefusedCandidates is the per-candidate half of the scope proof: which THINGS
// one rule refused inside steps that ran anyway. `Refusals` answers which whole
// steps it refused, and only 0039 made the two different questions.
func (s *Store) RefusedCandidates(ctx context.Context, workspace, rule id.ID, limit int) ([]domain.Candidate, error) {
	rows, err := s.q(ctx).RefusedCandidatesForRule(ctx, rundb.RefusedCandidatesForRuleParams{
		WorkspaceID: uuid(workspace), RefusalRule: maybe(rule), Page: int32(limit),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "run: refused candidates")
	}
	out := make([]domain.Candidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, candidate(row))
	}
	return out, nil
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
			CheckID: ident(row.CheckID), Kind: row.SubjectKind,
			Value: row.SubjectValue, At: instant(row.LastChecked),
		})
	}
	return out, nil
}

// Unavailable is `health`'s first probe: a tool `execx` could not start at all.
// Grouped by tool and reason, so one tool off PATH across forty invocations is
// one symptom with a count.
func (s *Store) Unavailable(ctx context.Context, workspace id.ID) ([]domain.Unavailable, error) {
	rows, err := s.q(ctx).ToolsThatCouldNotStart(ctx, uuid(workspace))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "run: tools that could not start")
	}
	out := make([]domain.Unavailable, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Unavailable{
			ToolID: ident(row.ToolID), Reason: row.Unavailable.String,
			Seen: int(row.Seen), Since: instant(row.Since),
		})
	}
	return out, nil
}

// Invocations counted, which is the DENOMINATOR that probe needs: "no tool
// failed to start, out of nought invocations" is not a measurement.
func (s *Store) CountInvocations(ctx context.Context, workspace id.ID) (int, error) {
	n, err := s.q(ctx).InvocationsConsidered(ctx, uuid(workspace))
	if err != nil {
		return 0, postgres.Translate(ctx, err, "run: invocations considered")
	}
	return int(n), nil
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
