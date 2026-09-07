package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/run/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/execx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Executor turns a plan into processes. It is the FOURTH lifecycle beside the
// listener, the outbox dispatcher and the journal sweep — decisions/0033 §6 —
// and it is started in run.go rather than constructed and forgotten, which
// `0022` names as the failure that looks exactly like a system with no work.
type Executor struct {
	repo       Repository
	runs       *Runs
	tools      Tools
	workspaces Workspaces
	spawner    Spawner
	blobs      Blobs
	extracts   Extracts
	tx         Transactor
	publisher  events.Publisher
	ids        Minter
	clock      Clock

	policy execx.Policy
	batch  int
	every  time.Duration
	log    Logger
}

// Logger is the narrowest thing this package needs to say something went wrong
// in a loop nobody is waiting on. A worker that swallows its errors is a worker
// that appears to be working.
type Logger interface {
	Error(msg string, args ...any)
	Info(msg string, args ...any)
}

func NewExecutor(repo Repository, runs *Runs, tools Tools, workspaces Workspaces,
	spawner Spawner, blobs Blobs, extracts Extracts, tx Transactor, publisher events.Publisher,
	ids Minter, clock Clock, policy execx.Policy, batch int, every time.Duration,
	log Logger) *Executor {
	if repo == nil || runs == nil || tools == nil || workspaces == nil ||
		spawner == nil || blobs == nil || extracts == nil || tx == nil ||
		publisher == nil || ids == nil || clock == nil || log == nil {
		panic("run: NewExecutor with a nil dependency")
	}
	if batch < 1 {
		batch = 1
	}
	if every <= 0 {
		every = 2 * time.Second
	}
	return &Executor{repo: repo, runs: runs, tools: tools, workspaces: workspaces,
		spawner: spawner, blobs: blobs, extracts: extracts, tx: tx, publisher: publisher,
		ids: ids, clock: clock, policy: policy, batch: batch, every: every, log: log}
}

// Run polls until the context is cancelled. It polls rather than subscribing
// because the claim query is also the CRASH RECOVERY read — a run left `running`
// by a killed process is picked up on the next tick with no separate sweep and
// no flag to get out of step with the work.
func (e *Executor) Run(ctx context.Context) {
	ticker := time.NewTicker(e.every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := e.tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				e.log.Error("run executor", "cause", err)
			}
		}
	}
}

func (e *Executor) tick(ctx context.Context) error {
	// The claim and the execution are in ONE transaction, so `for update skip
	// locked` still holds the row while the processes run. That serialises a
	// batch against other workers and is the point: a run executed twice writes
	// two sets of artifacts for one plan.
	return e.tx.InTx(ctx, func(ctx context.Context) error {
		claimed, err := e.repo.Claim(ctx, e.batch)
		if err != nil {
			return err
		}
		for _, r := range claimed {
			if err := e.execute(ctx, r); err != nil {
				return err
			}
		}
		return nil
	})
}

func (e *Executor) execute(ctx context.Context, r domain.Run) error {
	org, err := e.workspaces.OrgOf(ctx, r.WorkspaceID)
	if err != nil {
		return err
	}
	planned, err := e.repo.Invocations(ctx, r.ID)
	if err != nil {
		return err
	}

	var ran, failed int
	for _, i := range planned {
		if i.Phase != domain.PhasePending {
			continue
		}
		done, err := e.one(ctx, org, i)
		if err != nil {
			return err
		}
		ran++
		if done.Phase == domain.PhaseFailed {
			failed++
		}
	}

	// COMPLETE even when every invocation was refused. A run that was wholly
	// refused is a complete answer to "may we look at this", and calling it
	// stopped would make the scope proof read as a failure.
	finished, err := r.Finish(domain.StateComplete, e.clock.Now())
	if err != nil {
		return err
	}
	if err := e.repo.Finish(ctx, finished); err != nil {
		return err
	}
	return e.runs.work(ctx, domain.EventRunFinished, finished, domain.Finished{
		RunID: finished.ID.String(), WorkspaceID: finished.WorkspaceID.String(),
		State: finished.State.String(), Ran: ran, Failed: failed,
	})
}

// one runs a single invocation and records everything that came back, including
// the two artifacts.
func (e *Executor) one(ctx context.Context, org id.ID, i domain.Invocation) (domain.Invocation, error) {
	started, err := i.Start(e.clock.Now())
	if err != nil {
		return i, err
	}
	if err := e.repo.SaveInvocation(ctx, started); err != nil {
		return i, err
	}

	begin := e.clock.Now()
	result, spawnErr := e.spawner.Spawn(ctx, started.Argv, e.policy)
	took := e.clock.Now().Sub(begin)

	var done domain.Invocation
	switch {
	case spawnErr != nil:
		// The tool could not be run at all — off PATH, not executable. That is
		// `failed` WITH NO EXIT CODE, and CLAUDE.md's `health` noun exists
		// because the alternative looks like silence.
		done, err = started.Broke(spawnErr.Error(), result.Signal, e.clock.Now(), took)
	case result.Outcome != execx.Ran:
		// Timed out, or refused by the policy. Also no exit code.
		done, err = started.Broke(reasonOf(result), result.Signal, e.clock.Now(), took)
	default:
		var ok bool
		ok, err = e.tools.Succeeded(ctx, org, started.ToolID, result.ExitCode)
		if err != nil {
			return i, err
		}
		done, err = started.Ended(result.Argv, result.Binary, result.ExitCode, ok,
			result.Signal, e.clock.Now(), took)
	}
	if err != nil {
		return i, err
	}
	if err := e.repo.SaveInvocation(ctx, done); err != nil {
		return i, err
	}

	media, err := e.tools.MediaType(ctx, org, done.ToolID)
	if err != nil {
		return i, err
	}
	var (
		wrote    *int64
		observed *int
	)
	for _, stream := range []struct {
		which     domain.Stream
		body      []byte
		truncated bool
		media     string
	}{
		{domain.StreamStdout, result.Stdout, result.StdoutTruncated, media},
		// stderr is TEXT whatever the tool's stdout is. A tool invoked with
		// `-json` writes JSON to one stream and warnings to the other, and
		// labelling both the same makes the second unreadable.
		{domain.StreamStderr, result.Stderr, result.StderrTruncated, "text/plain"},
	} {
		if stream.body == nil {
			// NOTHING WAS WRITTEN — no row. A zero-byte artifact is a different
			// fact and gets one.
			continue
		}
		artifact, err := e.store(ctx, done, stream.which, stream.body, stream.truncated, stream.media)
		if err != nil {
			return i, err
		}
		if stream.which != domain.StreamStdout {
			// Only stdout is EXTRACTED. stderr is a tool's warnings, kept
			// verbatim and citable, and reading field paths out of it would
			// turn a progress bar into observations.
			continue
		}
		n := artifact.Bytes
		wrote = &n
		read, err := e.extracts.Extract(ctx, Extraction{
			WorkspaceID: done.WorkspaceID, OrgID: org, InvocationID: done.ID,
			ArtifactID: artifact.ID, ToolID: done.ToolID, Body: stream.body,
			// The INVOCATION's start, not now — decisions/0035 §5.
			ObservedAt: done.StartedAt,
		})
		if err != nil {
			return i, err
		}
		observed = &read
	}

	return done, e.runs.work(ctx, domain.EventInvocationEnded,
		domain.Run{ID: done.RunID, WorkspaceID: done.WorkspaceID},
		domain.InvocationEnded{
			InvocationID: done.ID.String(), RunID: done.RunID.String(),
			WorkspaceID: done.WorkspaceID.String(), ToolID: done.ToolID.String(),
			State: done.Phase.String(), Bytes: wrote, Observations: observed,
		})
}

func (e *Executor) store(ctx context.Context, of domain.Invocation, stream domain.Stream,
	body []byte, truncated bool, media string) (domain.Artifact, error) {
	hash, size, err := e.blobs.Put(ctx, bytes.NewReader(body))
	if err != nil {
		return domain.Artifact{}, fmt.Errorf("run: store %s: %w", stream, err)
	}
	a, err := domain.NewArtifact(e.ids.NewID(), of.WorkspaceID, of.ID, stream,
		hash, size, truncated, media, e.clock.Now())
	if err != nil {
		return domain.Artifact{}, err
	}
	return a, e.repo.AddArtifact(ctx, a)
}

func reasonOf(r execx.Result) string {
	if r.Reason != "" {
		return r.Reason
	}
	return r.Outcome.String()
}
