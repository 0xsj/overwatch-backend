package domain

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestBriefNormalisesAndKeepsAuthorship(t *testing.T) {
	want := id.ID{1}
	workspace := id.ID{2}
	author := id.ID{3}
	other := id.ID{4}
	first, err := New(want, workspace, author, "  Handoff  ", "  What happened? ", "  Account  ", " alternatives ", " limits ", " next ", []id.ID{other}, []id.ID{want}, nil, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if first.Title != "Handoff" || first.Question != "What happened?" || first.ObservationIDs[0] != other {
		t.Fatalf("brief was not normalised: %+v", first)
	}
	next, err := first.Edit(id.ID{5}, "Updated", "Still?", "new account", "new alternatives", "new limits", "new steps", nil, nil, nil, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if next.Author != author || next.UpdatedBy != (id.ID{5}) || !next.UpdatedAt.Equal(time.Unix(2, 0)) {
		t.Fatalf("edit moved authorship or time: %+v", next)
	}
}

func TestBriefRejectsDuplicateLinks(t *testing.T) {
	one := id.ID{1}
	_, err := New(id.ID{2}, id.ID{3}, id.ID{4}, "title", "question", "", "", "", "", []id.ID{one, one}, nil, nil, time.Unix(1, 0))
	if err != ErrDuplicateObservation {
		t.Fatalf("got %v, want duplicate observation error", err)
	}
	_, err = New(id.ID{2}, id.ID{3}, id.ID{4}, "title", "question", "", "", "", "", nil, []id.ID{one, one}, nil, time.Unix(1, 0))
	if err != ErrDuplicateQuestion {
		t.Fatalf("got %v, want duplicate question error", err)
	}
	_, err = NewWithEvents(id.ID{2}, id.ID{3}, id.ID{4}, "title", "question", "", "", "", "", nil, nil, nil, []id.ID{one, one}, time.Unix(1, 0))
	if err != ErrDuplicateEvent {
		t.Fatalf("got %v, want duplicate event error", err)
	}
}

func TestSnapshotCopiesLinkedQuestionAtFreezeTime(t *testing.T) {
	brief, err := New(id.ID{1}, id.ID{2}, id.ID{3}, "title", "question", "account", "", "", "next", nil, []id.ID{{4}}, nil, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	linkedObservation := id.ID{7}
	questions := []QuestionSnapshot{{ID: id.ID{4}, Prompt: "Which account?", State: "open", ObservationIDs: []id.ID{linkedObservation}}}
	frozen, err := NewSnapshot(id.ID{5}, id.ID{6}, brief, questions, nil, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Questions[0].Prompt != "Which account?" || frozen.FrozenBy != (id.ID{6}) || frozen.SourceUpdatedAt != brief.UpdatedAt {
		t.Fatalf("unexpected snapshot: %+v", frozen)
	}
	questions[0].ObservationIDs[0] = id.ID{8}
	if len(frozen.Questions[0].ObservationIDs) != 1 || frozen.Questions[0].ObservationIDs[0] != linkedObservation {
		t.Fatalf("question observation links were not copied: %+v", frozen.Questions[0].ObservationIDs)
	}
	if _, err := NewSnapshot(id.ID{5}, id.ID{6}, brief, nil, nil, time.Unix(2, 0)); err != ErrQuestionSnapshotMissing {
		t.Fatalf("got %v, want missing question snapshot", err)
	}
}

func TestSnapshotCopiesLinkedConnectionAtFreezeTime(t *testing.T) {
	connection := id.ID{7}
	brief, err := New(id.ID{1}, id.ID{2}, id.ID{3}, "title", "question", "", "", "", "", nil, nil, []id.ID{connection}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := NewSnapshot(id.ID{4}, id.ID{5}, brief, nil, []ConnectionSnapshot{{ID: connection, FromRecordID: id.ID{8}, FromRecordKind: "person", FromRecordName: "A", ToRecordID: id.ID{9}, ToRecordKind: "place", ToRecordName: "East Quay", Kind: "located_at", State: "proposed", Rationale: "The wording places the person there."}}, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Connections[0].ToRecordName != "East Quay" || frozen.Connections[0].State != "proposed" {
		t.Fatalf("unexpected frozen connection: %+v", frozen.Connections[0])
	}
	if _, err := NewSnapshot(id.ID{4}, id.ID{5}, brief, nil, nil, time.Unix(2, 0)); err != ErrConnectionSnapshotMissing {
		t.Fatalf("got %v, want missing connection snapshot", err)
	}
}

func TestSnapshotCopiesLinkedEventAtFreezeTime(t *testing.T) {
	eventID := id.ID{10}
	brief, err := NewWithEvents(id.ID{1}, id.ID{2}, id.ID{3}, "title", "question", "", "", "", "", nil, nil, nil, []id.ID{eventID}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	event := EventSnapshot{ID: eventID, Title: "East Quay disruption", ReportedTime: "around 18:00", TimePrecision: "approximate", ObservationIDs: []id.ID{{11}},
		ParticipantRecords: []EventRecordSnapshot{{ID: id.ID{12}, Kind: "person", Name: "Harborline author", ObservationIDs: []id.ID{{13}}}},
		LocationRecord:     &EventRecordSnapshot{ID: id.ID{14}, Kind: "place", Name: "East Quay"},
	}
	frozen, err := NewSnapshotWithEvents(id.ID{4}, id.ID{5}, brief, nil, nil, []EventSnapshot{event}, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Events[0].Title != "East Quay disruption" || frozen.Events[0].ParticipantRecords[0].Name != "Harborline author" || frozen.Events[0].LocationRecord.Name != "East Quay" {
		t.Fatalf("unexpected frozen event: %+v", frozen.Events[0])
	}
	event.ObservationIDs[0] = id.ID{15}
	if frozen.Events[0].ObservationIDs[0] != (id.ID{11}) {
		t.Fatal("event observation links were not copied")
	}
	if _, err := NewSnapshotWithEvents(id.ID{4}, id.ID{5}, brief, nil, nil, nil, time.Unix(2, 0)); err != ErrEventSnapshotMissing {
		t.Fatalf("got %v, want missing event snapshot", err)
	}
}

func TestSnapshotCopiesLinkedEventRelationshipsAndEvidenceSides(t *testing.T) {
	first, second := id.ID{20}, id.ID{21}
	brief, err := NewWithEvents(id.ID{22}, id.ID{23}, id.ID{24}, "title", "question", "", "", "", "", nil, nil, nil, []id.ID{first, second}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	events := []EventSnapshot{
		{ID: first, Title: "First event", TimePrecision: "exact"},
		{ID: second, Title: "Second event", TimePrecision: "exact"},
	}
	relationship := EventRelationshipSnapshot{ID: id.ID{25}, FromEventID: first, ToEventID: second, Kind: "possibly_causes", Rationale: "The earlier event may explain the later account.", State: "proposed", SupportingObservationIDs: []id.ID{{26}}, OpposingObservationIDs: []id.ID{{27}}}
	frozen, err := NewSnapshotWithEventsAndClustersAndRelationships(id.ID{28}, id.ID{29}, brief, nil, nil, nil, events, []EventRelationshipSnapshot{relationship}, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(frozen.EventRelationships) != 1 || frozen.EventRelationships[0].Kind != "possibly_causes" || len(frozen.EventRelationships[0].SupportingObservationIDs) != 1 || len(frozen.EventRelationships[0].OpposingObservationIDs) != 1 {
		t.Fatalf("unexpected frozen event relationship: %+v", frozen.EventRelationships)
	}
	relationship.SupportingObservationIDs[0] = id.ID{30}
	if frozen.EventRelationships[0].SupportingObservationIDs[0] != (id.ID{26}) {
		t.Fatal("event relationship evidence links were not copied")
	}
	bad := relationship
	bad.FromEventID = id.ID{31}
	if _, err := NewSnapshotWithEventsAndClustersAndRelationships(id.ID{32}, id.ID{29}, brief, nil, nil, nil, events, []EventRelationshipSnapshot{bad}, time.Unix(2, 0)); err != ErrEventSnapshotMissing {
		t.Fatalf("got %v, want missing event snapshot", err)
	}
}
