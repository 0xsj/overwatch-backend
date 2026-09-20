package domain

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestResolutionSetAcceptKeepsAliasesAndUnionsObservations(t *testing.T) {
	at := time.Unix(10, 0).UTC()
	set, err := NewSet(resolutionID(1), resolutionID(2), resolutionID(3), resolutionID(4), []id.ID{resolutionID(6), resolutionID(5)}, "same subject", at)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.AliasRecordIDs) != 2 || set.AliasRecordIDs[0] != resolutionID(5) {
		t.Fatalf("aliases were not normalized: %+v", set.AliasRecordIDs)
	}
	accepted, err := set.Accept(resolutionID(7), []id.ID{resolutionID(8)}, []id.ID{resolutionID(9), resolutionID(10)}, at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if accepted.State != Accepted || len(accepted.CanonicalObservationIDsAfter()) != 3 {
		t.Fatalf("accepted set: %+v", accepted)
	}
	if len(accepted.AliasRecordIDs) != 2 {
		t.Fatalf("acceptance changed aliases: %+v", accepted.AliasRecordIDs)
	}
}

func TestResolutionSetRejectsCanonicalOverlapAndOversizedSets(t *testing.T) {
	at := time.Unix(10, 0).UTC()
	if _, err := NewSet(resolutionID(1), resolutionID(2), resolutionID(3), resolutionID(4), []id.ID{resolutionID(3)}, "same subject", at); err != ErrResolutionSetOverlap {
		t.Fatalf("overlap error = %v", err)
	}
	if _, err := NewSet(resolutionID(1), resolutionID(2), resolutionID(3), resolutionID(4), []id.ID{resolutionID(5), resolutionID(6), resolutionID(7), resolutionID(8)}, "same subject", at); err != ErrResolutionSetSize {
		t.Fatalf("size error = %v", err)
	}
}
