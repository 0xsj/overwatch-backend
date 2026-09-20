package domain_test

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/event/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestParticipantRolesRemainExplicitAndDistinct(t *testing.T) {
	at := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	event, err := domain.NewWithParticipantLinks(eid(41), eid(42), eid(43), "Harbor disruption", "", "18:00", "exact", "2026-09-19", "East Quay", nil, []domain.ParticipantLink{
		{RecordID: eid(44), Role: domain.ParticipantActor},
		{RecordID: eid(45), Role: domain.ParticipantAffected},
	}, nil, at)
	if err != nil {
		t.Fatal(err)
	}
	if len(event.ParticipantLinks) != 2 || event.ParticipantLinks[0].Role != domain.ParticipantActor || event.ParticipantLinks[1].Role != domain.ParticipantAffected || event.ParticipantRecordIDs[0] != eid(44) {
		t.Fatalf("unexpected participant links: %+v", event)
	}
	if _, err := domain.NewWithParticipantLinks(eid(46), eid(42), eid(43), "Bad role", "", "18:00", "exact", "2026-09-19", "", nil, []domain.ParticipantLink{{RecordID: eid(44), Role: "invented"}}, nil, at); err != domain.ErrParticipantRoleUnknown {
		t.Fatalf("unknown role error=%v, want role error", err)
	}
}

func TestEventRelationshipReviewKeepsTypeAndRequiresDecisionNote(t *testing.T) {
	createdAt := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	reviewedAt := createdAt.Add(time.Hour)
	relationship, err := domain.NewRelationship(eid(47), eid(48), eid(49), eid(50), eid(51), domain.RelationshipPrecedes, "The reported ordering dates support a sequence.", createdAt)
	if err != nil {
		t.Fatal(err)
	}
	if relationship.State != domain.RelationshipProposed || relationship.Kind != domain.RelationshipPrecedes {
		t.Fatalf("unexpected relationship: %+v", relationship)
	}
	if _, err := relationship.Review(eid(52), domain.RelationshipAccepted, "", reviewedAt); err != domain.ErrEventRelationshipReviewRequired {
		t.Fatalf("empty review note error=%v, want review required", err)
	}
	relationship, err = relationship.Review(eid(52), domain.RelationshipAccepted, "The sequence is accepted for this investigation.", reviewedAt)
	if err != nil {
		t.Fatal(err)
	}
	if relationship.State != domain.RelationshipAccepted || relationship.ReviewedBy == nil || *relationship.ReviewedBy != eid(52) || relationship.Kind != domain.RelationshipPrecedes {
		t.Fatalf("unexpected reviewed relationship: %+v", relationship)
	}
	if _, err := domain.NewRelationship(eid(53), eid(48), eid(49), eid(49), eid(51), domain.RelationshipRelated, "Self relation", createdAt); err != domain.ErrEventRelationshipSelf {
		t.Fatalf("self relationship error=%v, want self error", err)
	}
}

func TestEventRelationshipEvidenceKeepsSidesDistinctAndAllowsQualifiedCausality(t *testing.T) {
	at := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	relationship, err := domain.NewRelationshipWithEvidence(eid(54), eid(55), eid(56), eid(57), eid(58), domain.RelationshipPossiblyCauses, "The earlier disruption may explain the later account, but the evidence remains contested.", []id.ID{eid(60)}, []id.ID{eid(61)}, at)
	if err != nil {
		t.Fatal(err)
	}
	if relationship.Kind != domain.RelationshipPossiblyCauses || len(relationship.SupportingObservationIDs) != 1 || len(relationship.OpposingObservationIDs) != 1 || relationship.State != domain.RelationshipProposed {
		t.Fatalf("unexpected evidence-backed relationship: %+v", relationship)
	}
	if _, err := domain.NewRelationshipWithEvidence(eid(62), eid(55), eid(56), eid(57), eid(58), domain.RelationshipPossiblyCauses, "The same observation cannot be placed on both evidence sides.", []id.ID{eid(63)}, []id.ID{eid(63)}, at); err != domain.ErrDuplicateRelationshipObservation {
		t.Fatalf("overlapping evidence error=%v, want duplicate relationship observation", err)
	}
}
