// Author-written, from decisions/0032's Verification block. NOT barriered — the
// oracle is a record written before the code, which is independent of the
// implementation; the author of both is the same, which is not.
package domain_test

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/check/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var at = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

func nonZero(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}

func step(n, tool byte) domain.Step {
	return domain.Step{ID: nonZero(n), CheckID: nonZero(200), ToolID: nonZero(tool)}
}

func flow(from, to byte) domain.Flow {
	return domain.Flow{CheckID: nonZero(200), From: nonZero(from), To: nonZero(to)}
}

// "a cycle" — 0032 Verification.
func TestAChainWithACycleIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flows []domain.Flow
	}{
		{"two steps", []domain.Flow{flow(1, 2), flow(2, 1)}},
		{"three steps", []domain.Flow{flow(1, 2), flow(2, 3), flow(3, 1)}},
		{"a cycle reached from a clean source", []domain.Flow{
			flow(1, 2), flow(2, 3), flow(3, 4), flow(4, 2),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chain := domain.Chain{
				Steps: []domain.Step{step(1, 10), step(2, 11), step(3, 12), step(4, 13)},
				Flows: tc.flows,
			}
			if err := chain.Validate(); !errors.Is(err, domain.ErrChainCyclic) {
				t.Fatalf("want ErrChainCyclic, got %v", err)
			}
		})
	}
}

// A diamond is not a cycle. This is the case a naive "have I seen this node"
// walk gets wrong: step 4 is reached twice by two different paths and nothing
// about that is circular.
func TestADiamondIsNotACycle(t *testing.T) {
	chain := domain.Chain{
		Steps: []domain.Step{step(1, 10), step(2, 11), step(3, 12), step(4, 13)},
		Flows: []domain.Flow{flow(1, 2), flow(1, 3), flow(2, 4), flow(3, 4)},
	}
	if err := chain.Validate(); err != nil {
		t.Fatalf("a diamond is acyclic: %v", err)
	}
}

// "a flow whose endpoints are not both steps of that check" — 0032.
func TestAFlowMustNameStepsTheChainHas(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    domain.Flow
	}{
		{"unknown source", flow(9, 2)},
		{"unknown target", flow(1, 9)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chain := domain.Chain{
				Steps: []domain.Step{step(1, 10), step(2, 11)},
				Flows: []domain.Flow{tc.f},
			}
			if err := chain.Validate(); !errors.Is(err, domain.ErrStepUnknown) {
				t.Fatalf("want ErrStepUnknown, got %v", err)
			}
		})
	}
}

func TestAStepCannotFeedItself(t *testing.T) {
	chain := domain.Chain{
		Steps: []domain.Step{step(1, 10)},
		Flows: []domain.Flow{flow(1, 1)},
	}
	if err := chain.Validate(); !errors.Is(err, domain.ErrFlowToSelf) {
		t.Fatalf("want ErrFlowToSelf, got %v", err)
	}
}

// A duplicate edge is a second copy of one fact, and the primary key refuses it.
// The domain refuses it first so the editor gets a message rather than a
// constraint violation.
func TestADuplicateFlowIsRefused(t *testing.T) {
	chain := domain.Chain{
		Steps: []domain.Step{step(1, 10), step(2, 11)},
		Flows: []domain.Flow{flow(1, 2), flow(1, 2)},
	}
	if err := chain.Validate(); !errors.Is(err, domain.ErrFlowDuplicate) {
		t.Fatalf("want ErrFlowDuplicate, got %v", err)
	}
}

// "a step naming a tool from another org" is checked in the command, which can
// see `tool`. What the DOMAIN owes is that a step names a tool at all.
func TestAStepNeedsATool(t *testing.T) {
	chain := domain.Chain{Steps: []domain.Step{{ID: nonZero(1), CheckID: nonZero(200)}}}
	if err := chain.Validate(); !errors.Is(err, domain.ErrToolRequired) {
		t.Fatalf("want ErrToolRequired, got %v", err)
	}
}

// READ BY YOU. An empty chain is the human check and is valid.
func TestAnEmptyChainIsTheHumanCheck(t *testing.T) {
	var chain domain.Chain
	if !chain.Empty() {
		t.Fatal("a chain with no steps is empty")
	}
	if err := chain.Validate(); err != nil {
		t.Fatalf("the human check has no chain and that is not an error: %v", err)
	}
	if got := chain.Sources(); len(got) != 0 {
		t.Fatalf("no steps means no sources, got %d", len(got))
	}
}

// A source is a step nothing feeds. A chain of steps with no flows at all is
// four sources, not an error — a half-drawn graph is a normal thing to save.
func TestSourcesAreTheStepsNothingFeeds(t *testing.T) {
	chain := domain.Chain{
		Steps: []domain.Step{step(1, 10), step(2, 11), step(3, 12)},
		Flows: []domain.Flow{flow(1, 2), flow(1, 3)},
	}
	sources := chain.Sources()
	if len(sources) != 1 || sources[0].ID != nonZero(1) {
		t.Fatalf("want step 1 as the only source, got %v", sources)
	}

	unlinked := domain.Chain{Steps: []domain.Step{step(1, 10), step(2, 11)}}
	if err := unlinked.Validate(); err != nil {
		t.Fatalf("a chain with no flows is valid: %v", err)
	}
	if got := len(unlinked.Sources()); got != 2 {
		t.Fatalf("both steps are sources, got %d", got)
	}
}

// "a check with an empty applies_to" — 0032 Verification.
func TestACheckAppliesToSomething(t *testing.T) {
	_, err := domain.New(nonZero(1), nonZero(2), nonZero(3), domain.Draft{
		Name: "ports", Question: "what ports are open?",
	}, at)
	if !errors.Is(err, domain.ErrAppliesEmpty) {
		t.Fatalf("want ErrAppliesEmpty, got %v", err)
	}
}

// n/a is NOT a pair — 0011. `Applies` false is the cell that does not exist, and
// it must be distinguishable from "never checked", which lives elsewhere.
func TestAppliesAnswersOnlyForDeclaredKinds(t *testing.T) {
	c, err := domain.New(nonZero(1), nonZero(2), nonZero(3), domain.Draft{
		Name:      "tls",
		Question:  "what certificate does it present?",
		AppliesTo: []domain.Subject{domain.SubjectHost, domain.SubjectURL},
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Applies(domain.SubjectHost) {
		t.Fatal("declared host, so a host is a pair")
	}
	if c.Applies(domain.SubjectASN) {
		t.Fatal("an ASN has no TLS certificate — that cell is n/a, not never")
	}
}

// "READ BY YOU has applies_to = all six kinds and interval NULL" — 0032.
func TestTheHumanCheckIsUniversalAndHasNoClock(t *testing.T) {
	c, err := domain.New(nonZero(1), nonZero(2), nonZero(3), domain.Draft{
		Name:      "read by you",
		Question:  "has a person looked at this?",
		AppliesTo: domain.EverySubject(),
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	if !c.OnDemand() {
		t.Fatal("a human read does not go stale, so it has no interval")
	}
	for _, s := range domain.EverySubject() {
		if !c.Applies(s) {
			t.Fatalf("a person can read anything, including %s", s)
		}
	}
}

// EverySubject must hand back a fresh slice. A caller appending to a shared one
// would widen the human check for everybody, silently.
func TestEverySubjectCannotBeMutatedByACaller(t *testing.T) {
	// The count is READ, not written down. Hard-coding it made this test fail
	// when the vocabulary changed (0034) for a reason that had nothing to do
	// with what it asserts — which is only that a caller cannot edit the
	// package's own slice.
	want := len(domain.EverySubject())
	first := domain.EverySubject()
	first = append(first[:1], first[2:]...)
	_ = first
	if got := len(domain.EverySubject()); got != want {
		t.Fatalf("EverySubject was edited through a caller: %d, want %d", got, want)
	}
}

// A stored order nobody chose is a diff nobody made.
func TestAppliesToIsSortedAndDeduplicated(t *testing.T) {
	c, err := domain.New(nonZero(1), nonZero(2), nonZero(3), domain.Draft{
		Name:     "ports",
		Question: "what ports are open?",
		AppliesTo: []domain.Subject{
			domain.SubjectURL, domain.SubjectHost, domain.SubjectHost, domain.SubjectCIDR,
		},
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Subject{domain.SubjectHost, domain.SubjectCIDR, domain.SubjectURL}
	if len(c.AppliesTo) != len(want) {
		t.Fatalf("want %v, got %v", want, c.AppliesTo)
	}
	for i := range want {
		if c.AppliesTo[i] != want[i] {
			t.Fatalf("want %v, got %v", want, c.AppliesTo)
		}
	}
}

// A subject crosses a module boundary as its SPELLING, never as an ordinal.
func TestEverySubjectRoundTripsThroughItsName(t *testing.T) {
	for _, s := range domain.EverySubject() {
		back, err := domain.ParseSubject(s.String())
		if err != nil {
			t.Fatalf("%v does not parse back from %q: %v", s, s.String(), err)
		}
		if back != s {
			t.Fatalf("%q parsed to %v, not %v", s.String(), back, s)
		}
	}
	if _, err := domain.ParseSubject("finding"); !errors.Is(err, domain.ErrSubjectUnknown) {
		t.Fatal("a finding is not a coverage subject — 0032 §Consequences")
	}
}

func TestAnIntervalCannotBeNegative(t *testing.T) {
	_, err := domain.New(nonZero(1), nonZero(2), nonZero(3), domain.Draft{
		Name: "ports", Question: "what ports are open?",
		AppliesTo: []domain.Subject{domain.SubjectHost},
		Interval:  -time.Hour,
	}, at)
	if !errors.Is(err, domain.ErrIntervalNegative) {
		t.Fatalf("want ErrIntervalNegative, got %v", err)
	}
}
