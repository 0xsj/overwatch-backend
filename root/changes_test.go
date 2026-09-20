package root

import (
	"testing"
	"time"

	rundomain "github.com/0xsj/overwatch-backend/internal/run/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func testID(t *testing.T, value string) id.ID {
	t.Helper()
	found, err := id.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func TestDiffRunSnapshotsKeepsMeasuredAbsenceApartFromNeverChecked(t *testing.T) {
	current := rundomain.Run{
		ID:         testID(t, "01a07bc3-7004-7000-9000-000000000001"),
		StartedAt:  time.Date(2026, 9, 6, 22, 4, 11, 0, time.UTC),
		FinishedAt: time.Date(2026, 9, 6, 22, 4, 31, 0, time.UTC),
	}
	previous := rundomain.Run{
		ID:        testID(t, "01a07bc3-7004-7000-9000-000000000002"),
		StartedAt: time.Date(2026, 9, 5, 3, 0, 2, 0, time.UTC),
	}

	old := newRunSnapshot()
	old.Subjects[subjectKey("host", "api.example")] = struct{}{}
	old.Values[observationKey("host", "api.example", "http.server")] = observedValue{
		SubjectKind: "host", SubjectValue: "api.example", Field: "http.server", Value: "nginx/1.24",
	}
	old.Subjects[subjectKey("host", "old.example")] = struct{}{}
	old.Values[observationKey("host", "old.example", "dns.a")] = observedValue{
		SubjectKind: "host", SubjectValue: "old.example", Field: "dns.a", Value: "203.0.113.7",
	}
	old.Subjects[subjectKey("host", "never.example")] = struct{}{}
	old.Values[observationKey("host", "never.example", "http.title")] = observedValue{
		SubjectKind: "host", SubjectValue: "never.example", Field: "http.title", Value: "Old",
	}

	now := newRunSnapshot()
	now.Subjects[subjectKey("host", "api.example")] = struct{}{}
	now.Values[observationKey("host", "api.example", "http.server")] = observedValue{
		SubjectKind: "host", SubjectValue: "api.example", Field: "http.server", Value: "nginx/1.25",
	}
	now.Subjects[subjectKey("host", "old.example")] = struct{}{}
	now.Subjects[subjectKey("host", "new.example")] = struct{}{}
	now.Values[observationKey("host", "new.example", "dns.a")] = observedValue{
		SubjectKind: "host", SubjectValue: "new.example", Field: "dns.a", Value: "203.0.113.8",
	}

	changes := diffRunSnapshots("target-1", "Target", current, previous, now, old)
	if len(changes) != 3 {
		t.Fatalf("got %d changes, want changed + added + gone", len(changes))
	}
	got := map[string]string{}
	for _, change := range changes {
		got[change.SubjectValue+":"+change.Field] = change.Kind
	}
	if got["api.example:http.server"] != "changed" {
		t.Errorf("changed value classified as %q", got["api.example:http.server"])
	}
	if got["new.example:dns.a"] != "added" {
		t.Errorf("new subject classified as %q", got["new.example:dns.a"])
	}
	if got["old.example:dns.a"] != "gone" {
		t.Errorf("measured absence classified as %q", got["old.example:dns.a"])
	}
	if _, exists := got["never.example:http.title"]; exists {
		t.Error("never-measured subject was incorrectly classified as gone")
	}
}
