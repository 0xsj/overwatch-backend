package query_test

import (
	"context"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/assistance/app/query"
	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type reader struct {
	operation  domain.Operation
	operations []domain.Operation
	proposals  []domain.Proposal
	err        error
}

func (r reader) ByOperation(context.Context, id.ID, id.ID) (domain.Operation, error) {
	return r.operation, r.err
}

func (r reader) ByCapture(context.Context, id.ID, id.ID, id.ID, id.ID, int) ([]domain.Operation, error) {
	return r.operations, r.err
}

func (r reader) LatestByCapture(context.Context, id.ID, id.ID, id.ID, id.ID) (domain.Operation, error) {
	return r.operation, r.err
}

func (r reader) Proposals(context.Context, id.ID, id.ID) ([]domain.Proposal, error) {
	return r.proposals, r.err
}

func testID(t *testing.T, raw string) id.ID {
	t.Helper()
	found, err := id.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func TestLatestReturnsEmptyWhenCaptureHasNoOperation(t *testing.T) {
	space := testID(t, "018f3f4e-8b91-7000-8000-000000000001")
	source := testID(t, "018f3f4e-8b91-7000-8000-000000000002")
	capture := testID(t, "018f3f4e-8b91-7000-8000-000000000003")
	service := query.NewOperations(reader{err: domain.ErrNotFound})

	found, err := service.Latest(context.Background(), space, source, capture, id.Nil)
	if err != nil {
		t.Fatalf("latest without an operation: %v", err)
	}
	if found.Operation != nil || found.Proposals == nil || len(found.Proposals) != 0 {
		t.Fatalf("unexpected empty latest result: %+v", found)
	}
}

func TestLatestReturnsOperationAndProposals(t *testing.T) {
	space := testID(t, "018f3f4e-8b91-7000-8000-000000000011")
	source := testID(t, "018f3f4e-8b91-7000-8000-000000000012")
	capture := testID(t, "018f3f4e-8b91-7000-8000-000000000013")
	operationID := testID(t, "018f3f4e-8b91-7000-8000-000000000014")
	proposalID := testID(t, "018f3f4e-8b91-7000-8000-000000000015")
	operation := domain.Operation{ID: operationID, WorkspaceID: space, SourceID: source, CaptureID: capture, Status: domain.OperationCompleted, Provider: "local", Method: "sentence-passages-v1", CreatedBy: space, CreatedAt: time.Unix(10, 0).UTC(), CompletedAt: time.Unix(11, 0).UTC(), ProposalCount: 1}
	proposal := domain.Proposal{ID: proposalID, OperationID: operationID, WorkspaceID: space, SourceID: source, CaptureID: capture, GeneratedStatement: "A retained statement.", GeneratedQuote: "A retained statement.", State: domain.ProposalProposed}
	service := query.NewOperations(reader{operation: operation, proposals: []domain.Proposal{proposal}})

	found, err := service.Latest(context.Background(), space, source, capture, id.Nil)
	if err != nil {
		t.Fatalf("latest operation: %v", err)
	}
	if found.Operation == nil || found.Operation.ID != operationID || len(found.Proposals) != 1 || found.Proposals[0].ID != proposalID {
		t.Fatalf("unexpected latest result: %+v", found)
	}
}

func TestHistoryReturnsEachOperationWithItsProposals(t *testing.T) {
	space := testID(t, "018f3f4e-8b91-7000-8000-000000000021")
	source := testID(t, "018f3f4e-8b91-7000-8000-000000000022")
	capture := testID(t, "018f3f4e-8b91-7000-8000-000000000023")
	firstID := testID(t, "018f3f4e-8b91-7000-8000-000000000024")
	secondID := testID(t, "018f3f4e-8b91-7000-8000-000000000025")
	first := domain.Operation{ID: firstID, WorkspaceID: space, SourceID: source, CaptureID: capture, Status: domain.OperationCompleted}
	second := domain.Operation{ID: secondID, WorkspaceID: space, SourceID: source, CaptureID: capture, Status: domain.OperationCompleted}
	proposal := domain.Proposal{ID: testID(t, "018f3f4e-8b91-7000-8000-000000000026"), OperationID: firstID, WorkspaceID: space, SourceID: source, CaptureID: capture, State: domain.ProposalAccepted}
	service := query.NewOperations(reader{operations: []domain.Operation{second, first}, proposals: []domain.Proposal{proposal}})

	found, err := service.History(context.Background(), space, source, capture, id.Nil, 10)
	if err != nil {
		t.Fatalf("assistance history: %v", err)
	}
	if len(found.Items) != 2 || found.Items[0].Operation.ID != secondID || found.Items[1].Operation.ID != firstID || len(found.Items[1].Proposals) != 1 {
		t.Fatalf("unexpected assistance history: %+v", found)
	}
}
