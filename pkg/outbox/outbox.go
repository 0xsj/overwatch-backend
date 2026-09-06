package outbox

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Pending is a claimed row: the event, plus what delivery has learned about it.
type Pending struct {
	Event    events.Event
	Attempts int
}

// Store is a real port — the postgres adapter is what runs, and the memory one
// exists so the dispatcher's POLICY can be tested with no database.
type Store interface {
	// Add writes events through whatever the context's transaction is. It is the
	// only method that must run inside the caller's transaction.
	Add(ctx context.Context, evs ...events.Event) error

	// Claim takes up to n undelivered events that are due, marking them so a
	// concurrent dispatcher takes different ones.
	Claim(ctx context.Context, n int, now time.Time) ([]Pending, error)

	// Delivered drains the rows: the handlers are done with them.
	Delivered(ctx context.Context, ids []id.ID) error

	// Failed records why, and when to try again. An empty retryAt buries the row.
	Failed(ctx context.Context, ids []id.ID, reason string, retryAt time.Time) error

	// Depth is the health metric: how many are owed, and how old the oldest is.
	Depth(ctx context.Context, now time.Time) (Level, error)
}

type Level struct {
	Pending int
	Buried  int
	Oldest  time.Duration
}

type Clock interface {
	Now() time.Time
}

// Publisher satisfies events.Publisher. It is a thin thing on purpose: the
// transaction comes from the context, so there is nothing here to configure and
// nothing to get wrong except forgetting the transaction.
type Publisher struct {
	store Store
}

func NewPublisher(s Store) *Publisher {
	if s == nil {
		panic("outbox: NewPublisher with a nil Store")
	}
	return &Publisher{store: s}
}

func (p *Publisher) Publish(ctx context.Context, evs ...events.Event) error {
	if len(evs) == 0 {
		return nil
	}
	for _, e := range evs {
		// An event with no identity cannot be deduplicated, and at-least-once
		// delivery makes deduplication the handler's only defence.
		if e.IsZero() {
			return errors.New(errors.Internal, "outbox: publish an event with no id")
		}
		if !events.ValidName(e.Name) {
			return errors.Newf(errors.Internal, "outbox: publish %q, which is not a name", e.Name)
		}
	}
	return p.store.Add(ctx, evs...)
}
