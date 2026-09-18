package command

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/assistance/app"
	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type Repository interface {
	CreateOperation(context.Context, domain.Operation) error
	CreateProposal(context.Context, domain.Proposal) error
	ByProposal(context.Context, id.ID, id.ID) (domain.Proposal, error)
	SaveProposal(context.Context, domain.Proposal) error
	CreateReview(context.Context, domain.Review) error
}

type Transactor interface {
	InTx(context.Context, func(context.Context) error) error
}
type Minter interface{ NewID() id.ID }
type Clock interface{ Now() time.Time }

// Captures is the small composition port into retained source material.
type Captures interface {
	Retained(context.Context, id.ID, id.ID, id.ID, id.ID) (RetainedCapture, error)
}

type RetainedCapture struct {
	WorkspaceID  id.ID
	SourceID     id.ID
	CaptureID    id.ID
	ExtractionID id.ID
	MediaType    string
	Content      string
}

type Operations struct {
	repo      Repository
	captures  Captures
	provider  app.Provider
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewOperations(repo Repository, captures Captures, provider app.Provider, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Operations {
	if repo == nil || captures == nil || provider == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("assistance: NewOperations with a nil dependency")
	}
	return &Operations{repo: repo, captures: captures, provider: provider, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (o *Operations) Generate(ctx context.Context, workspace, source, capture, extraction, actor id.ID) (domain.Operation, []domain.Proposal, error) {
	if workspace.IsZero() || source.IsZero() || capture.IsZero() || actor.IsZero() {
		return domain.Operation{}, nil, domain.ErrIDRequired
	}
	retained, err := o.captures.Retained(ctx, workspace, source, capture, extraction)
	if err != nil {
		return domain.Operation{}, nil, err
	}
	if retained.WorkspaceID != workspace || retained.SourceID != source || retained.CaptureID != capture || retained.ExtractionID != extraction {
		return domain.Operation{}, nil, domain.ErrNotFound
	}
	if extraction.IsZero() && retained.MediaType != "text/plain" && retained.MediaType != "text/html" && retained.MediaType != "application/json" {
		return domain.Operation{}, nil, domain.ErrTextCaptureRequired
	}
	drafts, err := o.provider.Extract(ctx, app.Input{SourceID: source, CaptureID: capture, ExtractionID: extraction, Content: retained.Content})
	if err != nil {
		return domain.Operation{}, nil, err
	}
	if len(drafts) > domain.MaxProposals {
		drafts = drafts[:domain.MaxProposals]
	}
	at := o.clock.Now()
	operation, err := domain.NewOperation(o.ids.NewID(), workspace, source, capture, actor, o.provider.Name(), o.provider.Method(), len(drafts), at)
	if err != nil {
		return domain.Operation{}, nil, err
	}
	if !extraction.IsZero() {
		value := extraction
		operation.ExtractionID = &value
	}
	proposals := make([]domain.Proposal, 0, len(drafts))
	for _, draft := range drafts {
		proposal, err := domain.NewProposal(o.ids.NewID(), operation.ID, workspace, source, capture, draft, at)
		if err != nil {
			continue
		}
		if !extraction.IsZero() {
			value := extraction
			proposal.ExtractionID = &value
		}
		proposals = append(proposals, proposal)
	}
	operation.ProposalCount = len(proposals)
	if err := o.tx.InTx(ctx, func(ctx context.Context) error {
		if err := o.repo.CreateOperation(ctx, operation); err != nil {
			return err
		}
		for _, proposal := range proposals {
			if err := o.repo.CreateProposal(ctx, proposal); err != nil {
				return err
			}
		}
		payload := map[string]any{
			"workspace_id": workspace.String(), "source_id": source.String(), "capture_id": capture.String(),
			"operation_id": operation.ID.String(), "provider": operation.Provider, "method": operation.Method,
			"proposal_count": operation.ProposalCount, "actor": actor.String(),
		}
		if !extraction.IsZero() {
			payload["extraction_id"] = extraction.String()
		}
		return o.publish(ctx, domain.EventGenerated, workspace, payload)
	}); err != nil {
		return domain.Operation{}, nil, err
	}
	return operation, proposals, nil
}

func (o *Operations) Review(ctx context.Context, workspace, proposalID, reviewer id.ID, decision domain.ReviewDecision, statement, quote string, start *int, note string) (domain.Proposal, error) {
	if workspace.IsZero() || proposalID.IsZero() || reviewer.IsZero() {
		return domain.Proposal{}, domain.ErrIDRequired
	}
	proposal, err := o.repo.ByProposal(ctx, workspace, proposalID)
	if err != nil {
		return domain.Proposal{}, err
	}
	extraction := id.ID{}
	if proposal.ExtractionID != nil {
		extraction = *proposal.ExtractionID
	}
	retained, err := o.captures.Retained(ctx, workspace, proposal.SourceID, proposal.CaptureID, extraction)
	if err != nil {
		return domain.Proposal{}, err
	}
	if retained.WorkspaceID != workspace || retained.SourceID != proposal.SourceID || retained.CaptureID != proposal.CaptureID || retained.ExtractionID != extraction {
		return domain.Proposal{}, domain.ErrNotFound
	}
	next, err := proposal.Review(reviewer, decision, statement, quote, start, note, retained.Content, o.clock.Now())
	if err != nil {
		return domain.Proposal{}, err
	}
	review := domain.NewReview(o.ids.NewID(), next, reviewer, decision, statement, quote, start, note, next.ReviewedAtValue())
	if err := o.tx.InTx(ctx, func(ctx context.Context) error {
		if err := o.repo.SaveProposal(ctx, next); err != nil {
			return err
		}
		if err := o.repo.CreateReview(ctx, review); err != nil {
			return err
		}
		payload := map[string]any{
			"workspace_id": workspace.String(), "operation_id": next.OperationID.String(), "proposal_id": next.ID.String(),
			"decision": string(decision), "reviewer": reviewer.String(),
		}
		if next.ExtractionID != nil {
			payload["extraction_id"] = next.ExtractionID.String()
		}
		return o.publish(ctx, domain.EventReviewed, workspace, payload)
	}); err != nil {
		return domain.Proposal{}, err
	}
	return next, nil
}

func (o *Operations) publish(ctx context.Context, name string, workspace id.ID, payload any) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, o.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	event, err := events.NewDecision(o.ids, o.clock, name, "workspace:"+workspace.String(), prov, payload)
	if err != nil {
		return err
	}
	return o.publisher.Publish(ctx, event)
}
