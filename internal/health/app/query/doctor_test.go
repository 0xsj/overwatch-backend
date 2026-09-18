// Author-written. `CLAUDE.md` justifies `health` with one clause — *"a tool off
// PATH looks like silence"* — and the claim worth testing hardest is the one
// about the report's own honesty: a clean bill of health that does not say what
// it examined is indistinguishable from a report nobody ran.
package query_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/health/app/query"
	"github.com/0xsj/overwatch-backend/internal/health/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var at = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func an(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}

type frozen struct{}

func (frozen) Now() time.Time { return at }

// answers is what each probe returns. `failing` names the kinds whose read
// errors, which is the case this package exists to keep separate from "clean".
type answers struct {
	found   map[domain.Kind][]domain.Symptom
	looked  map[domain.Kind]int
	failing map[domain.Kind]error
}

func (a answers) of(k domain.Kind) ([]domain.Symptom, int, error) {
	if err, held := a.failing[k]; held {
		return nil, 0, err
	}
	return a.found[k], a.looked[k], nil
}

func (a answers) ToolsThatCouldNotStart(context.Context, id.ID) ([]domain.Symptom, int, error) {
	return a.of(domain.ToolUnavailable)
}
func (a answers) ToolsNobodyReads(context.Context, id.ID) ([]domain.Symptom, int, error) {
	return a.of(domain.ToolUnread)
}
func (a answers) FindingToolsWithNoSignature(context.Context, id.ID) ([]domain.Symptom, int, error) {
	return a.of(domain.ToolNoSignature)
}
func (a answers) ChecksThatCannotRun(context.Context, id.ID) ([]domain.Symptom, int, error) {
	return a.of(domain.CheckUnrunnable)
}
func (a answers) EventsThatGaveUp(context.Context, id.ID) ([]domain.Symptom, int, error) {
	return a.of(domain.EventBuried)
}
func (a answers) FieldsNobodyMapped(context.Context, id.ID) ([]domain.Symptom, int, error) {
	return a.of(domain.FieldUnmapped)
}

func examine(t *testing.T, in answers) domain.Report {
	t.Helper()
	got, err := query.NewDoctor(in, frozen{}).Examine(context.Background(), an(1))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// **EVERY KIND GETS A PROBE**, whether or not it found anything. A kind missing
// from this list is a bug in the assembler, not a clean result — and a screen
// cannot tell those apart.
func TestEveryKindIsProbedEvenWhenNothingIsWrong(t *testing.T) {
	got := examine(t, answers{looked: map[domain.Kind]int{
		domain.ToolUnavailable: 412, domain.ToolUnread: 6,
		domain.ToolNoSignature: 6, domain.CheckUnrunnable: 4,
		domain.EventBuried: 900, domain.FieldUnmapped: 0,
	}})
	if len(got.Probes) != len(domain.All) {
		t.Fatalf("want %d probes, got %d", len(domain.All), len(got.Probes))
	}
	for n, p := range got.Probes {
		if p.Kind != domain.All[n] {
			t.Fatalf("probe %d is %s, want %s", n, p.Kind, domain.All[n])
		}
		if !p.Measured {
			t.Fatalf("%s was not measured", p.Kind)
		}
	}
	if len(got.Symptoms) != 0 {
		t.Fatalf("nothing was wrong: %+v", got.Symptoms)
	}
	// **AND THE DENOMINATOR IS THERE.** "No tool failed to start" is a
	// measurement only beside "out of 412 invocations".
	if got.Probes[0].Looked != 412 {
		t.Fatalf("the denominator survives: %+v", got.Probes[0])
	}
	if !got.Trustworthy() {
		t.Fatal("every probe ran, so `nothing is wrong` is sayable")
	}
}

// **THE test of this package.** A probe that could not run is NOT a clean probe,
// and one failing read must not blank the screen — a health endpoint that goes
// down when a dependency does has become the thing it exists to detect.
func TestAFailingProbeDoesNotFailTheReportAndIsNotCounted(t *testing.T) {
	got := examine(t, answers{
		looked:  map[domain.Kind]int{domain.ToolUnavailable: 412},
		failing: map[domain.Kind]error{domain.EventBuried: errors.New("outbox unreachable")},
	})
	if len(got.Probes) != len(domain.All) {
		t.Fatalf("a failing probe is still a probe: %d", len(got.Probes))
	}
	var buried domain.Probe
	for _, p := range got.Probes {
		if p.Kind == domain.EventBuried {
			buried = p
		}
	}
	if buried.Measured {
		t.Fatal("a read that errored was reported as measured")
	}
	if buried.Because == "" {
		t.Fatal("an unmeasured probe says why")
	}
	// **AND `nothing is wrong` STOPS BEING SAYABLE.** The symptom list is empty
	// and that now means "nothing found where we could look".
	if got.Trustworthy() {
		t.Fatal("a report with an unmeasured probe is not trustworthy")
	}
	if len(got.Unmeasured()) != 1 {
		t.Fatalf("one probe could not run: %+v", got.Unmeasured())
	}
	// The other five still answered, which is the whole point of not failing.
	measured := 0
	for _, p := range got.Probes {
		if p.Measured {
			measured++
		}
	}
	if measured != len(domain.All)-1 {
		t.Fatalf("one failure took out %d probes", len(domain.All)-measured)
	}
}

// Symptoms come back WORST KIND FIRST, then by count. A health screen sorted by
// time puts a tool that has been off PATH for a week under a field nobody
// mapped this morning.
func TestSymptomsAreWorstFirstThenByCount(t *testing.T) {
	got := examine(t, answers{found: map[domain.Kind][]domain.Symptom{
		domain.FieldUnmapped: {{Kind: domain.FieldUnmapped, Subject: ".tech[]", Count: 400}},
		domain.ToolUnavailable: {
			{Kind: domain.ToolUnavailable, Subject: "httpx", Count: 2},
			{Kind: domain.ToolUnavailable, Subject: "subfinder", Count: 40},
		},
		domain.CheckUnrunnable: {{Kind: domain.CheckUnrunnable, Subject: "surface", Count: 1}},
	}})
	if len(got.Symptoms) != 4 {
		t.Fatalf("want four, got %d", len(got.Symptoms))
	}
	// A tool off PATH outranks 400 unmapped fields, and within a kind the
	// bigger count comes first.
	want := []string{"subfinder", "httpx", "surface", ".tech[]"}
	for n, one := range got.Symptoms {
		if one.Subject != want[n] {
			t.Fatalf("position %d is %q, want %q (%v)", n, one.Subject, want[n],
				[]string{got.Symptoms[0].Subject, got.Symptoms[1].Subject,
					got.Symptoms[2].Subject, got.Symptoms[3].Subject})
		}
	}
}

// Every kind says what its silence MEANS. A symptom list of enum names is a list
// nobody acts on, and the sentence is server-side because it is an argument
// about the absence rather than a label.
func TestEveryKindSaysWhatItsSilenceMeans(t *testing.T) {
	for _, kind := range domain.All {
		if kind.String() == "" {
			t.Fatalf("kind %d has no name", kind)
		}
		if kind.Says() == "" {
			t.Fatalf("%s does not say what it means", kind)
		}
	}
}

func TestExamineNeedsAnEngagement(t *testing.T) {
	_, err := query.NewDoctor(answers{}, frozen{}).Examine(context.Background(), id.ID{})
	if !errors.Is(err, domain.ErrWorkspaceRequired) {
		t.Fatalf("want ErrWorkspaceRequired, got %v", err)
	}
}
