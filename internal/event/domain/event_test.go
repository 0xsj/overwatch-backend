package domain_test

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/event/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func eid(value byte) id.ID { var out id.ID; out[15] = value; return out }

func TestEventPreservesUnknownTimeAndCanonicalisesObservationLinks(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	one, err := domain.New(eid(1), eid(2), eid(3), "East Quay disruption", "The reports may describe one incident.", "", "unknown", "", "East Quay", []id.ID{eid(8), eid(7)}, at)
	if err != nil {
		t.Fatal(err)
	}
	if one.TimePrecision != domain.TimeUnknown || len(one.ObservationIDs) != 2 || one.ObservationIDs[0] != eid(7) {
		t.Fatalf("unexpected event: %+v", one)
	}
	if _, err := one.Edit(eid(4), one.Title, one.Description, "", "unknown", "2026-09-17", one.Location, one.ObservationIDs, at); err != domain.ErrTimeRequired {
		t.Fatalf("sort date without time error=%v, want time required", err)
	}
}

func TestEventRejectsMalformedOrderingDateAndDuplicateCitations(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	if _, err := domain.New(eid(11), eid(12), eid(13), "Event", "", "18:00", "exact", "2026-02-30", "", nil, at); err != domain.ErrSortDateInvalid {
		t.Fatalf("bad date error=%v, want sort date error", err)
	}
	if _, err := domain.New(eid(11), eid(12), eid(13), "Event", "", "18:00", "exact", "2026-09-17", "", []id.ID{eid(14), eid(14)}, at); err != domain.ErrDuplicateObservation {
		t.Fatalf("duplicate citation error=%v, want duplicate error", err)
	}
}
