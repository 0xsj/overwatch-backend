package command

import (
	"context"

	assistapp "github.com/0xsj/overwatch-backend/internal/assistance/app"
	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type ComparisonRepository interface {
	CreateComparison(context.Context, domain.Comparison) error
}

type ComparisonEvidence interface {
	LoadComparisonInput(context.Context, id.ID, []id.ID) (assistapp.ComparisonInput, error)
}

type Comparisons struct {
	repo      ComparisonRepository
	evidence  ComparisonEvidence
	provider  assistapp.ComparisonProvider
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewComparisons(repo ComparisonRepository, evidence ComparisonEvidence, provider assistapp.ComparisonProvider, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Comparisons {
	if repo == nil || evidence == nil || provider == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("assistance: NewComparisons with a nil dependency")
	}
	return &Comparisons{repo: repo, evidence: evidence, provider: provider, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (c *Comparisons) Generate(ctx context.Context, workspace id.ID, observations []id.ID, actor id.ID) (domain.Comparison, error) {
	if workspace.IsZero() || actor.IsZero() {
		return domain.Comparison{}, domain.ErrIDRequired
	}
	if len(observations) == 0 {
		return domain.Comparison{}, domain.ErrSynthesisRequired
	}
	if len(observations) > domain.MaxComparisonObservations {
		return domain.Comparison{}, domain.ErrSynthesisTooLarge
	}
	input, err := c.evidence.LoadComparisonInput(ctx, workspace, observations)
	if err != nil {
		return domain.Comparison{}, err
	}
	if len(input.Observations) != len(observations) {
		return domain.Comparison{}, domain.ErrCandidateInvalid
	}
	out, err := c.provider.Compare(ctx, input)
	if err != nil {
		return domain.Comparison{}, err
	}
	fresh, err := domain.NewComparison(c.ids.NewID(), workspace, actor, observations, c.provider.Name(), c.provider.Method(), c.provider.TemplateVersion(), out.Status, out.Text, out.Findings, c.clock.Now())
	if err != nil {
		return domain.Comparison{}, err
	}
	if err := c.tx.InTx(ctx, func(ctx context.Context) error {
		if err := c.repo.CreateComparison(ctx, fresh); err != nil {
			return err
		}
		prov, ok := provenance.Current(ctx)
		if !ok {
			prov = provenance.New(provenance.OriginRequest, c.ids)
		}
		prov, err := prov.WithTenant(workspace.String())
		if err != nil {
			return err
		}
		event, err := events.NewDecision(c.ids, c.clock, domain.EventComparisonGenerated, "workspace:"+workspace.String(), prov, map[string]any{
			"workspace_id": workspace.String(), "comparison_id": fresh.ID.String(), "observation_count": len(fresh.ObservationIDs), "finding_count": len(fresh.Findings), "provider": fresh.Provider, "method": fresh.Method, "template_version": fresh.TemplateVersion, "status": fresh.Status.String(), "actor": actor.String(),
		})
		if err != nil {
			return err
		}
		return c.publisher.Publish(ctx, event)
	}); err != nil {
		return domain.Comparison{}, err
	}
	return fresh, nil
}
