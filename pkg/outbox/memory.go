package outbox

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Memory is the second adapter, and it is what makes Store a port rather than an
// interface with one implementation. It exists so the dispatcher's POLICY —
// backoff, burial, poison isolation — is testable with no database, which is
// where those bugs actually live.
//
// It does not model transactions. `Add` outside a transaction is exactly the
// mistake the postgres adapter cannot catch either, so the memory one does not
// pretend to.
type Memory struct {
	mu   sync.Mutex
	rows map[id.ID]*row
}

type row struct {
	event    events.Event
	attempts int
	dueAt    time.Time
	claimed  bool
	buried   bool
	reason   string
	written  int
}

func NewMemory() *Memory { return &Memory{rows: map[id.ID]*row{}} }

func (m *Memory) Add(_ context.Context, evs ...events.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range evs {
		if _, seen := m.rows[e.ID]; seen {
			continue // idempotent by event id, like the real one
		}
		m.rows[e.ID] = &row{event: e, written: len(m.rows)}
	}
	return nil
}

func (m *Memory) Claim(_ context.Context, n int, now time.Time) ([]Pending, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var due []*row
	for _, r := range m.rows {
		if r.buried || r.claimed || r.dueAt.After(now) {
			continue
		}
		due = append(due, r)
	}
	sort.Slice(due, func(i, j int) bool { return due[i].written < due[j].written })
	if len(due) > n {
		due = due[:n]
	}
	out := make([]Pending, 0, len(due))
	for _, r := range due {
		r.claimed = true
		out = append(out, Pending{Event: r.event, Attempts: r.attempts})
	}
	return out, nil
}

func (m *Memory) Delivered(_ context.Context, ids []id.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, i := range ids {
		delete(m.rows, i)
	}
	return nil
}

func (m *Memory) Failed(_ context.Context, ids []id.ID, reason string, retryAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, i := range ids {
		r, ok := m.rows[i]
		if !ok {
			continue
		}
		r.attempts++
		r.claimed = false
		r.reason = reason
		if retryAt.IsZero() {
			r.buried = true
			continue
		}
		r.dueAt = retryAt
	}
	return nil
}

func (m *Memory) Depth(_ context.Context, now time.Time) (Level, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var l Level
	for _, r := range m.rows {
		if r.buried {
			l.Buried++
			continue
		}
		l.Pending++
		if age := now.Sub(r.event.OccurredAt); age > l.Oldest {
			l.Oldest = age
		}
	}
	return l, nil
}

// Buried is for tests and for an operator's read. Nothing in the delivery path
// uses it.
func (m *Memory) Buried() []events.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []events.Event
	for _, r := range m.rows {
		if r.buried {
			out = append(out, r.event)
		}
	}
	return out
}
