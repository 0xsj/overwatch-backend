package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/review/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func reviewID(value byte) id.ID {
	var out id.ID
	out[15] = value
	return out
}

func TestRelationCanonicalisesAnUnorderedObservationPair(t *testing.T) {
	at := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	got, err := domain.New(reviewID(1), reviewID(2), reviewID(3), reviewID(9), reviewID(4), " supports ", "  Same event  ", at)
	if err != nil {
		t.Fatal(err)
	}
	if got.LeftObservationID != reviewID(4) || got.RightObservationID != reviewID(9) {
		t.Fatalf("pair was not canonicalised: %+v", got)
	}
	if got.Kind != domain.Supports || got.Rationale != "Same event" {
		t.Fatalf("normalisation: %+v", got)
	}
}

func TestRelationRejectsInvalidReviewDecisions(t *testing.T) {
	at := time.Now()
	for _, tc := range []struct {
		name, kind, rationale string
		a, b                  id.ID
	}{
		{"same observation", "supports", "reason", reviewID(4), reviewID(4)},
		{"unknown kind", "maybe", "reason", reviewID(4), reviewID(5)},
		{"empty rationale", "supports", " ", reviewID(4), reviewID(5)},
		{"too long", "supports", strings.Repeat("x", domain.MaxRationale+1), reviewID(4), reviewID(5)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := domain.New(reviewID(1), reviewID(2), reviewID(3), tc.a, tc.b, tc.kind, tc.rationale, at); err == nil {
				t.Fatal("accepted invalid review decision")
			}
		})
	}
}

func TestRelationEditChangesTheAssessmentButKeepsItsIdentity(t *testing.T) {
	at := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	later := at.Add(time.Hour)
	held, err := domain.New(reviewID(1), reviewID(2), reviewID(3), reviewID(4), reviewID(5), "unresolved", "Need another source", at)
	if err != nil {
		t.Fatal(err)
	}
	got, err := held.Edit(reviewID(8), "contradicts", "The dates cannot both be true", later)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != held.ID || got.LeftObservationID != held.LeftObservationID || got.RightObservationID != held.RightObservationID {
		t.Fatalf("edit changed relation identity: %+v", got)
	}
	if got.Kind != domain.Contradicts || got.Author != reviewID(8) || !got.UpdatedAt.Equal(later) {
		t.Fatalf("edit did not update assessment: %+v", got)
	}
	if !got.CreatedAt.Equal(at) {
		t.Fatalf("edit changed creation time: %v", got.CreatedAt)
	}
}
