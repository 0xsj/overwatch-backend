package command

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/internal/assistance/app"
	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type Repository interface {
	CreateOperation(context.Context, domain.Operation) error
	ByOperation(context.Context, id.ID, id.ID) (domain.Operation, error)
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

type ProviderPolicy interface {
	Current(context.Context, id.ID) (domain.ProviderPolicy, error)
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
	policy    ProviderPolicy
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewOperations(repo Repository, captures Captures, provider app.Provider, policy ProviderPolicy, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Operations {
	if repo == nil || captures == nil || provider == nil || policy == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("assistance: NewOperations with a nil dependency")
	}
	return &Operations{repo: repo, captures: captures, provider: provider, policy: policy, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (o *Operations) Generate(ctx context.Context, workspace, source, capture, extraction, actor id.ID) (domain.Operation, []domain.Proposal, error) {
	return o.generate(ctx, workspace, source, capture, extraction, actor, nil)
}

func (o *Operations) Retry(ctx context.Context, workspace, source, capture, operation, actor id.ID) (domain.Operation, []domain.Proposal, error) {
	if workspace.IsZero() || source.IsZero() || capture.IsZero() || operation.IsZero() || actor.IsZero() {
		return domain.Operation{}, nil, domain.ErrIDRequired
	}
	previous, err := o.repo.ByOperation(ctx, workspace, operation)
	if err != nil {
		return domain.Operation{}, nil, err
	}
	if previous.SourceID != source || previous.CaptureID != capture {
		return domain.Operation{}, nil, domain.ErrNotFound
	}
	if previous.Status != domain.OperationEmpty && previous.Status != domain.OperationFailed && previous.Status != domain.OperationUnsupported && previous.Status != domain.OperationPartial {
		return domain.Operation{}, nil, domain.ErrRetryUnavailable
	}
	extraction := id.Nil
	if previous.ExtractionID != nil {
		extraction = *previous.ExtractionID
	}
	return o.generate(ctx, workspace, previous.SourceID, previous.CaptureID, extraction, actor, &previous.ID)
}

func (o *Operations) generate(ctx context.Context, workspace, source, capture, extraction, actor id.ID, retryOf *id.ID) (domain.Operation, []domain.Proposal, error) {
	started := time.Now()
	if workspace.IsZero() || source.IsZero() || capture.IsZero() || actor.IsZero() {
		return domain.Operation{}, nil, domain.ErrIDRequired
	}
	if providerUsesExternal(o.provider) {
		policy, err := o.policy.Current(ctx, workspace)
		if err != nil {
			return domain.Operation{}, nil, err
		}
		if !policy.AllowExternal {
			operation, persistErr := o.persistTerminal(ctx, workspace, source, capture, extraction, actor, domain.OperationUnsupported, domain.ErrExternalProviderDisabled.Error(), retryOf, 0, false, started)
			if persistErr != nil {
				return domain.Operation{}, nil, persistErr
			}
			return operation, nil, domain.ErrExternalProviderDisabled
		}
	}
	retained, err := o.captures.Retained(ctx, workspace, source, capture, extraction)
	if err != nil {
		if isUnsupported(err) {
			operation, persistErr := o.persistTerminal(ctx, workspace, source, capture, extraction, actor, domain.OperationUnsupported, err.Error(), retryOf, 0, false, started)
			if persistErr != nil {
				return domain.Operation{}, nil, persistErr
			}
			return operation, nil, err
		}
		return domain.Operation{}, nil, err
	}
	if retained.WorkspaceID != workspace || retained.SourceID != source || retained.CaptureID != capture || retained.ExtractionID != extraction {
		return domain.Operation{}, nil, domain.ErrNotFound
	}
	if extraction.IsZero() && retained.MediaType != "text/plain" && retained.MediaType != "text/html" && retained.MediaType != "application/json" {
		operation, persistErr := o.persistTerminal(ctx, workspace, source, capture, extraction, actor, domain.OperationUnsupported, domain.ErrTextCaptureRequired.Error(), retryOf, len([]byte(retained.Content)), false, started)
		if persistErr != nil {
			return domain.Operation{}, nil, persistErr
		}
		return operation, nil, domain.ErrTextCaptureRequired
	}
	drafts, err := o.provider.Extract(ctx, app.Input{SourceID: source, CaptureID: capture, ExtractionID: extraction, Content: retained.Content})
	if err != nil {
		status := domain.OperationFailed
		if stderrors.Is(err, app.ErrProcessProviderUnavailable) {
			status = domain.OperationUnsupported
		}
		operation, persistErr := o.persistTerminal(ctx, workspace, source, capture, extraction, actor, status, err.Error(), retryOf, len([]byte(retained.Content)), stderrors.Is(err, app.ErrProcessProviderTimedOut), started)
		if persistErr != nil {
			return domain.Operation{}, nil, persistErr
		}
		return operation, nil, err
	}
	if len(drafts) > domain.MaxProposals {
		drafts = drafts[:domain.MaxProposals]
	}
	output, _ := json.Marshal(drafts)
	at := o.clock.Now()
	status := domain.OperationCompleted
	failure := ""
	if len(drafts) == 0 {
		status = domain.OperationEmpty
	}
	operation, err := domain.NewOperationResult(o.ids.NewID(), workspace, source, capture, actor, o.provider.Name(), o.provider.Method(), status, failure, len(drafts), retryOf, at)
	if err != nil {
		return domain.Operation{}, nil, err
	}
	if !extraction.IsZero() {
		value := extraction
		operation.ExtractionID = &value
	}
	operation.TemplateVersion = providerTemplateVersion(o.provider)
	operation.InputBytes = int64(len([]byte(retained.Content)))
	operation.OutputBytes = int64(len(output))
	operation.DurationMS = operationDuration(started)
	proposals := make([]domain.Proposal, 0, len(drafts))
	invalidDrafts := 0
	for _, draft := range drafts {
		proposal, err := domain.NewProposal(o.ids.NewID(), operation.ID, workspace, source, capture, draft, at)
		if err != nil {
			invalidDrafts++
			continue
		}
		if !extraction.IsZero() {
			value := extraction
			proposal.ExtractionID = &value
		}
		proposals = append(proposals, proposal)
	}
	operation.ProposalCount = len(proposals)
	if invalidDrafts > 0 {
		operation.Status = domain.OperationPartial
		operation.Error = fmt.Sprintf("%d provider proposal(s) could not be retained after validation", invalidDrafts)
	}
	if len(proposals) == 0 && len(drafts) > 0 && invalidDrafts == len(drafts) {
		operation.Status = domain.OperationPartial
	}
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
			"template_version": operation.TemplateVersion, "status": operation.Status.String(), "error": operation.Error, "proposal_count": operation.ProposalCount, "input_bytes": operation.InputBytes, "output_bytes": operation.OutputBytes, "duration_ms": operation.DurationMS, "timed_out": operation.TimedOut, "actor": actor.String(),
		}
		if !extraction.IsZero() {
			payload["extraction_id"] = extraction.String()
		}
		if operation.RetryOf != nil {
			payload["retry_of"] = operation.RetryOf.String()
		}
		return o.publish(ctx, domain.EventGenerated, workspace, payload)
	}); err != nil {
		return domain.Operation{}, nil, err
	}
	return operation, proposals, nil
}

func (o *Operations) persistTerminal(ctx context.Context, workspace, source, capture, extraction, actor id.ID, status domain.OperationStatus, failure string, retryOf *id.ID, inputBytes int, timedOut bool, started time.Time) (domain.Operation, error) {
	at := o.clock.Now()
	operation, err := domain.NewOperationResult(o.ids.NewID(), workspace, source, capture, actor, o.provider.Name(), o.provider.Method(), status, trimOperationError(failure), 0, retryOf, at)
	if err != nil {
		return domain.Operation{}, err
	}
	if !extraction.IsZero() {
		value := extraction
		operation.ExtractionID = &value
	}
	operation.TemplateVersion = providerTemplateVersion(o.provider)
	operation.InputBytes = int64(inputBytes)
	operation.DurationMS = operationDuration(started)
	operation.TimedOut = timedOut
	if err := o.tx.InTx(ctx, func(ctx context.Context) error {
		if err := o.repo.CreateOperation(ctx, operation); err != nil {
			return err
		}
		return o.publish(ctx, domain.EventGenerated, workspace, map[string]any{
			"workspace_id": workspace.String(), "source_id": source.String(), "capture_id": capture.String(), "operation_id": operation.ID.String(),
			"provider": operation.Provider, "method": operation.Method, "template_version": operation.TemplateVersion, "status": operation.Status.String(), "error": operation.Error, "proposal_count": 0, "input_bytes": operation.InputBytes, "output_bytes": operation.OutputBytes, "duration_ms": operation.DurationMS, "timed_out": operation.TimedOut, "actor": actor.String(),
			"retry_of": retryOperationID(operation.RetryOf),
		})
	}); err != nil {
		return domain.Operation{}, err
	}
	return operation, nil
}

type templateVersionedProvider interface{ TemplateVersion() string }

func providerTemplateVersion(provider app.Provider) string {
	if versioned, ok := provider.(templateVersionedProvider); ok && strings.TrimSpace(versioned.TemplateVersion()) != "" {
		return strings.TrimSpace(versioned.TemplateVersion())
	}
	return provider.Method()
}

func operationDuration(started time.Time) int64 {
	if started.IsZero() {
		return 0
	}
	return time.Since(started).Milliseconds()
}

func retryOperationID(value *id.ID) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func providerUsesExternal(provider app.Provider) bool {
	external, ok := provider.(app.ExternalProvider)
	return !ok || external.External()
}

func isUnsupported(err error) bool {
	return stderrors.Is(err, domain.ErrUnsupported) || stderrors.Is(err, domain.ErrTextCaptureRequired) || stderrors.Is(err, app.ErrProcessProviderUnavailable)
}

func trimOperationError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= domain.MaxOperationError {
		return value
	}
	return value[:domain.MaxOperationError-3] + "..."
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
