package domain

import (
	"testing"
	"time"

	connectiondomain "github.com/0xsj/overwatch-backend/internal/researchconnection/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestNewConnectionReviewResultRetainsProviderFailureWithoutOutput(t *testing.T) {
	found, err := NewConnectionReviewResult(id.ID{1}, id.ID{2}, id.ID{3}, id.ID{4}, id.ID{5}, id.ID{6}, connectiondomain.AssociatedWith.String(), connectiondomain.Proposed.String(), "review the authored relationship", []id.ID{{7}}, nil, "external-process", "connection-review-v1", "connection-review-v1", ConnectionReviewUnsupported, "", nil, "provider is not configured", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if found.Status != ConnectionReviewUnsupported || found.Error == "" || found.Output != "" || len(found.Findings) != 0 {
		t.Fatalf("unexpected failed connection review: %+v", found)
	}
	if _, err := NewConnectionReviewResult(id.ID{1}, id.ID{2}, id.ID{3}, id.ID{4}, id.ID{5}, id.ID{6}, connectiondomain.AssociatedWith.String(), connectiondomain.Proposed.String(), "review the authored relationship", []id.ID{{7}}, nil, "local", "connection-review-v1", "connection-review-v1", ConnectionReviewFailed, "output", nil, "provider failed", time.Now()); err == nil {
		t.Fatal("failed connection review should not retain output")
	}
}
