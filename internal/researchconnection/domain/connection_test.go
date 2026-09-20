package domain

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestConnectionCanonicalisesEvidenceAndKeepsAssessmentSeparate(t *testing.T) {
	at := time.Unix(10, 0).UTC()
	connection, err := New(id.ID{10}, id.ID{11}, id.ID{12}, id.ID{13}, id.ID{14}, "may_belong_to", "proposed", "The account may be controlled by the person.", []id.ID{idFrom(2), idFrom(1)}, []id.ID{idFrom(3)}, at)
	if err != nil {
		t.Fatal(err)
	}
	flags := connection.ReviewFlags()
	if connection.State != Proposed || connection.SupportingObservationIDs[0] != idFrom(1) || connection.SupportingObservationIDs[1] != idFrom(2) || !flags.Open || !flags.Conflicted || flags.Uncited {
		t.Fatalf("connection: %+v", connection)
	}
	edited, err := connection.Edit(id.ID{15}, "may_belong_to", "deferred", "The evidence is not yet sufficient.", []id.ID{idFrom(1)}, []id.ID{idFrom(3)}, at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if edited.State != Deferred || edited.UpdatedBy.IsZero() || edited.Author != connection.Author {
		t.Fatalf("edited connection: %+v", edited)
	}
}

func TestConnectionReviewFlagsIdentifyUncitedClosedAssessment(t *testing.T) {
	one, err := New(id.ID{1}, id.ID{2}, id.ID{3}, id.ID{4}, id.ID{5}, "associated_with", "accepted", "The authored relationship is retained for reference.", nil, nil, time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	flags := one.ReviewFlags()
	if flags.Open || flags.Conflicted || !flags.Uncited {
		t.Fatalf("review flags: %+v", flags)
	}
}

func TestConnectionRejectsConflictingEvidence(t *testing.T) {
	_, err := New(id.ID{10}, id.ID{11}, id.ID{12}, id.ID{13}, id.ID{14}, "associated_with", "proposed", "Review this possibility.", []id.ID{idFrom(1)}, []id.ID{idFrom(1)}, time.Unix(10, 0))
	if err != ErrEvidenceConflict {
		t.Fatalf("error = %v, want %v", err, ErrEvidenceConflict)
	}
}

func TestPossibleSameSubjectIsAnExplicitNonMergingRelationship(t *testing.T) {
	one, err := New(id.ID{1}, id.ID{2}, id.ID{3}, id.ID{4}, id.ID{5}, "possible_same_subject", "proposed", "Two authored records may refer to the same subject; review scope.", nil, nil, time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if one.Kind != PossibleSameSubject || one.FromRecordID == one.ToRecordID {
		t.Fatalf("unexpected possible-same-subject relationship: %+v", one)
	}
}

func idFrom(value byte) id.ID {
	var out id.ID
	out[15] = value
	return out
}
