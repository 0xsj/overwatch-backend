package clock

import (
	"context"
	"sort"
	"sync"
	"time"
)

type Fake struct {
	mu      sync.Mutex
	wall    time.Time
	elapsed time.Duration
	waiters []*waiter
	seq     uint64
}

type waiter struct {
	deadline time.Duration
	seq      uint64
	ch       chan time.Time
}

func NewFake(at time.Time) *Fake { return &Fake{wall: at.UTC()} }

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.wall
}

func (f *Fake) Elapsed() time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.elapsed
}

func (f *Fake) Advance(d time.Duration) time.Time {
	if d < 0 {
		panic("clock: Fake.Advance with a negative duration — monotonic time does not go backwards; use Set to move the wall clock back")
	}
	f.mu.Lock()
	f.wall = f.wall.Add(d)
	f.elapsed += d
	due := f.take()
	now := f.wall
	f.mu.Unlock()
	for _, w := range due {
		w.ch <- now
	}
	return now
}

func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wall = t.UTC()
}

func (f *Fake) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.Advance(d)
	return nil
}

func (f *Fake) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := &waiter{deadline: f.elapsed + d, seq: f.seq, ch: make(chan time.Time, 1)}
	f.seq++
	f.waiters = append(f.waiters, w)
	return w.ch
}

func (f *Fake) take() []*waiter {
	var due, rest []*waiter
	for _, w := range f.waiters {
		if w.deadline <= f.elapsed {
			due = append(due, w)
		} else {
			rest = append(rest, w)
		}
	}
	f.waiters = rest
	sort.Slice(due, func(i, j int) bool {
		if due[i].deadline != due[j].deadline {
			return due[i].deadline < due[j].deadline
		}
		return due[i].seq < due[j].seq
	})
	return due
}
