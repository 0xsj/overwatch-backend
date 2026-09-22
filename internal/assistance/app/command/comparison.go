package command

import (
	"context"
	stderrors "errors"
	"strings"
	"unicode/utf8"

	assistapp "github.com/0xsj/overwatch-backend/internal/assistance/app"
	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	pkgerrors "github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type ComparisonRepository interface {
	CreateComparison(context.Context, domain.Comparison) error
	ProviderRunRepository
}

type ComparisonEvidence interface {
	LoadComparisonInput(context.Context, id.ID, []id.ID) (assistapp.ComparisonInput, error)
}

type Comparisons struct {
	repo      ComparisonRepository
	evidence  ComparisonEvidence
	provider  assistapp.ComparisonProvider
	policy    ProviderPolicy
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewComparisons(repo ComparisonRepository, evidence ComparisonEvidence, provider assistapp.ComparisonProvider, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Comparisons {
	if repo == nil || evidence == nil || provider == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("assistance: NewComparisons with a nil dependency")
	}
	return &Comparisons{repo: repo, evidence: evidence, provider: provider, policy: allowAllProviderPolicy{}, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func NewComparisonsWithPolicy(repo ComparisonRepository, evidence ComparisonEvidence, provider assistapp.ComparisonProvider, policy ProviderPolicy, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Comparisons {
	if repo == nil || evidence == nil || provider == nil || policy == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("assistance: NewComparisonsWithPolicy with a nil dependency")
	}
	return &Comparisons{repo: repo, evidence: evidence, provider: provider, policy: policy, tx: tx, publisher: publisher, ids: ids, clock: clock}
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
	timer := startProviderRun(input)
	if providerUsesExternal(c.provider) {
		allowed, err := c.policy.Current(ctx, workspace)
		if err != nil {
			return domain.Comparison{}, err
		}
		if !allowed.AllowExternal {
			failed, buildErr := domain.NewComparisonResult(c.ids.NewID(), workspace, actor, observations, c.provider.Name(), c.provider.Method(), c.provider.TemplateVersion(), domain.ComparisonUnsupported, "", nil, domain.ErrExternalProviderDisabled.Error(), c.clock.Now())
			if buildErr != nil {
				return domain.Comparison{}, buildErr
			}
			run, runErr := timer.finish(failed.ID, workspace, actor, "comparison", failed.Provider, failed.Method, failed.TemplateVersion, failed.Status.String(), failed.Output, failed.Error, false, failed.CreatedAt)
			if runErr != nil {
				return domain.Comparison{}, runErr
			}
			if persistErr := c.persist(ctx, failed, run, actor); persistErr != nil {
				return domain.Comparison{}, persistErr
			}
			return failed, domain.ErrExternalProviderDisabled
		}
	}
	out, err := c.provider.Compare(ctx, input)
	if err != nil {
		status := domain.ComparisonFailed
		if stderrors.Is(err, assistapp.ErrProcessProviderUnavailable) {
			status = domain.ComparisonUnsupported
		} else if pkgerrors.KindOf(err) == pkgerrors.Timeout {
			status = domain.ComparisonTimedOut
		}
		failed, buildErr := domain.NewComparisonResult(c.ids.NewID(), workspace, actor, observations, c.provider.Name(), c.provider.Method(), c.provider.TemplateVersion(), status, "", nil, trimComparisonError(err.Error()), c.clock.Now())
		if buildErr != nil {
			return domain.Comparison{}, buildErr
		}
		run, runErr := timer.finish(failed.ID, workspace, actor, "comparison", failed.Provider, failed.Method, failed.TemplateVersion, failed.Status.String(), failed.Output, failed.Error, status == domain.ComparisonTimedOut, failed.CreatedAt)
		if runErr != nil {
			return domain.Comparison{}, runErr
		}
		if persistErr := c.persist(ctx, failed, run, actor); persistErr != nil {
			return domain.Comparison{}, persistErr
		}
		return failed, err
	}
	fresh, err := domain.NewComparison(c.ids.NewID(), workspace, actor, observations, c.provider.Name(), c.provider.Method(), c.provider.TemplateVersion(), out.Status, out.Text, out.Findings, c.clock.Now())
	if err != nil {
		return domain.Comparison{}, err
	}
	run, err := timer.finish(fresh.ID, workspace, actor, "comparison", fresh.Provider, fresh.Method, fresh.TemplateVersion, fresh.Status.String(), fresh.Output, fresh.Error, false, fresh.CreatedAt)
	if err != nil {
		return domain.Comparison{}, err
	}
	if err := c.persist(ctx, fresh, run, actor); err != nil {
		return domain.Comparison{}, err
	}
	return fresh, nil
}

func (c *Comparisons) persist(ctx context.Context, fresh domain.Comparison, run domain.ProviderRun, actor id.ID) error {
	return c.tx.InTx(ctx, func(ctx context.Context) error {
		if err := c.repo.CreateComparison(ctx, fresh); err != nil {
			return err
		}
		if err := c.repo.CreateProviderRun(ctx, run); err != nil {
			return err
		}
		prov, ok := provenance.Current(ctx)
		if !ok {
			prov = provenance.New(provenance.OriginRequest, c.ids)
		}
		prov, err := prov.WithTenant(fresh.WorkspaceID.String())
		if err != nil {
			return err
		}
		event, err := events.NewDecision(c.ids, c.clock, domain.EventComparisonGenerated, "workspace:"+fresh.WorkspaceID.String(), prov, map[string]any{
			"workspace_id": fresh.WorkspaceID.String(), "comparison_id": fresh.ID.String(), "observation_count": len(fresh.ObservationIDs), "finding_count": len(fresh.Findings), "provider": fresh.Provider, "method": fresh.Method, "template_version": fresh.TemplateVersion, "status": fresh.Status.String(), "error": fresh.Error, "actor": actor.String(),
		})
		if err != nil {
			return err
		}
		return c.publisher.Publish(ctx, event)
	})
}

func trimComparisonError(raw string) string {
	failure := strings.TrimSpace(raw)
	if len(failure) <= domain.MaxComparisonError {
		return failure
	}
	failure = failure[:domain.MaxComparisonError]
	for !utf8.ValidString(failure) {
		failure = failure[:len(failure)-1]
	}
	return failure
}
