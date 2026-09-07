package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
)

const (
	// DefaultRetention is decisions/0022's ninety days: long enough that an
	// incident reported at the end of a quarter can still be traced, short
	// enough that a redelivered parse does not occupy a year of storage.
	DefaultRetention = 90 * 24 * time.Hour

	// MinRetention is a floor and not politeness. This is the one component
	// whose job is to delete evidence, its input is an environment variable,
	// and `RETENTION=0` or a unit typo is silent and irreversible. Refusing to
	// start is the only failure mode that cannot destroy anything.
	MinRetention = 7 * 24 * time.Hour

	// DefaultBatch bounds the transaction, not the throughput. An unbounded
	// delete against a year of rows is one very large lock.
	DefaultBatch = 5000

	// DefaultInterval is a day. Nothing here is urgent: a row that lives an
	// extra few hours past its window costs nothing, and a tighter loop only
	// scans more often for the same result.
	DefaultInterval = 24 * time.Hour
)

var ErrRetentionTooShort = errors.Newf(errors.Invalid,
	"a journal retention below %s would delete evidence somebody still needs", MinRetention)

// Expirer is the narrowest thing this needs: delete a batch, say how many.
type Expirer interface {
	ExpireBefore(ctx context.Context, cutoff time.Time, batch int) (int, error)
}

type SweeperConfig struct {
	Store     Expirer
	Clock     Clock
	Log       *slog.Logger
	Retention time.Duration
	Interval  time.Duration
	Batch     int
}

// Sweeper deletes journal work older than the retention window — decisions/0022.
//
// **It never deletes a decision.** That is enforced in the SQL rather than here,
// so a second caller of the store cannot get it wrong, and it is what makes a
// chain older than the window degrade to its skeleton instead of disappearing.
type Sweeper struct {
	store     Expirer
	clock     Clock
	log       *slog.Logger
	retention time.Duration
	interval  time.Duration
	batch     int
}

// NewSweeper refuses a retention below the floor, so a misconfigured deployment
// dies at boot rather than at the first tick — by which point it has deleted
// everything.
func NewSweeper(cfg SweeperConfig) (*Sweeper, error) {
	if cfg.Store == nil || cfg.Clock == nil {
		panic("journal: NewSweeper with a nil dependency")
	}
	if cfg.Retention == 0 {
		cfg.Retention = DefaultRetention
	}
	if cfg.Retention < MinRetention {
		return nil, ErrRetentionTooShort
	}
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.Batch <= 0 {
		cfg.Batch = DefaultBatch
	}
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Sweeper{
		store: cfg.Store, clock: cfg.Clock, log: log,
		retention: cfg.Retention, interval: cfg.Interval, batch: cfg.Batch,
	}, nil
}

func (s *Sweeper) Retention() time.Duration { return s.retention }

// Sweep is one pass: batches until a batch comes back short, and answers with
// the total. The cutoff is computed ONCE per pass rather than per batch, so a
// long sweep deletes a consistent window instead of a creeping one.
func (s *Sweeper) Sweep(ctx context.Context) (int, error) {
	cutoff := s.clock.Now().Add(-s.retention)
	total := 0
	for {
		if err := ctx.Err(); err != nil {
			// Interrupted, and that is safe to report as progress rather than
			// as failure: what was deleted stays deleted, and the next pass
			// recomputes the cutoff and carries on.
			return total, nil
		}
		n, err := s.store.ExpireBefore(ctx, cutoff, s.batch)
		total += n
		if err != nil {
			return total, err
		}
		if n < s.batch {
			return total, nil
		}
	}
}

// Run sweeps on a ticker until the context is done.
//
// **It sweeps once at start**, because the interval is a day and a process that
// restarts daily would otherwise never sweep at all — the failure `0022` calls
// out as invisible, since a table that is not shrinking looks like nothing.
//
// Two instances sweeping concurrently is harmless: the delete is idempotent and
// the second finds fewer rows. There is deliberately no lease and no leader
// election.
func (s *Sweeper) Run(ctx context.Context) {
	s.once(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.once(ctx)
		}
	}
}

// once logs the outcome, and it is the ONLY record a sweep leaves. It is work,
// so it would belong in the journal — and a journal row about deleting journal
// rows is a row that will itself be deleted.
func (s *Sweeper) once(ctx context.Context) {
	started := s.clock.Now()
	n, err := s.Sweep(ctx)
	if err != nil {
		s.log.ErrorContext(ctx, "journal sweep failed",
			"deleted", n, "retention", s.retention, "error", err)
		return
	}
	if n == 0 {
		// Debug rather than info: the common case is nothing to do, and a daily
		// line saying so buries the days when something happened.
		s.log.DebugContext(ctx, "journal swept",
			"deleted", 0, "retention", s.retention)
		return
	}
	s.log.InfoContext(ctx, "journal swept",
		"deleted", n, "retention", s.retention, "took", s.clock.Now().Sub(started))
}
