package command

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/event/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type Repository interface {
	Create(context.Context, domain.Event) error
	ByID(context.Context, id.ID, id.ID) (domain.Event, error)
	Save(context.Context, domain.Event) error
	ReplaceObservations(context.Context, id.ID, id.ID, []id.ID) error
}
type Transactor interface {
	InTx(context.Context, func(context.Context) error) error
}
type Minter interface{ NewID() id.ID }
type Clock interface{ Now() time.Time }

type Events struct {
	repo      Repository
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewEvents(repo Repository, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Events {
	if repo == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("event: NewEvents with a nil dependency")
	}
	return &Events{repo: repo, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (e *Events) Create(ctx context.Context, workspace, author id.ID, title, description, reportedTime, precision, sortDate, location string, observations []id.ID) (domain.Event, error) {
	fresh, err := domain.New(e.ids.NewID(), workspace, author, title, description, reportedTime, precision, sortDate, location, observations, e.clock.Now())
	if err != nil {
		return domain.Event{}, err
	}
	if err := e.tx.InTx(ctx, func(ctx context.Context) error {
		if err := e.repo.Create(ctx, fresh); err != nil {
			return err
		}
		if err := e.repo.ReplaceObservations(ctx, workspace, fresh.ID, fresh.ObservationIDs); err != nil {
			return err
		}
		return e.publish(ctx, workspace, fresh, false)
	}); err != nil {
		return domain.Event{}, err
	}
	return fresh, nil
}

func (e *Events) Edit(ctx context.Context, workspace, want, editor id.ID, title, description, reportedTime, precision, sortDate, location string, observations []id.ID) (domain.Event, error) {
	held, err := e.repo.ByID(ctx, workspace, want)
	if err != nil {
		return domain.Event{}, err
	}
	next, err := held.Edit(editor, title, description, reportedTime, precision, sortDate, location, observations, e.clock.Now())
	if err != nil {
		return domain.Event{}, err
	}
	if err := e.tx.InTx(ctx, func(ctx context.Context) error {
		if err := e.repo.Save(ctx, next); err != nil {
			return err
		}
		if err := e.repo.ReplaceObservations(ctx, workspace, next.ID, next.ObservationIDs); err != nil {
			return err
		}
		return e.publish(ctx, workspace, next, true)
	}); err != nil {
		return domain.Event{}, err
	}
	return next, nil
}

func (e *Events) publish(ctx context.Context, workspace id.ID, event domain.Event, edit bool) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, e.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	out, err := events.NewDecision(e.ids, e.clock, domain.EventChanged, "workspace:"+workspace.String(), prov, map[string]any{
		"workspace_id": workspace.String(), "event_id": event.ID.String(), "updated_by": event.UpdatedBy.String(), "edit": edit,
	})
	if err != nil {
		return err
	}
	return e.publisher.Publish(ctx, out)
}
