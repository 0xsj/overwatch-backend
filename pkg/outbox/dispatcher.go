package outbox

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Config struct {
	Store    Store
	Clock    Clock
	Log      *slog.Logger
	Handlers []events.Handler

	// Wake is an optional nudge: a receive means something MAY be due, never
	// that anything is. The dispatcher does not know what feeds it — a Postgres
	// LISTEN today, a broker later — and correctness never depends on it, which
	// is why Interval remains and is the backstop rather than the fallback.
	Wake <-chan struct{}

	Batch       int
	Interval    time.Duration
	MaxAttempts int
	Backoff     func(attempt int) time.Duration
}

func (c Config) withDefaults() Config {
	if c.Batch == 0 {
		c.Batch = 100
	}
	if c.Interval == 0 {
		c.Interval = time.Second
	}
	if c.MaxAttempts == 0 {
		c.MaxAttempts = 8
	}
	if c.Backoff == nil {
		// Doubling from a second, capped. The cap matters more than the curve:
		// an event nobody is watching should still be retried this hour.
		c.Backoff = func(attempt int) time.Duration {
			d := time.Second << min(attempt, 9)
			return min(d, 10*time.Minute)
		}
	}
	return c
}

type Dispatcher struct {
	cfg Config

	mu    sync.Mutex
	stats Stats
}

type Stats struct {
	Delivered int
	Failed    int
	Buried    int
	Runs      int
}

func New(cfg Config) *Dispatcher {
	// Eagerly, and each by name. A nil here does not fail where it is wired — it
	// fails inside a goroutine, later, on a delivery nobody is watching.
	if cfg.Store == nil {
		panic("outbox: New with a nil Store")
	}
	if cfg.Clock == nil {
		panic("outbox: New with a nil Clock")
	}
	if cfg.Log == nil {
		panic("outbox: New with a nil Logger — use logger.Nop()")
	}
	return &Dispatcher{cfg: cfg.withDefaults()}
}

// Dispatch claims one batch and delivers it. It returns how many were drained,
// so a caller can loop until it returns zero.
func (d *Dispatcher) Dispatch(ctx context.Context) (int, error) {
	now := d.cfg.Clock.Now()
	batch, err := d.cfg.Store.Claim(ctx, d.cfg.Batch, now)
	if err != nil {
		return 0, err
	}
	d.bump(func(s *Stats) { s.Runs++ })
	if len(batch) == 0 {
		return 0, nil
	}

	var done []id.ID
	for _, p := range batch {
		if err := d.deliver(ctx, p.Event); err != nil {
			d.fail(ctx, p, err)
			continue
		}
		done = append(done, p.Event.ID)
	}
	if len(done) > 0 {
		if err := d.cfg.Store.Delivered(ctx, done); err != nil {
			// The handlers ran. Failing to drain means they run again, which is
			// exactly why a handler is idempotent.
			return 0, err
		}
		d.bump(func(s *Stats) { s.Delivered += len(done) })
	}
	return len(done), nil
}

// deliver hands the event to every handler. A panicking handler is contained:
// it is one subscriber's bug, and letting it kill the dispatcher would stop
// every other event being delivered — which is the failure this package's
// poison-isolation rule exists to prevent, arriving by a different route.
func (d *Dispatcher) deliver(ctx context.Context, e events.Event) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = errors.Newf(errors.Internal, "outbox: handler panicked on %s: %v", e.Name, v)
		}
	}()
	for _, h := range d.cfg.Handlers {
		if err := h(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

func (d *Dispatcher) fail(ctx context.Context, p Pending, cause error) {
	attempts := p.Attempts + 1
	ids := []id.ID{p.Event.ID}

	if attempts >= d.cfg.MaxAttempts {
		// Buried, not deleted. The reason it could not be delivered is the only
		// place the bug is visible, and a deleted row takes it with it.
		d.cfg.Log.ErrorContext(ctx, "event buried",
			"event", p.Event.Name, "event_id", p.Event.ID, "attempts", attempts, "cause", cause)
		if err := d.cfg.Store.Failed(ctx, ids, cause.Error(), time.Time{}); err != nil {
			d.cfg.Log.ErrorContext(ctx, "burying failed", "cause", err)
		}
		d.bump(func(s *Stats) { s.Buried++ })
		return
	}

	retryAt := d.cfg.Clock.Now().Add(d.cfg.Backoff(attempts))
	d.cfg.Log.WarnContext(ctx, "event delivery failed",
		"event", p.Event.Name, "event_id", p.Event.ID, "attempts", attempts,
		"retry_at", retryAt, "cause", cause)
	if err := d.cfg.Store.Failed(ctx, ids, cause.Error(), retryAt); err != nil {
		d.cfg.Log.ErrorContext(ctx, "recording a failure failed", "cause", err)
	}
	d.bump(func(s *Stats) { s.Failed++ })
}

// Drain dispatches until nothing is owed. It is what a test calls, and what a
// shutdown calls to avoid leaving delivered work unrecorded.
func (d *Dispatcher) Drain(ctx context.Context) error {
	for {
		n, err := d.Dispatch(ctx)
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
	}
}

// Run polls until the context is cancelled. Cancellation is not a failure.
func (d *Dispatcher) Run(ctx context.Context) error {
	t := time.NewTicker(d.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-d.cfg.Wake:
		case <-t.C:
		}
		if _, err := d.Dispatch(ctx); err != nil && ctx.Err() == nil {
			d.cfg.Log.ErrorContext(ctx, "dispatch failed", "cause", err)
		}
	}
}

func (d *Dispatcher) Depth(ctx context.Context) (Level, error) {
	return d.cfg.Store.Depth(ctx, d.cfg.Clock.Now())
}

func (d *Dispatcher) Stats() Stats {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stats
}

func (d *Dispatcher) bump(f func(*Stats)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	f(&d.stats)
}
