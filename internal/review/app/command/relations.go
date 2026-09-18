package command

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/review/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type Repository interface {
	Upsert(context.Context, domain.Relation) (domain.Relation, error)
}
type Transactor interface {
	InTx(context.Context, func(context.Context) error) error
}
type Minter interface{ NewID() id.ID }
type Clock interface{ Now() time.Time }

type Relations struct {
	repo      Repository
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewRelations(repo Repository, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Relations {
	if repo == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("review: NewRelations with a nil dependency")
	}
	return &Relations{repo: repo, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (r *Relations) Set(ctx context.Context, workspace, author, a, b id.ID, kind, rationale string) (domain.Relation, error) {
	fresh, err := domain.New(r.ids.NewID(), workspace, author, a, b, kind, rationale, r.clock.Now())
	if err != nil {
		return domain.Relation{}, err
	}
	var stored domain.Relation
	err = r.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		stored, err = r.repo.Upsert(ctx, fresh)
		if err != nil {
			return err
		}
		prov, ok := provenance.Current(ctx)
		if !ok {
			prov = provenance.New(provenance.OriginRequest, r.ids)
		}
		prov, err = prov.WithTenant(workspace.String())
		if err != nil {
			return err
		}
		event, err := events.NewDecision(r.ids, r.clock, domain.EventRecorded,
			"workspace:"+workspace.String(), prov, map[string]string{
				"workspace_id":         workspace.String(),
				"relation_id":          stored.ID.String(),
				"left_observation_id":  stored.LeftObservationID.String(),
				"right_observation_id": stored.RightObservationID.String(),
				"kind":                 stored.Kind.String(),
				"author":               stored.Author.String(),
			})
		if err != nil {
			return err
		}
		return r.publisher.Publish(ctx, event)
	})
	if err != nil {
		return domain.Relation{}, err
	}
	return stored, nil
}
