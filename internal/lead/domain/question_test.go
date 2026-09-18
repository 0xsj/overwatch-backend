package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/lead/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func leadID(value byte) id.ID {
	var out id.ID
	out[15] = value
	return out
}

func TestQuestionCanonicalisesCitedObservations(t *testing.T) {
	at := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	got, err := domain.New(leadID(1), leadID(2), leadID(3),
		"  Which notice is the original account?  ", "The reports use similar wording.",
		"open", "", []id.ID{leadID(9), leadID(4)}, at)
	if err != nil {
		t.Fatal(err)
	}
	if got.Prompt != "Which notice is the original account?" || got.Context != "The reports use similar wording." {
		t.Fatalf("text was not normalised: %+v", got)
	}
	if len(got.ObservationIDs) != 2 || got.ObservationIDs[0] != leadID(4) || got.ObservationIDs[1] != leadID(9) {
		t.Fatalf("links were not canonicalised: %+v", got.ObservationIDs)
	}
}

func TestQuestionClosedStatesNeedAResolution(t *testing.T) {
	at := time.Now()
	for _, state := range []string{"answered", "dismissed"} {
		if _, err := domain.New(leadID(1), leadID(2), leadID(3), "Question", "", state, "", nil, at); err == nil {
			t.Fatalf("%s question accepted without a resolution", state)
		}
	}
	if _, err := domain.New(leadID(1), leadID(2), leadID(3), "Question", "", "open", "not open", nil, at); err == nil {
		t.Fatal("open question accepted a resolution")
	}
	if _, err := domain.New(leadID(1), leadID(2), leadID(3), strings.Repeat("x", domain.MaxQuestionLength+1), "", "open", "", nil, at); err == nil {
		t.Fatal("overlong question accepted")
	}
}

func TestQuestionEditKeepsItsAuthorAndCreationTime(t *testing.T) {
	at := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	later := at.Add(time.Hour)
	held, err := domain.New(leadID(1), leadID(2), leadID(3), "What happened?", "", "open", "", nil, at)
	if err != nil {
		t.Fatal(err)
	}
	got, err := held.Edit(leadID(8), "What happened at 18:00?", "The reports disagree.", "answered", "The second notice was published later.", []id.ID{leadID(5)}, later)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != held.ID || got.Author != held.Author || !got.CreatedAt.Equal(at) {
		t.Fatalf("edit changed identity: %+v", got)
	}
	if got.UpdatedBy != leadID(8) || got.State != domain.Answered || !got.UpdatedAt.Equal(later) {
		t.Fatalf("edit did not change current state: %+v", got)
	}
}

func TestQuestionRejectsDuplicateAndExcessiveCitations(t *testing.T) {
	at := time.Now()
	if _, err := domain.New(leadID(1), leadID(2), leadID(3), "Question", "", "open", "", []id.ID{leadID(4), leadID(4)}, at); err == nil {
		t.Fatal("duplicate observation accepted")
	}
	tooMany := make([]id.ID, domain.MaxObservationLinks+1)
	for i := range tooMany {
		tooMany[i] = leadID(byte(i + 1))
	}
	if _, err := domain.New(leadID(1), leadID(2), leadID(3), "Question", "", "open", "", tooMany, at); err == nil {
		t.Fatal("too many observations accepted")
	}
}
