package app

import (
	"context"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/audit/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Ledger interface {
	Append(ctx context.Context, e domain.Entry) (bool, error)
}

type Minter interface {
	NewID() id.ID
}

type Clock interface {
	Now() time.Time
}

type Subscriber struct {
	ledger Ledger
	ids    Minter
	clock  Clock
}

func NewSubscriber(ledger Ledger, ids Minter, clock Clock) *Subscriber {
	if ledger == nil || ids == nil || clock == nil {
		panic("audit: NewSubscriber with a nil dependency")
	}
	return &Subscriber{ledger: ledger, ids: ids, clock: clock}
}

func (s *Subscriber) Handle(ctx context.Context, e events.Event) error {
	if !e.Decision {
		return nil
	}
	entry, err := domain.FromEvent(s.ids.NewID(), e, s.clock.Now())
	if err != nil {
		if errors.Is(err, domain.ErrNotADecision) {
			return nil
		}
		return fmt.Errorf("audit: build entry for %s: %w", e.Name, err)
	}
	if _, err := s.ledger.Append(ctx, entry); err != nil {
		return fmt.Errorf("audit: record %s: %w", e.Name, err)
	}
	return nil
}
