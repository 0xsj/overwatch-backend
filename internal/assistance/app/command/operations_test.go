package command_test

import (
	"context"
	"errors"
	"testing"
	"time"

	assistapp "github.com/0xsj/overwatch-backend/internal/assistance/app"
	"github.com/0xsj/overwatch-backend/internal/assistance/app/command"
	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type operationStore struct {
	operation domain.Operation
	proposal  domain.Proposal
}

func (s *operationStore) CreateOperation(_ context.Context, in domain.Operation) error {
	s.operation = in
	return nil
}
func (s *operationStore) ByOperation(_ context.Context, _, _ id.ID) (domain.Operation, error) {
	return s.operation, nil
}
func (s *operationStore) CreateProposal(_ context.Context, in domain.Proposal) error {
	s.proposal = in
	return nil
}
func (s *operationStore) ByProposal(_ context.Context, _, _ id.ID) (domain.Proposal, error) {
	return s.proposal, nil
}
func (s *operationStore) SaveProposal(_ context.Context, in domain.Proposal) error {
	s.proposal = in
	return nil
}
func (s *operationStore) CreateReview(context.Context, domain.Review) error { return nil }
func (s *operationStore) InTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (s *operationStore) Publish(context.Context, ...events.Event) error { return nil }

type extractedCapture struct{ value command.RetainedCapture }

func (c extractedCapture) Retained(context.Context, id.ID, id.ID, id.ID, id.ID) (command.RetainedCapture, error) {
	return c.value, nil
}

type providerPolicy struct{ allowExternal bool }

func (p providerPolicy) Current(_ context.Context, workspace id.ID) (domain.ProviderPolicy, error) {
	return domain.ProviderPolicy{WorkspaceID: workspace, AllowExternal: p.allowExternal}, nil
}

func TestDerivedAssistanceCarriesExtractionThroughGenerationAndReview(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	ids := id.NewSequence(at)
	workspace, source, capture, extraction, actor := ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID()
	repo := &operationStore{}
	service := command.NewOperations(repo, extractedCapture{command.RetainedCapture{
		WorkspaceID: workspace, SourceID: source, CaptureID: capture, ExtractionID: extraction,
		MediaType: "text/plain", Content: "The derived PDF names East Quay. A second check is needed.",
	}}, assistapp.LocalSentenceProvider{}, providerPolicy{}, repo, repo, ids, clock.NewFake(at))

	operation, proposals, err := service.Generate(context.Background(), workspace, source, capture, extraction, actor)
	if err != nil || operation.ExtractionID == nil || *operation.ExtractionID != extraction || operation.TemplateVersion != "sentence-passages-v1" || operation.InputBytes == 0 || operation.OutputBytes == 0 || len(proposals) != 2 || proposals[0].ExtractionID == nil || *proposals[0].ExtractionID != extraction {
		t.Fatalf("generated derived assistance: operation=%+v proposals=%+v err=%v", operation, proposals, err)
	}

	accepted, err := service.Review(context.Background(), workspace, proposals[0].ID, actor, domain.DecisionAccept, "The derived PDF names East Quay.", proposals[0].GeneratedQuote, &proposals[0].GeneratedQuoteStart, "")
	if err != nil || accepted.State != domain.ProposalAccepted || accepted.ExtractionID == nil || *accepted.ExtractionID != extraction {
		t.Fatalf("reviewed derived assistance: %+v err=%v", accepted, err)
	}
}

type failedProvider struct{}

func (failedProvider) Name() string   { return "external-process" }
func (failedProvider) Method() string { return "json-passage-proposals-v1" }
func (failedProvider) External() bool { return true }
func (failedProvider) Extract(context.Context, assistapp.Input) ([]domain.ProposalDraft, error) {
	return nil, assistapp.ErrProcessProviderUnavailable
}

func TestFailedAssistanceIsPersistedAndCanBeRetried(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	ids := id.NewSequence(at)
	workspace, source, capture, actor := ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID()
	repo := &operationStore{}
	service := command.NewOperations(repo, extractedCapture{command.RetainedCapture{
		WorkspaceID: workspace, SourceID: source, CaptureID: capture, MediaType: "text/plain", Content: "A retained passage.",
	}}, failedProvider{}, providerPolicy{allowExternal: true}, repo, repo, ids, clock.NewFake(at))

	failed, proposals, err := service.Generate(context.Background(), workspace, source, capture, id.Nil, actor)
	if err == nil || failed.Status != domain.OperationUnsupported || failed.Error == "" || len(proposals) != 0 {
		t.Fatalf("provider failure was not persisted as unsupported: operation=%+v proposals=%v err=%v", failed, proposals, err)
	}
	retried, _, retryErr := service.Retry(context.Background(), workspace, source, capture, failed.ID, actor)
	if retryErr == nil || retried.RetryOf == nil || *retried.RetryOf != failed.ID || retried.Status != domain.OperationUnsupported {
		t.Fatalf("retry did not preserve failure lineage: operation=%+v err=%v", retried, retryErr)
	}
}

func TestExternalAssistanceIsBlockedBeforeRetainedMaterialIsRead(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	ids := id.NewSequence(at)
	workspace, source, capture, actor := ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID()
	repo := &operationStore{}
	service := command.NewOperations(repo, extractedCapture{command.RetainedCapture{
		WorkspaceID: workspace, SourceID: source, CaptureID: capture, MediaType: "text/plain", Content: "This retained passage must remain inside the service.",
	}}, failedProvider{}, providerPolicy{}, repo, repo, ids, clock.NewFake(at))

	operation, proposals, err := service.Generate(context.Background(), workspace, source, capture, id.Nil, actor)
	if !errors.Is(err, domain.ErrExternalProviderDisabled) || operation.Status != domain.OperationUnsupported || operation.Error == "" || len(proposals) != 0 {
		t.Fatalf("external provider was not blocked by the default policy: operation=%+v proposals=%v err=%v", operation, proposals, err)
	}
}
