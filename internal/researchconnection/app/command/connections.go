package command

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/researchconnection/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type Repository interface {
	Create(context.Context, domain.Connection) error
	ByID(context.Context, id.ID, id.ID) (domain.Connection, error)
	Save(context.Context, domain.Connection) error
	ReplaceEvidence(context.Context, id.ID, id.ID, []id.ID, []id.ID) error
	CreateRevision(context.Context, domain.Revision) error
}
type Transactor interface {
	InTx(context.Context, func(context.Context) error) error
}
type Minter interface{ NewID() id.ID }
type Clock interface{ Now() time.Time }

type Connections struct {
	repo      Repository
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewConnections(repo Repository, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Connections {
	if repo == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("researchconnection: NewConnections with a nil dependency")
	}
	return &Connections{repo: repo, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (c *Connections) Create(ctx context.Context, workspace, author, from, to id.ID, kind, state, rationale string, supporting, opposing []id.ID) (domain.Connection, error) {
	fresh, err := domain.New(c.ids.NewID(), workspace, author, from, to, kind, state, rationale, supporting, opposing, c.clock.Now())
	if err != nil {
		return domain.Connection{}, err
	}
	if err := c.tx.InTx(ctx, func(ctx context.Context) error {
		if err := c.repo.Create(ctx, fresh); err != nil {
			return err
		}
		if err := c.repo.ReplaceEvidence(ctx, workspace, fresh.ID, fresh.SupportingObservationIDs, fresh.OpposingObservationIDs); err != nil {
			return err
		}
		if err := c.repo.CreateRevision(ctx, revision(c.ids, fresh)); err != nil {
			return err
		}
		return c.publish(ctx, workspace, fresh, false)
	}); err != nil {
		return domain.Connection{}, err
	}
	return fresh, nil
}

func (c *Connections) Edit(ctx context.Context, workspace, want, editor id.ID, kind, state, rationale string, supporting, opposing []id.ID) (domain.Connection, error) {
	held, err := c.repo.ByID(ctx, workspace, want)
	if err != nil {
		return domain.Connection{}, err
	}
	next, err := held.Edit(editor, kind, state, rationale, supporting, opposing, c.clock.Now())
	if err != nil {
		return domain.Connection{}, err
	}
	if err := c.tx.InTx(ctx, func(ctx context.Context) error {
		if err := c.repo.Save(ctx, next); err != nil {
			return err
		}
		if err := c.repo.ReplaceEvidence(ctx, workspace, next.ID, next.SupportingObservationIDs, next.OpposingObservationIDs); err != nil {
			return err
		}
		if err := c.repo.CreateRevision(ctx, revision(c.ids, next)); err != nil {
			return err
		}
		return c.publish(ctx, workspace, next, true)
	}); err != nil {
		return domain.Connection{}, err
	}
	return next, nil
}

func revision(ids Minter, connection domain.Connection) domain.Revision {
	return domain.Revision{
		ID: ids.NewID(), WorkspaceID: connection.WorkspaceID, ConnectionID: connection.ID,
		FromRecordID: connection.FromRecordID, ToRecordID: connection.ToRecordID,
		Kind: connection.Kind, State: connection.State, Rationale: connection.Rationale,
		SupportingObservationIDs: append([]id.ID(nil), connection.SupportingObservationIDs...),
		OpposingObservationIDs:   append([]id.ID(nil), connection.OpposingObservationIDs...),
		ChangedBy:                connection.UpdatedBy, ChangedAt: connection.UpdatedAt,
	}
}

func (c *Connections) publish(ctx context.Context, workspace id.ID, connection domain.Connection, edit bool) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, c.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	event, err := events.NewDecision(c.ids, c.clock, domain.EventChanged, "workspace:"+workspace.String(), prov, domain.Changed{
		WorkspaceID: workspace.String(), ConnectionID: connection.ID.String(), UpdatedBy: connection.UpdatedBy.String(), Edit: edit,
	})
	if err != nil {
		return err
	}
	return c.publisher.Publish(ctx, event)
}
