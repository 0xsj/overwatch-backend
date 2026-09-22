package command

import (
	"context"
	"strings"
	"unicode/utf8"

	assistapp "github.com/0xsj/overwatch-backend/internal/assistance/app"
	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	pkgerrors "github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type BriefDraftRepository interface {
	CreateBriefDraft(context.Context, domain.BriefDraft) error
	ProviderRunRepository
}

type BriefDraftEvidence interface {
	LoadBriefDraftInput(context.Context, id.ID, []id.ID) (assistapp.BriefDraftInput, error)
}

type BriefDrafts struct {
	repo      BriefDraftRepository
	evidence  BriefDraftEvidence
	provider  assistapp.BriefDraftProvider
	policy    ProviderPolicy
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewBriefDrafts(repo BriefDraftRepository, evidence BriefDraftEvidence, provider assistapp.BriefDraftProvider, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *BriefDrafts {
	if repo == nil || evidence == nil || provider == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("assistance: NewBriefDrafts with a nil dependency")
	}
	return &BriefDrafts{repo: repo, evidence: evidence, provider: provider, policy: allowAllProviderPolicy{}, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func NewBriefDraftsWithPolicy(repo BriefDraftRepository, evidence BriefDraftEvidence, provider assistapp.BriefDraftProvider, policy ProviderPolicy, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *BriefDrafts {
	if repo == nil || evidence == nil || provider == nil || policy == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("assistance: NewBriefDraftsWithPolicy with a nil dependency")
	}
	return &BriefDrafts{repo: repo, evidence: evidence, provider: provider, policy: policy, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (b *BriefDrafts) Generate(ctx context.Context, workspace id.ID, observations []id.ID, actor id.ID) (domain.BriefDraft, error) {
	if workspace.IsZero() || actor.IsZero() {
		return domain.BriefDraft{}, domain.ErrIDRequired
	}
	if len(observations) == 0 {
		return domain.BriefDraft{}, domain.ErrBriefDraftRequired
	}
	input, err := b.evidence.LoadBriefDraftInput(ctx, workspace, observations)
	if err != nil {
		return domain.BriefDraft{}, err
	}
	timer := startProviderRun(input)
	if providerUsesExternal(b.provider) {
		allowed, err := b.policy.Current(ctx, workspace)
		if err != nil {
			return domain.BriefDraft{}, err
		}
		if !allowed.AllowExternal {
			failed, buildErr := domain.NewBriefDraftResult(b.ids.NewID(), workspace, actor, input.Brief, b.provider.Name(), b.provider.Method(), b.provider.TemplateVersion(), domain.BriefDraftUnsupported, "", nil, domain.ErrExternalProviderDisabled.Error(), b.clock.Now())
			if buildErr != nil {
				return domain.BriefDraft{}, buildErr
			}
			run, runErr := timer.finish(failed.ID, workspace, actor, "brief_draft", failed.Provider, failed.Method, failed.TemplateVersion, failed.Status.String(), failed.Output, failed.Error, false, failed.CreatedAt)
			if runErr != nil {
				return domain.BriefDraft{}, runErr
			}
			if persistErr := b.persist(ctx, failed, run, actor); persistErr != nil {
				return domain.BriefDraft{}, persistErr
			}
			return failed, domain.ErrExternalProviderDisabled
		}
	}
	status, output, changes, err := b.provider.DraftBrief(ctx, input)
	if err != nil {
		status := domain.BriefDraftFailed
		if pkgerrors.KindOf(err) == pkgerrors.Unavailable {
			status = domain.BriefDraftUnsupported
		} else if pkgerrors.KindOf(err) == pkgerrors.Timeout {
			status = domain.BriefDraftTimedOut
		}
		failed, buildErr := domain.NewBriefDraftResult(b.ids.NewID(), workspace, actor, input.Brief, b.provider.Name(), b.provider.Method(), b.provider.TemplateVersion(), status, "", nil, trimBriefDraftError(err.Error()), b.clock.Now())
		if buildErr != nil {
			return domain.BriefDraft{}, buildErr
		}
		run, runErr := timer.finish(failed.ID, workspace, actor, "brief_draft", failed.Provider, failed.Method, failed.TemplateVersion, failed.Status.String(), failed.Output, failed.Error, status == domain.BriefDraftTimedOut, failed.CreatedAt)
		if runErr != nil {
			return domain.BriefDraft{}, runErr
		}
		if persistErr := b.persist(ctx, failed, run, actor); persistErr != nil {
			return domain.BriefDraft{}, persistErr
		}
		return failed, err
	}
	fresh, err := domain.NewBriefDraft(b.ids.NewID(), workspace, actor, input.Brief, b.provider.Name(), b.provider.Method(), b.provider.TemplateVersion(), status, output, changes, b.clock.Now())
	if err != nil {
		return domain.BriefDraft{}, err
	}
	run, err := timer.finish(fresh.ID, workspace, actor, "brief_draft", fresh.Provider, fresh.Method, fresh.TemplateVersion, fresh.Status.String(), fresh.Output, fresh.Error, false, fresh.CreatedAt)
	if err != nil {
		return domain.BriefDraft{}, err
	}
	if err := b.persist(ctx, fresh, run, actor); err != nil {
		return domain.BriefDraft{}, err
	}
	return fresh, nil
}

func (b *BriefDrafts) persist(ctx context.Context, fresh domain.BriefDraft, run domain.ProviderRun, actor id.ID) error {
	if err := b.tx.InTx(ctx, func(ctx context.Context) error {
		if err := b.repo.CreateBriefDraft(ctx, fresh); err != nil {
			return err
		}
		if err := b.repo.CreateProviderRun(ctx, run); err != nil {
			return err
		}
		prov, ok := provenance.Current(ctx)
		if !ok {
			prov = provenance.New(provenance.OriginRequest, b.ids)
		}
		prov, err := prov.WithTenant(fresh.WorkspaceID.String())
		if err != nil {
			return err
		}
		event, err := events.NewDecision(b.ids, b.clock, domain.EventBriefDraftGenerated, "workspace:"+fresh.WorkspaceID.String(), prov, map[string]any{
			"workspace_id": fresh.WorkspaceID.String(), "brief_draft_id": fresh.ID.String(), "brief_id": fresh.Input.BriefID.String(), "observation_count": len(fresh.Input.ObservationIDs), "change_count": len(fresh.Changes), "provider": fresh.Provider, "method": fresh.Method, "template_version": fresh.TemplateVersion, "status": fresh.Status.String(), "error": fresh.Error, "actor": actor.String(),
		})
		if err != nil {
			return err
		}
		return b.publisher.Publish(ctx, event)
	}); err != nil {
		return err
	}
	return nil
}

func trimBriefDraftError(raw string) string {
	failure := strings.TrimSpace(raw)
	if len(failure) <= domain.MaxBriefDraftError {
		return failure
	}
	failure = failure[:domain.MaxBriefDraftError]
	for !utf8.ValidString(failure) {
		failure = failure[:len(failure)-1]
	}
	return failure
}
