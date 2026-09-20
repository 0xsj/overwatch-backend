package domain

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

func recordID(n byte) id.ID { var out id.ID; out[0] = n; return out }

func TestRecordKeepsAuthoredKindAndCanonicalObservationLinks(t *testing.T) {
	at := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	one, err := New(recordID(1), recordID(2), recordID(3), "account", "@harborline", "Possible public account.", []id.ID{recordID(5), recordID(4)}, at)
	if err != nil {
		t.Fatal(err)
	}
	if one.Kind != Account || one.ObservationIDs[0] != recordID(4) || one.ObservationIDs[1] != recordID(5) {
		t.Fatalf("record: %+v", one)
	}
}

func TestRecordRejectsUnknownKindAndDuplicateObservation(t *testing.T) {
	at := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	if _, err := New(recordID(1), recordID(2), recordID(3), "thing", "name", "", nil, at); err != ErrKindUnknown {
		t.Fatalf("unknown kind: %v", err)
	}
	if _, err := New(recordID(1), recordID(2), recordID(3), "person", "name", "", []id.ID{recordID(4), recordID(4)}, at); err != ErrDuplicateObservation {
		t.Fatalf("duplicate observation: %v", err)
	}
}

func TestPlaceGeometryIsCitedQualifiedAndCopied(t *testing.T) {
	at := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	geometry := &PlaceGeometry{Latitude: 41.0082, Longitude: 28.9784, Precision: PlaceApproximate, ObservationIDs: []id.ID{recordID(5)}}
	one, err := NewWithPlaceGeometry(recordID(1), recordID(2), recordID(3), "place", "East Quay", "A reported place.", []id.ID{recordID(5)}, geometry, at)
	if err != nil {
		t.Fatal(err)
	}
	geometry.ObservationIDs[0] = recordID(9)
	if one.PlaceGeometry == nil || one.PlaceGeometry.ObservationIDs[0] != recordID(5) || one.PlaceGeometry.Precision != PlaceApproximate {
		t.Fatalf("geometry was not copied: %+v", one.PlaceGeometry)
	}
	if _, err := NewWithPlaceGeometry(recordID(1), recordID(2), recordID(3), "person", "Not a place", "", []id.ID{recordID(5)}, geometry, at); err != ErrPlaceGeometryKind {
		t.Fatalf("non-place geometry: %v", err)
	}
	if _, err := NewWithPlaceGeometry(recordID(1), recordID(2), recordID(3), "place", "Bad point", "", []id.ID{recordID(5)}, &PlaceGeometry{Latitude: 91, Longitude: 0, Precision: PlaceExact, ObservationIDs: []id.ID{recordID(5)}}, at); err != ErrPlaceGeometryCoordinates {
		t.Fatalf("invalid coordinates: %v", err)
	}
}
