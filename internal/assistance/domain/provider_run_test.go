package domain

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestNewProviderRunKeepsOperationalMeasurementsSeparateFromPayloads(t *testing.T) {
	at := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	found, err := NewProviderRun(id.ID{1}, id.ID{2}, id.ID{3}, id.ID{4}, "comparison", "external-process", "comparison-v1", "comparison-v1", "completed", 1200, 450, 37, false, "", at, at.Add(37*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if found.InputBytes != 1200 || found.OutputBytes != 450 || found.DurationMS != 37 || found.Error != "" {
		t.Fatalf("unexpected provider measurements: %+v", found)
	}
}

func TestNewProviderRunRejectsNegativeMeasurements(t *testing.T) {
	at := time.Now()
	if _, err := NewProviderRun(id.ID{1}, id.ID{2}, id.ID{3}, id.ID{4}, "comparison", "local", "comparison-v1", "comparison-v1", "completed", -1, 0, 0, false, "", at, at); err == nil {
		t.Fatal("negative input bytes should be rejected")
	}
}
