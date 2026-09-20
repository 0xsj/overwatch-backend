package command

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/review/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type ClusterRepository interface {
	Create(context.Context, domain.Cluster) (domain.Cluster, error)
	ByID(context.Context, id.ID, id.ID) (domain.Cluster, error)
	Update(context.Context, domain.Cluster) (domain.Cluster, error)
}

type ClusterTransactor interface {
	InTx(context.Context, func(context.Context) error) error
}

type ClusterMinter interface{ NewID() id.ID }
type ClusterClock interface{ Now() time.Time }

type Clusters struct {
	repo      ClusterRepository
	tx        ClusterTransactor
	publisher events.Publisher
	ids       ClusterMinter
	clock     ClusterClock
}

func NewClusters(repo ClusterRepository, tx ClusterTransactor, publisher events.Publisher, ids ClusterMinter, clock ClusterClock) *Clusters {
	if repo == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("review: NewClusters with a nil dependency")
	}
	return &Clusters{repo: repo, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (c *Clusters) Create(ctx context.Context, workspace, author id.ID, kind, title, description string, observations []id.ID) (domain.Cluster, error) {
	fresh, err := domain.NewCluster(c.ids.NewID(), workspace, author, kind, title, description, observations, c.clock.Now())
	if err != nil {
		return domain.Cluster{}, err
	}
	var stored domain.Cluster
	err = c.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		stored, err = c.repo.Create(ctx, fresh)
		if err != nil {
			return err
		}
		return c.publish(ctx, domain.ClusterRecorded, stored)
	})
	if err != nil {
		return domain.Cluster{}, err
	}
	return stored, nil
}

func (c *Clusters) Edit(ctx context.Context, workspace, want, author id.ID, kind, title, description string, observations []id.ID) (domain.Cluster, error) {
	var stored domain.Cluster
	err := c.tx.InTx(ctx, func(ctx context.Context) error {
		current, err := c.repo.ByID(ctx, workspace, want)
		if err != nil {
			return err
		}
		fresh, err := current.Edit(author, kind, title, description, observations, c.clock.Now())
		if err != nil {
			return err
		}
		stored, err = c.repo.Update(ctx, fresh)
		if err != nil {
			return err
		}
		return c.publish(ctx, domain.ClusterUpdated, stored)
	})
	if err != nil {
		return domain.Cluster{}, err
	}
	return stored, nil
}

func (c *Clusters) publish(ctx context.Context, name string, cluster domain.Cluster) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, c.ids)
	}
	var err error
	prov, err = prov.WithTenant(cluster.WorkspaceID.String())
	if err != nil {
		return err
	}
	event, err := events.NewDecision(c.ids, c.clock, name, "workspace:"+cluster.WorkspaceID.String(), prov, map[string]string{
		"workspace_id": cluster.WorkspaceID.String(), "cluster_id": cluster.ID.String(),
		"kind": cluster.Kind.String(), "author": cluster.UpdatedBy.String(),
	})
	if err != nil {
		return err
	}
	return c.publisher.Publish(ctx, event)
}
