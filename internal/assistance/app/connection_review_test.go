package app

import (
	"context"
	"testing"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestLocalConnectionReviewProviderKeepsFindingsCitationGrounded(t *testing.T) {
	left := id.ID{1}
	right := id.ID{2}
	out, err := (LocalConnectionReviewProvider{}).ReviewConnection(context.Background(), ConnectionReviewInput{
		ConnectionKind:           "may_belong_to",
		ConnectionState:          "proposed",
		ConnectionRationale:      "The same handle appears in both records, but control is not established.",
		SupportingObservationIDs: []id.ID{left},
		OpposingObservationIDs:   []id.ID{right},
		Decisions:                []ComparisonDecision{{LeftObservationID: left, RightObservationID: right, Kind: "contradicts", Rationale: "The source contexts differ."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != domain.ConnectionReviewCompleted || out.Text == "" || len(out.Findings) < 4 {
		t.Fatalf("connection review output: %+v", out)
	}
	seenKinds := map[domain.ConnectionReviewFindingKind]bool{}
	for _, finding := range out.Findings {
		seenKinds[finding.Kind] = true
		if len(finding.ObservationIDs) == 0 {
			t.Fatalf("finding lost citations: %+v", finding)
		}
		for _, observation := range finding.ObservationIDs {
			if observation != left && observation != right {
				t.Fatalf("finding cited an unrelated observation: %+v", finding)
			}
		}
	}
	for _, kind := range []domain.ConnectionReviewFindingKind{domain.ConnectionSupport, domain.ConnectionOpposition, domain.ConnectionAlternative, domain.ConnectionDiscriminatingEvidence} {
		if !seenKinds[kind] {
			t.Fatalf("missing %s finding: %+v", kind, out.Findings)
		}
	}
}
