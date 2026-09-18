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
