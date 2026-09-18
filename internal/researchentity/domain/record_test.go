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
