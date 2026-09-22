package domain

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestNewComparisonResultRetainsProviderFailureWithoutOutput(t *testing.T) {
	found, err := NewComparisonResult(id.ID{1}, id.ID{2}, id.ID{3}, []id.ID{id.ID{4}, id.ID{5}}, "external-process", "comparison-v1", "comparison-v1", ComparisonUnsupported, "", nil, "provider is not configured", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if found.Status != ComparisonUnsupported || found.Error == "" || found.Output != "" || len(found.Findings) != 0 {
		t.Fatalf("unexpected failed comparison: %+v", found)
	}
	if _, err := NewComparisonResult(id.ID{1}, id.ID{2}, id.ID{3}, []id.ID{id.ID{4}, id.ID{5}}, "local", "comparison-v1", "comparison-v1", ComparisonFailed, "output", nil, "provider failed", time.Now()); err == nil {
		t.Fatal("failed comparison should not retain output")
	}
}
