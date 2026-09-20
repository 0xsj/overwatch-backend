package command

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type ProviderPolicyRepository interface {
	SaveProviderPolicy(context.Context, domain.ProviderPolicy) error
}

type ProviderPolicies struct {
	repo      ProviderPolicyRepository
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewProviderPolicies(repo ProviderPolicyRepository, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *ProviderPolicies {
	if repo == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("assistance: NewProviderPolicies with a nil dependency")
	}
	return &ProviderPolicies{repo: repo, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (p *ProviderPolicies) Set(ctx context.Context, workspace, actor id.ID, allowExternal bool) (domain.ProviderPolicy, error) {
	policy, err := domain.NewProviderPolicy(workspace, actor, allowExternal, p.clock.Now())
	if err != nil {
		return domain.ProviderPolicy{}, err
	}
	if err := p.tx.InTx(ctx, func(ctx context.Context) error {
		if err := p.repo.SaveProviderPolicy(ctx, policy); err != nil {
			return err
		}
		prov, ok := provenance.Current(ctx)
		if !ok {
			prov = provenance.New(provenance.OriginRequest, p.ids)
		}
		prov, err := prov.WithTenant(workspace.String())
		if err != nil {
			return err
		}
		event, err := events.NewDecision(p.ids, p.clock, domain.EventProviderPolicyUpdated, "workspace:"+workspace.String(), prov, map[string]any{
			"workspace_id": workspace.String(), "allow_external": allowExternal, "actor": actor.String(),
		})
		if err != nil {
			return err
		}
		return p.publisher.Publish(ctx, event)
	}); err != nil {
		return domain.ProviderPolicy{}, err
	}
	return policy, nil
}
