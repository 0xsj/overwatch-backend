package domain

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestNewSynthesisKeepsCandidatesInsideSelectedEvidence(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	want, workspace, actor, first, second := id.ID{1}, id.ID{2}, id.ID{3}, id.ID{4}, id.ID{5}
	candidate := SynthesisCandidate{Kind: "account", Name: "@harborline", ObservationIDs: []id.ID{first}, Rationale: "Verify this exact identifier."}
	found, err := NewSynthesis(want, workspace, actor, []id.ID{first, second}, "local", "selected-observations-v1", "Notice: @harborline", []SynthesisCandidate{candidate}, at)
	if err != nil || found.ID != want || found.Candidates[0].ObservationIDs[0] != first {
		t.Fatalf("valid synthesis: %+v, %v", found, err)
	}
	if _, err := NewSynthesis(want, workspace, actor, []id.ID{first, first}, "local", "method", "text", nil, at); err == nil {
		t.Fatal("duplicate observations should be rejected")
	}
	outside := SynthesisCandidate{Kind: "account", Name: "@outside", ObservationIDs: []id.ID{id.ID{9}}, Rationale: "not selected"}
	if _, err := NewSynthesis(want, workspace, actor, []id.ID{first}, "local", "method", "text", []SynthesisCandidate{outside}, at); err == nil {
		t.Fatal("candidate outside the selected observations should be rejected")
	}
}

func TestNewSynthesisBoundsSelectedObservations(t *testing.T) {
	observations := make([]id.ID, MaxSynthesisObservations+1)
	for index := range observations {
		observations[index][0] = byte(index + 1)
	}
	if _, err := NewSynthesis(id.ID{1}, id.ID{2}, id.ID{3}, observations, "local", "method", "text", nil, time.Now()); err != ErrSynthesisTooLarge {
		t.Fatalf("expected size error, got %v", err)
	}
}

func TestNewSynthesisResultRetainsProviderFailureWithoutOutput(t *testing.T) {
	found, err := NewSynthesisResult(id.ID{1}, id.ID{2}, id.ID{3}, []id.ID{id.ID{4}}, "external-process", "json-selected-observations-v1", SynthesisUnsupported, "", nil, "provider is not configured", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if found.Status != SynthesisUnsupported || found.Error == "" || found.Output != "" || len(found.Candidates) != 0 {
		t.Fatalf("unexpected failed synthesis: %+v", found)
	}
	if _, err := NewSynthesisResult(id.ID{1}, id.ID{2}, id.ID{3}, []id.ID{id.ID{4}}, "local", "method", SynthesisFailed, "output", nil, "provider failed", time.Now()); err == nil {
		t.Fatal("failed synthesis should not retain output")
	}
}
