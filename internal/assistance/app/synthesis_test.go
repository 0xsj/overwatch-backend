package app

import (
	"context"
	"testing"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestLocalSynthesisProviderIsDeterministicAndCitationBound(t *testing.T) {
	first, second := id.ID{1}, id.ID{2}
	provider := LocalSynthesisProvider{}
	input := []Observation{
		{ID: first, SourceTitle: "Notice A", Statement: "The account @HarborLine posted.", Quote: "@HarborLine"},
		{ID: second, SourceTitle: "Notice B", Statement: "Contact user@example.com.", Quote: "user@example.com"},
	}
	found, err := provider.Synthesize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if found.Text != "Notice A: The account @HarborLine posted.\nNotice B: Contact user@example.com." {
		t.Fatalf("output: %q", found.Text)
	}
	if len(found.Candidates) != 2 || found.Candidates[0].Kind != "account" || len(found.Candidates[0].ObservationIDs) != 1 {
		t.Fatalf("candidates: %+v", found.Candidates)
	}
	if found.Candidates[0].ObservationIDs[0] != first && found.Candidates[1].ObservationIDs[0] != first {
		t.Fatalf("candidate lost its supporting citation: %+v", found.Candidates)
	}
}
