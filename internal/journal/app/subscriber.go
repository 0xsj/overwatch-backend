package app

import (
	"context"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/journal/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Log interface {
	Append(ctx context.Context, l domain.Line) (bool, error)
}

type Minter interface {
	NewID() id.ID
}

type Clock interface {
	Now() time.Time
}

type Subscriber struct {
	log   Log
	ids   Minter
	clock Clock
}

func NewSubscriber(log Log, ids Minter, clock Clock) *Subscriber {
	if log == nil || ids == nil || clock == nil {
		panic("journal: NewSubscriber with a nil dependency")
	}
	return &Subscriber{log: log, ids: ids, clock: clock}
}

func (s *Subscriber) Handle(ctx context.Context, e events.Event) error {
	l, err := domain.FromEvent(s.ids.NewID(), e, s.clock.Now())
	if err != nil {
		return fmt.Errorf("journal: build line for %s: %w", e.Name, err)
	}
	if _, err := s.log.Append(ctx, l); err != nil {
		return fmt.Errorf("journal: record %s: %w", e.Name, err)
	}
	return nil
}
