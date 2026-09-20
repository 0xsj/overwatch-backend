package command

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/event/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type ClusterRepository interface {
	CreateCluster(context.Context, domain.Cluster) error
	ClusterByID(context.Context, id.ID, id.ID) (domain.Cluster, error)
	SaveCluster(context.Context, domain.Cluster) error
}

type Clusters struct {
	repo      ClusterRepository
	events    EventReader
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewClusters(repo ClusterRepository, eventReader EventReader, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Clusters {
	if repo == nil || eventReader == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("event: NewClusters with a nil dependency")
	}
	return &Clusters{repo: repo, events: eventReader, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (c *Clusters) Create(ctx context.Context, workspace, author id.ID, title, description string, eventIDs []id.ID) (domain.Cluster, error) {
	if err := c.validateEvents(ctx, workspace, eventIDs); err != nil {
		return domain.Cluster{}, err
	}
	fresh, err := domain.NewCluster(c.ids.NewID(), workspace, author, title, description, eventIDs, c.clock.Now())
	if err != nil {
		return domain.Cluster{}, err
	}
	if err := c.tx.InTx(ctx, func(ctx context.Context) error {
		if err := c.repo.CreateCluster(ctx, fresh); err != nil {
			return err
		}
		return c.publish(ctx, "timeline.event.cluster.recorded", fresh)
	}); err != nil {
		return domain.Cluster{}, err
	}
	return fresh, nil
}

func (c *Clusters) Edit(ctx context.Context, workspace, cluster, author id.ID, title, description string, eventIDs []id.ID) (domain.Cluster, error) {
	if err := c.validateEvents(ctx, workspace, eventIDs); err != nil {
		return domain.Cluster{}, err
	}
	var saved domain.Cluster
	if err := c.tx.InTx(ctx, func(ctx context.Context) error {
		current, err := c.repo.ClusterByID(ctx, workspace, cluster)
		if err != nil {
			return err
		}
		saved, err = current.Edit(author, title, description, eventIDs, c.clock.Now())
		if err != nil {
			return err
		}
		if err := c.repo.SaveCluster(ctx, saved); err != nil {
			return err
		}
		return c.publish(ctx, "timeline.event.cluster.updated", saved)
	}); err != nil {
		return domain.Cluster{}, err
	}
	return saved, nil
}

func (c *Clusters) Review(ctx context.Context, workspace, cluster, reviewer id.ID, state domain.ClusterState, note string) (domain.Cluster, error) {
	var saved domain.Cluster
	if err := c.tx.InTx(ctx, func(ctx context.Context) error {
		current, err := c.repo.ClusterByID(ctx, workspace, cluster)
		if err != nil {
			return err
		}
		saved, err = current.Review(reviewer, state, note, c.clock.Now())
		if err != nil {
			return err
		}
		if err := c.repo.SaveCluster(ctx, saved); err != nil {
			return err
		}
		return c.publish(ctx, "timeline.event.cluster.reviewed", saved)
	}); err != nil {
		return domain.Cluster{}, err
	}
	return saved, nil
}

func (c *Clusters) validateEvents(ctx context.Context, workspace id.ID, eventIDs []id.ID) error {
	for _, eventID := range eventIDs {
		if _, err := c.events.ByID(ctx, workspace, eventID); err != nil {
			return err
		}
	}
	return nil
}

func (c *Clusters) publish(ctx context.Context, name string, cluster domain.Cluster) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, c.ids)
	}
	prov, err := prov.WithTenant(cluster.WorkspaceID.String())
	if err != nil {
		return err
	}
	event, err := events.NewDecision(c.ids, c.clock, name, "workspace:"+cluster.WorkspaceID.String(), prov, map[string]string{
		"workspace_id": cluster.WorkspaceID.String(), "cluster_id": cluster.ID.String(), "state": string(cluster.State), "updated_by": cluster.UpdatedBy.String(),
	})
	if err != nil {
		return err
	}
	return c.publisher.Publish(ctx, event)
}
