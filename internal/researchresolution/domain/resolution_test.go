package domain

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

func resolutionID(n byte) id.ID { var out id.ID; out[0] = n; return out }

func TestResolutionAcceptsAndReversesAnExplicitAliasDecision(t *testing.T) {
	at := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	one, err := New(resolutionID(1), resolutionID(2), resolutionID(3), resolutionID(4), resolutionID(5), "The reviewed records describe the same subject.", at)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := one.Accept(resolutionID(6), []id.ID{resolutionID(8)}, []id.ID{resolutionID(7), resolutionID(8)}, at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if accepted.State != Accepted || len(accepted.CanonicalObservationIDsBefore) != 1 || len(accepted.AddedObservationIDs) != 2 || len(accepted.CanonicalObservationIDsAfter()) != 2 {
		t.Fatalf("accepted: %+v", accepted)
	}
	reversed, err := accepted.Reverse(resolutionID(9), at.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if reversed.State != Reversed || reversed.ReversedBy != resolutionID(9) {
		t.Fatalf("reversed: %+v", reversed)
	}
}

func TestResolutionDoesNotTreatAProposalAsAnIdentityClaim(t *testing.T) {
	at := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	one, err := New(resolutionID(11), resolutionID(12), resolutionID(13), resolutionID(14), resolutionID(15), "Needs human confirmation.", at)
	if err != nil {
		t.Fatal(err)
	}
	if one.State != Proposed || len(one.CanonicalObservationIDsAfter()) != 0 {
		t.Fatalf("proposal: %+v", one)
	}
	if _, err := one.Accept(resolutionID(16), make([]id.ID, 0), make([]id.ID, 13), at); err != ErrObservationTooMany {
		t.Fatalf("too many observations: %v", err)
	}
}
