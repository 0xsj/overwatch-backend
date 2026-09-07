package command

import (
	"context"
	"errors"
	"time"

	"github.com/0xsj/overwatch-backend/internal/run/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// Schedulable is the port that enumerates what COULD run. It is assembled by the
// composition root out of `check`, `workspace` and `target` — three peers.
//
// **It is driven from CHECKS**, which is the smallest set: a firm has six, not
// six thousand targets. `0038` §4 accepts that the fan-out is N+1 and says so
// rather than hiding it.
type Schedulable interface {
	Pairs(ctx context.Context) ([]domain.Pair, error)
}

// Started is the scheduler's own read of `run`. It is a second interface rather
// than a method on Repository because the scheduler needs one query and a
// Repository it could start runs through would let it do so without going via
// [Runs.Start], which is where the plan is made.
type Started interface {
	LastStartedPerPair(ctx context.Context) (map[domain.PairKey]time.Time, error)
}

// Scheduler is the FIFTH lifecycle — decisions/0038 §2 — beside the listener,
// the outbox dispatcher, the journal sweep and the executor.
//
// **It plans and does not spawn.** The executor runs what it starts, and the two
// are separate because a scheduler that also spawned would hold a transaction
// for the length of a scan.
type Scheduler struct {
	runs    *Runs
	pairs   Schedulable
	started Started
	ids     Minter
	clock   Clock

	every time.Duration
	batch int
	log   Logger
}

func NewScheduler(runs *Runs, pairs Schedulable, started Started,
	ids Minter, clock Clock, every time.Duration, batch int, log Logger) *Scheduler {
	if runs == nil || pairs == nil || started == nil || ids == nil || clock == nil || log == nil {
		panic("run: NewScheduler with a nil dependency")
	}
	if every <= 0 {
		every = time.Minute
	}
	if batch < 1 {
		batch = 1
	}
	return &Scheduler{runs: runs, pairs: pairs, started: started,
		ids: ids, clock: clock, every: every, batch: batch, log: log}
}

func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.log.Error("run scheduler", "cause", err)
			}
		}
	}
}

// tick reads what could run, reads when each last ran, and starts the due ones.
//
// **A failure to start ONE pair does not stop the tick.** The commonest cause is
// a check whose chain was emptied or a target archived between the two reads —
// 0038 names that race as the claim that fails quietly — and losing every other
// due run to it would turn one stale pair into a stopped scheduler.
func (s *Scheduler) tick(ctx context.Context) error {
	pairs, err := s.pairs.Pairs(ctx)
	if err != nil {
		return err
	}
	if len(pairs) == 0 {
		return nil
	}
	last, err := s.started.LastStartedPerPair(ctx)
	if err != nil {
		return err
	}

	for _, due := range domain.Due(pairs, last, s.clock.Now(), s.batch) {
		// ORIGIN SCHEDULE, and no actor. A schedule is not a who —
		// decisions/0038 — so the run's `started_by` is zero and the journal
		// says what caused it rather than who.
		at := provenance.NewContext(ctx, provenance.New(provenance.OriginSchedule, s.ids))
		if _, err := s.runs.Start(at, due.WorkspaceID, due.TargetID, due.CheckID, id.ID{}); err != nil {
			s.log.Error("run scheduler: start",
				"workspace", due.WorkspaceID.String(),
				"target", due.TargetID.String(),
				"check", due.CheckName,
				"cause", err)
			continue
		}
		s.log.Info("scheduled",
			"check", due.CheckName,
			"target", due.TargetID.String(),
			"every", due.Interval.String())
	}
	return nil
}
