package app

import (
	"context"
	"testing"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestLocalComparisonProviderKeepsFindingsCitationGrounded(t *testing.T) {
	left := id.ID{1}
	right := id.ID{2}
	out, err := (LocalComparisonProvider{}).Compare(context.Background(), ComparisonInput{
		Observations: []Observation{
			{ID: left, SourceTitle: "First notice", Statement: "East Quay was closed at 18:00.", Quote: "East Quay was closed."},
			{ID: right, SourceTitle: "Second notice", Statement: "East Quay was open at 18:00.", Quote: "East Quay was open."},
		},
		Decisions: []ComparisonDecision{{LeftObservationID: left, RightObservationID: right, Kind: "contradicts", Rationale: "The opening status differs."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != domain.ComparisonCompleted || out.Text == "" || len(out.Findings) == 0 {
		t.Fatalf("comparison output: %+v", out)
	}
	foundContradiction := false
	for _, finding := range out.Findings {
		if finding.Kind == domain.Contradiction {
			foundContradiction = true
		}
		if len(finding.ObservationIDs) == 0 {
			t.Fatalf("finding lost citations: %+v", finding)
		}
		for _, observation := range finding.ObservationIDs {
			if observation != left && observation != right {
				t.Fatalf("finding cited an unselected observation: %+v", finding)
			}
		}
	}
	if !foundContradiction {
		t.Fatalf("human contradiction was not surfaced: %+v", out.Findings)
	}
}

func TestLocalComparisonProviderLeavesUnreviewedPairsAsCoverageGaps(t *testing.T) {
	out, err := (LocalComparisonProvider{}).Compare(context.Background(), ComparisonInput{Observations: []Observation{{ID: id.ID{1}, Statement: "A notice names East Quay."}, {ID: id.ID{2}, Statement: "Another notice names Harbor Line."}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range out.Findings {
		if finding.Kind == domain.CoverageGap {
			return
		}
	}
	t.Fatalf("unreviewed pair did not produce a coverage gap: %+v", out)
}
