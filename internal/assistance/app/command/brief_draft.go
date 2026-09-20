package command

import (
	"context"

	assistapp "github.com/0xsj/overwatch-backend/internal/assistance/app"
	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type BriefDraftRepository interface {
	CreateBriefDraft(context.Context, domain.BriefDraft) error
}

type BriefDraftEvidence interface {
	LoadBriefDraftInput(context.Context, id.ID, []id.ID) (assistapp.BriefDraftInput, error)
}

type BriefDrafts struct {
	repo      BriefDraftRepository
	evidence  BriefDraftEvidence
	provider  assistapp.BriefDraftProvider
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewBriefDrafts(repo BriefDraftRepository, evidence BriefDraftEvidence, provider assistapp.BriefDraftProvider, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *BriefDrafts {
	if repo == nil || evidence == nil || provider == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("assistance: NewBriefDrafts with a nil dependency")
	}
	return &BriefDrafts{repo: repo, evidence: evidence, provider: provider, tx: tx, publisher: publisher, ids: ids, clock: clock}
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
	status, output, changes, err := b.provider.DraftBrief(ctx, input)
	if err != nil {
		return domain.BriefDraft{}, err
	}
	fresh, err := domain.NewBriefDraft(b.ids.NewID(), workspace, actor, input.Brief, b.provider.Name(), b.provider.Method(), b.provider.TemplateVersion(), status, output, changes, b.clock.Now())
	if err != nil {
		return domain.BriefDraft{}, err
	}
	if err := b.tx.InTx(ctx, func(ctx context.Context) error {
		if err := b.repo.CreateBriefDraft(ctx, fresh); err != nil {
			return err
		}
		prov, ok := provenance.Current(ctx)
		if !ok {
			prov = provenance.New(provenance.OriginRequest, b.ids)
		}
		prov, err := prov.WithTenant(workspace.String())
		if err != nil {
			return err
		}
		event, err := events.NewDecision(b.ids, b.clock, domain.EventBriefDraftGenerated, "workspace:"+workspace.String(), prov, map[string]any{
			"workspace_id": workspace.String(), "brief_draft_id": fresh.ID.String(), "brief_id": fresh.Input.BriefID.String(), "observation_count": len(fresh.Input.ObservationIDs), "change_count": len(fresh.Changes), "provider": fresh.Provider, "method": fresh.Method, "template_version": fresh.TemplateVersion, "status": fresh.Status.String(), "actor": actor.String(),
		})
		if err != nil {
			return err
		}
		return b.publisher.Publish(ctx, event)
	}); err != nil {
		return domain.BriefDraft{}, err
	}
	return fresh, nil
}
