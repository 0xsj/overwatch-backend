package limit

import (
	"context"
	"sync"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
)

const DefaultMaxKeys = 4096

var ErrNoBudget = errors.New(errors.RateLimited, "no budget left for that key")

type Clock interface {
	Now() time.Time
}

// Rule is Burst tokens, refilling one every Every.
type Rule struct {
	Burst int
	Every time.Duration
}

type Config struct {
	Rule    Rule
	Clock   Clock
	MaxKeys int
}

type bucket struct {
	tokens float64
	seen   time.Time
}

type Limiter struct {
	rule    Rule
	clock   Clock
	maxKeys int

	mu      sync.Mutex
	buckets map[string]*bucket
}

func New(cfg Config) *Limiter {
	if cfg.Rule.Burst <= 0 {
		panic("limit: New with a burst of zero — a limiter that permits nothing is a bug, not a policy")
	}
	if cfg.Rule.Every <= 0 {
		panic("limit: New with no refill interval — the bucket would never refill")
	}
	if cfg.Clock == nil {
		panic("limit: New with a nil Clock")
	}
	if cfg.MaxKeys <= 0 {
		cfg.MaxKeys = DefaultMaxKeys
	}
	return &Limiter{
		rule: cfg.Rule, clock: cfg.Clock, maxKeys: cfg.MaxKeys,
		buckets: make(map[string]*bucket),
	}
}

// Allow takes a token if there is one. It never blocks and never errors: a
// caller that must refuse needs an answer, not a delay.
func (l *Limiter) Allow(key string) bool {
	_, ok := l.reserve(key)
	return ok
}

// Wait blocks until a token is available or ctx ends. It is the collector's
// shape: a host budget should slow the work down rather than fail it.
func (l *Limiter) Wait(ctx context.Context, key string) error {
	for {
		wait, ok := l.reserve(key)
		if ok {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return errors.Wrap(err, errors.Canceled, "limit: waiting for "+key)
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return errors.Wrap(ctx.Err(), errors.Canceled, "limit: waiting for "+key)
		case <-t.C:
		}
	}
}

// Tokens is what is left, for a caller that wants to report rather than act.
func (l *Limiter) Tokens(key string) float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.bucketFor(key, l.clock.Now())
	return b.tokens
}

func (l *Limiter) reserve(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clock.Now()
	b := l.bucketFor(key, now)
	if b.tokens >= 1 {
		b.tokens--
		return 0, true
	}
	// How long until one token exists. Reported so a waiter sleeps once rather
	// than spinning.
	missing := 1 - b.tokens
	return time.Duration(missing * float64(l.rule.Every)), false
}

func (l *Limiter) bucketFor(key string, now time.Time) *bucket {
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= l.maxKeys {
			l.evict(now)
		}
		b = &bucket{tokens: float64(l.rule.Burst)}
		l.buckets[key] = b
	} else {
		elapsed := now.Sub(b.seen)
		if elapsed > 0 {
			b.tokens += float64(elapsed) / float64(l.rule.Every)
			if b.tokens > float64(l.rule.Burst) {
				b.tokens = float64(l.rule.Burst)
			}
		}
	}
	b.seen = now
	return b
}

// evict drops FULL buckets first, because a full bucket and a missing one answer
// every future question identically — so that half of the sweep gives up
// nothing. Only when that is not enough does it drop the oldest, which is the
// one case where a caller loses accumulated state.
func (l *Limiter) evict(now time.Time) {
	full := float64(l.rule.Burst)
	target := l.maxKeys / 2

	for key, b := range l.buckets {
		refilled := b.tokens + float64(now.Sub(b.seen))/float64(l.rule.Every)
		if refilled >= full {
			delete(l.buckets, key)
		}
		if len(l.buckets) <= target {
			return
		}
	}
	var oldest string
	for len(l.buckets) > target {
		oldest = ""
		var at time.Time
		for key, b := range l.buckets {
			if oldest == "" || b.seen.Before(at) {
				oldest, at = key, b.seen
			}
		}
		delete(l.buckets, oldest)
	}
}
