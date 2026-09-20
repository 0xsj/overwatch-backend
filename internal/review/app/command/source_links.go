package command

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/review/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type SourceLinkRepository interface {
	UpsertSourceLink(context.Context, domain.SourceLink) (domain.SourceLink, error)
}

type SourceLinkTransactor interface {
	InTx(context.Context, func(context.Context) error) error
}

type SourceLinkMinter interface{ NewID() id.ID }
type SourceLinkClock interface{ Now() time.Time }

type SourceLinks struct {
	repo      SourceLinkRepository
	tx        SourceLinkTransactor
	publisher events.Publisher
	ids       SourceLinkMinter
	clock     SourceLinkClock
}

func NewSourceLinks(repo SourceLinkRepository, tx SourceLinkTransactor, publisher events.Publisher, ids SourceLinkMinter, clock SourceLinkClock) *SourceLinks {
	if repo == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("review: NewSourceLinks with a nil dependency")
	}
	return &SourceLinks{repo: repo, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (s *SourceLinks) Set(ctx context.Context, workspace, author, downstream, upstream id.ID, rationale string) (domain.SourceLink, error) {
	fresh, err := domain.NewSourceLink(s.ids.NewID(), workspace, author, downstream, upstream, rationale, s.clock.Now())
	if err != nil {
		return domain.SourceLink{}, err
	}
	var stored domain.SourceLink
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		stored, err = s.repo.UpsertSourceLink(ctx, fresh)
		if err != nil {
			return err
		}
		prov, ok := provenance.Current(ctx)
		if !ok {
			prov = provenance.New(provenance.OriginRequest, s.ids)
		}
		prov, err = prov.WithTenant(workspace.String())
		if err != nil {
			return err
		}
		event, err := events.NewDecision(s.ids, s.clock, domain.SourceLinkRecorded, "workspace:"+workspace.String(), prov, map[string]string{
			"workspace_id": workspace.String(), "source_link_id": stored.ID.String(),
			"downstream_observation_id": stored.DownstreamObservationID.String(), "upstream_observation_id": stored.UpstreamObservationID.String(),
			"author": stored.Author.String(),
		})
		if err != nil {
			return err
		}
		return s.publisher.Publish(ctx, event)
	})
	if err != nil {
		return domain.SourceLink{}, err
	}
	return stored, nil
}
