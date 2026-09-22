package command

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/researchentity/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type Repository interface {
	Create(context.Context, domain.Record) error
	ByID(context.Context, id.ID, id.ID) (domain.Record, error)
	Save(context.Context, domain.Record) error
	ReplaceObservations(context.Context, id.ID, id.ID, []id.ID) error
	ReplacePlaceGeometry(context.Context, domain.Record) error
	CreateRevision(context.Context, domain.Revision) error
}

type Transactor interface {
	InTx(context.Context, func(context.Context) error) error
}
type Minter interface{ NewID() id.ID }
type Clock interface{ Now() time.Time }

type Records struct {
	repo      Repository
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewRecords(repo Repository, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Records {
	if repo == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("researchentity: NewRecords with a nil dependency")
	}
	return &Records{repo: repo, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (r *Records) Create(ctx context.Context, workspace, author id.ID, kind, name, description string, observations []id.ID) (domain.Record, error) {
	return r.CreateWithPlaceGeometry(ctx, workspace, author, kind, name, description, observations, nil)
}

func (r *Records) CreateWithPlaceGeometry(ctx context.Context, workspace, author id.ID, kind, name, description string, observations []id.ID, geometry *domain.PlaceGeometry) (domain.Record, error) {
	fresh, err := domain.NewWithPlaceGeometry(r.ids.NewID(), workspace, author, kind, name, description, observations, geometry, r.clock.Now())
	if err != nil {
		return domain.Record{}, err
	}
	if err := r.tx.InTx(ctx, func(ctx context.Context) error {
		if err := r.repo.Create(ctx, fresh); err != nil {
			return err
		}
		if err := r.repo.ReplaceObservations(ctx, workspace, fresh.ID, fresh.ObservationIDs); err != nil {
			return err
		}
		if err := r.repo.ReplacePlaceGeometry(ctx, fresh); err != nil {
			return err
		}
		if err := r.repo.CreateRevision(ctx, revision(r.ids, fresh)); err != nil {
			return err
		}
		return r.publish(ctx, workspace, fresh, false, false, false)
	}); err != nil {
		return domain.Record{}, err
	}
	return fresh, nil
}

func (r *Records) Edit(ctx context.Context, workspace, want, editor id.ID, kind, name, description string, observations []id.ID) (domain.Record, error) {
	held, err := r.repo.ByID(ctx, workspace, want)
	if err != nil {
		return domain.Record{}, err
	}
	return r.edit(ctx, workspace, held, editor, kind, name, description, observations, held.PlaceGeometry)
}

func (r *Records) EditWithPlaceGeometry(ctx context.Context, workspace, want, editor id.ID, kind, name, description string, observations []id.ID, geometry *domain.PlaceGeometry) (domain.Record, error) {
	held, err := r.repo.ByID(ctx, workspace, want)
	if err != nil {
		return domain.Record{}, err
	}
	return r.edit(ctx, workspace, held, editor, kind, name, description, observations, geometry)
}

func (r *Records) Archive(ctx context.Context, workspace, want, editor id.ID) (domain.Record, error) {
	held, err := r.repo.ByID(ctx, workspace, want)
	if err != nil {
		return domain.Record{}, err
	}
	next, err := held.Archive(editor, r.clock.Now())
	if err != nil {
		return domain.Record{}, err
	}
	return r.transition(ctx, workspace, next, false, true, false)
}

func (r *Records) Restore(ctx context.Context, workspace, want, editor id.ID) (domain.Record, error) {
	held, err := r.repo.ByID(ctx, workspace, want)
	if err != nil {
		return domain.Record{}, err
	}
	next, err := held.Restore(editor, r.clock.Now())
	if err != nil {
		return domain.Record{}, err
	}
	return r.transition(ctx, workspace, next, false, false, true)
}

// CreateRevision records a snapshot inside a caller-owned transaction. It is
// used by resolution commands when they intentionally change a canonical
// record's cited observations without going through the record HTTP command.
func (r *Records) CreateRevision(ctx context.Context, record domain.Record) error {
	return r.repo.CreateRevision(ctx, revision(r.ids, record))
}

func (r *Records) edit(ctx context.Context, workspace id.ID, held domain.Record, editor id.ID, kind, name, description string, observations []id.ID, geometry *domain.PlaceGeometry) (domain.Record, error) {
	next, err := held.EditWithPlaceGeometry(editor, kind, name, description, observations, geometry, r.clock.Now())
	if err != nil {
		return domain.Record{}, err
	}
	if err := r.tx.InTx(ctx, func(ctx context.Context) error {
		if err := r.repo.Save(ctx, next); err != nil {
			return err
		}
		if err := r.repo.ReplaceObservations(ctx, workspace, next.ID, next.ObservationIDs); err != nil {
			return err
		}
		if err := r.repo.ReplacePlaceGeometry(ctx, next); err != nil {
			return err
		}
		if err := r.repo.CreateRevision(ctx, revision(r.ids, next)); err != nil {
			return err
		}
		return r.publish(ctx, workspace, next, true, false, false)
	}); err != nil {
		return domain.Record{}, err
	}
	return next, nil
}

func (r *Records) transition(ctx context.Context, workspace id.ID, next domain.Record, edit, archive, restore bool) (domain.Record, error) {
	if err := r.tx.InTx(ctx, func(ctx context.Context) error {
		if err := r.repo.Save(ctx, next); err != nil {
			return err
		}
		if err := r.repo.CreateRevision(ctx, revision(r.ids, next)); err != nil {
			return err
		}
		return r.publish(ctx, workspace, next, edit, archive, restore)
	}); err != nil {
		return domain.Record{}, err
	}
	return next, nil
}

func revision(ids Minter, record domain.Record) domain.Revision {
	return domain.NewRevision(ids.NewID(), record)
}

func (r *Records) publish(ctx context.Context, workspace id.ID, record domain.Record, edit, archive, restore bool) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, r.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	event, err := events.NewDecision(r.ids, r.clock, domain.EventChanged, "workspace:"+workspace.String(), prov, domain.Changed{
		WorkspaceID: workspace.String(), RecordID: record.ID.String(), UpdatedBy: record.UpdatedBy.String(), Edit: edit, Archive: archive, Restore: restore,
	})
	if err != nil {
		return err
	}
	return r.publisher.Publish(ctx, event)
}
