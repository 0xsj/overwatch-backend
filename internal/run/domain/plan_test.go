// Author-written, from decisions/0033's Verification block and §4, which were
// written before the code.
package domain_test

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/run/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var at = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

func nonZero(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}

// 0033 §4. THE test of this package: a value carrying shell metacharacters is
// one argv element, because the split happened first.
func TestASubstitutedValueIsAlwaysOneArgvElement(t *testing.T) {
	for _, value := range []string{
		"x; rm -rf /",
		"a b c",
		"$(whoami)",
		"`id`",
		"--oops --another",
		"one\ttab",
		"trailing ",
	} {
		got, err := domain.Argv("subfinder -d {{target}}", value)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 {
			t.Fatalf("%q produced %d elements, want 3: %q", value, len(got), got)
		}
		if got[2] != value {
			t.Fatalf("the value was altered: want %q, got %q", value, got[2])
		}
	}
}

func TestArgvSplitsTheTemplateAndNotTheValue(t *testing.T) {
	got, err := domain.Argv("  httpx   -json  -u {{host}} ", "example.com")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"httpx", "-json", "-u", "example.com"}
	if len(got) != len(want) {
		t.Fatalf("want %q, got %q", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %q, got %q", want, got)
		}
	}
}

// Any placeholder takes the value — a source step has one input, so a binding
// vocabulary would have nothing to bind.
func TestEveryPlaceholderSpellingTakesTheValue(t *testing.T) {
	for _, template := range []string{
		"t {{target}}", "t {{domain}}", "t {{host}}", "t {{whatever_you_like}}",
	} {
		got, err := domain.Argv(template, "acme.test")
		if err != nil {
			t.Fatal(err)
		}
		if got[1] != "acme.test" {
			t.Fatalf("%q left %q", template, got[1])
		}
	}
}

// THE COST OF SPLITTING FIRST, stated as a test rather than left to be
// discovered: a placeholder cannot contain whitespace, because the split has
// already happened by the time anything looks for `{{`. The halves are left
// verbatim, which is what an unterminated placeholder does everywhere else.
func TestAPlaceholderCannotContainWhitespace(t *testing.T) {
	got, err := domain.Argv("t {{two words}}", "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[1] != "{{two" || got[2] != "words}}" {
		t.Fatalf("want the halves verbatim, got %q", got)
	}
}

func TestTwoPlaceholdersInOneFieldBothResolve(t *testing.T) {
	got, err := domain.Argv("t {{a}}/{{b}}", "x")
	if err != nil {
		t.Fatal(err)
	}
	if got[1] != "x/x" {
		t.Fatalf("want x/x, got %q", got[1])
	}
}

// Guessing at intent would put `{{targe` into an argv as though it were meant.
func TestAnUnterminatedPlaceholderIsLeftAsWritten(t *testing.T) {
	got, err := domain.Argv("t {{targe", "x")
	if err != nil {
		t.Fatal(err)
	}
	if got[1] != "{{targe" {
		t.Fatalf("want the field verbatim, got %q", got[1])
	}
}

func TestATemplateWithNoFieldsIsRefused(t *testing.T) {
	if _, err := domain.Argv("   ", "x"); !errors.Is(err, domain.ErrArgvEmpty) {
		t.Fatalf("want ErrArgvEmpty, got %v", err)
	}
}

// Every non-source step is skipped, with a reason naming what did not arrive.
// 0039 §2 and §3: a SOURCE step has exactly one candidate, the target, and it
// is known at plan time. A downstream step has none yet — its candidates are
// what its feeders observe, and nothing has run.
func TestASourceStepHasOneCandidateAndADownstreamStepHasNoneYet(t *testing.T) {
	slots, err := domain.Outline([]domain.Step{
		{StepID: nonZero(1), ToolID: nonZero(10), Template: "subfinder -d {{target}}", Source: true},
		{StepID: nonZero(2), ToolID: nonZero(11), Template: "httpx -u {{host}}"},
		{StepID: nonZero(3), ToolID: nonZero(12), Template: "nuclei -u {{url}}"},
	}, "ACME.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 3 {
		t.Fatalf("every step gets a slot, got %d", len(slots))
	}
	if len(slots[0].Values) != 1 || slots[0].Values[0] != "acme.test" {
		t.Fatalf("a source step is aimed at the folded target, got %q", slots[0].Values)
	}
	for _, slot := range slots[1:] {
		if len(slot.Values) != 0 {
			t.Fatalf("a downstream step has no candidates at plan time, got %q", slot.Values)
		}
	}
}

// 0039 §4: a planned downstream argv holds the SPLIT, UNSUBSTITUTED template,
// so a step that has not run is visibly unresolved and the argv is never empty.
func TestADownstreamStepIsPlannedWithItsTemplateUnsubstituted(t *testing.T) {
	slots, err := domain.Outline([]domain.Step{
		{StepID: nonZero(1), ToolID: nonZero(10), Template: "subfinder -d {{target}}", Source: true},
		{StepID: nonZero(2), ToolID: nonZero(11), Template: "httpx -json -u {{host}}"},
	}, "acme.test")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"httpx", "-json", "-u", "{{host}}"}
	got := slots[1].Argv
	if len(got) != len(want) {
		t.Fatalf("want %q, got %q", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %q, got %q", want, got)
		}
	}
	if slots[0].Argv[2] != "acme.test" {
		t.Fatalf("a source step IS resolved at plan time, got %q", slots[0].Argv)
	}
}

// 0039 §2: many candidates, one command line. A value is ONE argv element
// whatever is in it — the safety property of the single-value case, generalised.
func TestManyValuesEachBecomeTheirOwnArgvElement(t *testing.T) {
	got, err := domain.Argv("httpx -json -u {{host}}",
		"a.acme.test", "b; rm -rf /", "c.acme.test")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"httpx", "-json", "-u", "a.acme.test", "b; rm -rf /", "c.acme.test"}
	if len(got) != len(want) {
		t.Fatalf("want %q, got %q", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %q, got %q", want, got)
		}
	}
	// A field with no placeholder is carried through ONCE however many values
	// there are. Repeating the flags would be a different command.
	if got[0] != "httpx" || got[1] != "-json" || got[2] != "-u" {
		t.Fatalf("the fixed fields were repeated: %q", got)
	}
}

// 0039 §4: Resolve overwrites the planned template with what ran, and REFUSES
// an empty argv rather than clearing it. Nothing can reach it with one today —
// the only caller passes a non-empty Fill of a non-empty template — so this is
// the guard tested directly rather than left as a line nothing can fire.
func TestResolvingWithNoArgvIsRefusedRatherThanClearingTheTemplate(t *testing.T) {
	planned, err := domain.Plan(nonZero(1), nonZero(2), nonZero(3), nonZero(4), nonZero(5),
		0, []string{"httpx", "-u", "{{host}}"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := planned.Resolve(nil); !errors.Is(err, domain.ErrArgvEmpty) {
		t.Fatalf("want ErrArgvEmpty, got %v", err)
	}
	if _, err := planned.Resolve([]string{}); !errors.Is(err, domain.ErrArgvEmpty) {
		t.Fatalf("want ErrArgvEmpty for an empty slice, got %v", err)
	}
	got, err := planned.Resolve([]string{"httpx", "-u", "a.acme.test"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Argv[2] != "a.acme.test" {
		t.Fatalf("want the resolved argv, got %q", got.Argv)
	}
	if planned.Argv[2] != "{{host}}" {
		t.Fatal("Resolve mutated the receiver — every other constructor copies")
	}
}

// Fill is the same walk on already-split fields, so the planned argv and the
// run one cannot be built by two different pieces of code — 0039 §4.
func TestFillResolvesAPlannedTemplateIntoWhatRan(t *testing.T) {
	planned, err := domain.Argv("httpx -u {{host}}")
	if err != nil {
		t.Fatal(err)
	}
	if planned[2] != "{{host}}" {
		t.Fatalf("no values means no substitution, got %q", planned)
	}
	got := domain.Fill(planned, []string{"a.acme.test", "b.acme.test"})
	want := []string{"httpx", "-u", "a.acme.test", "b.acme.test"}
	if len(got) != len(want) {
		t.Fatalf("want %q, got %q", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %q, got %q", want, got)
		}
	}
}

func TestOutlineRefusesAnEmptyChainAndAnEmptyTarget(t *testing.T) {
	if _, err := domain.Outline(nil, "acme.test"); !errors.Is(err, domain.ErrChainEmpty) {
		t.Fatalf("want ErrChainEmpty, got %v", err)
	}
	_, err := domain.Outline([]domain.Step{{
		StepID: nonZero(1), ToolID: nonZero(10), Template: "t {{target}}", Source: true,
	}}, "  ")
	if !errors.Is(err, domain.ErrTargetRequired) {
		t.Fatalf("want ErrTargetRequired, got %v", err)
	}
}

// 0033 §2: exit ABSENT means no process ever existed. A refusal that carried one
// would make `refused` and `failed` the same row.
func TestARefusalHasNoExitCode(t *testing.T) {
	planned, err := domain.Plan(nonZero(1), nonZero(2), nonZero(3), nonZero(4), nonZero(5),
		0, []string{"nmap", "acme.test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	refused, err := planned.Refuse(nonZero(9), "rule r3 excludes acme.test", at)
	if err != nil {
		t.Fatal(err)
	}
	if refused.HasExitCode {
		t.Fatal("a refused invocation never had a process")
	}
	if refused.Phase.HadProcess() {
		t.Fatal("refused is not a phase that had a process")
	}
	if err := refused.Valid(); err != nil {
		t.Fatal(err)
	}
	if len(refused.Argv) != 2 {
		t.Fatal("a refusal keeps the argv it would have run — that is what a person reviews")
	}
}

// The reset in Refuse is unreachable on today's call path — a planned
// invocation has no exit code to clear — so a mutant that removes it survives
// every other test here. INTRODUCING THE CONDITION is what makes it a guard
// rather than dead code: refuse something that already ran, and the exit code
// must not survive into a row whose phase says no process existed.
func TestRefusingSomethingThatRanClearsItsExitCode(t *testing.T) {
	planned, _ := domain.Plan(nonZero(1), nonZero(2), nonZero(3), nonZero(4), nonZero(5),
		0, []string{"nmap"}, nil)
	running, _ := planned.Start(at)
	ended, err := running.Ended(nil, "/usr/bin/nmap", 7, false, "", at, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !ended.HasExitCode {
		t.Fatal("precondition: it ran and exited")
	}

	refused, err := ended.Refuse(nonZero(9), "r3", at)
	if err != nil {
		t.Fatal(err)
	}
	if refused.HasExitCode || refused.ExitCode != 0 {
		t.Fatalf("a refused row claims no process existed, so it carries no exit code: %+v", refused)
	}
	if err := refused.Valid(); err != nil {
		t.Fatalf("the invariant the schema also holds: %v", err)
	}
}

// A refusal with no rule is "nothing permitted it", which is a different fact
// from "a rule excluded it" — 0010's default.
func TestNotInScopeIsARefusalWithNoRule(t *testing.T) {
	planned, _ := domain.Plan(nonZero(1), nonZero(2), nonZero(3), nonZero(4), nonZero(5),
		0, []string{"nmap"}, nil)
	out, err := planned.NotInScope("no rule permits a host here", at)
	if err != nil {
		t.Fatal(err)
	}
	if out.Phase != domain.PhaseRefused {
		t.Fatal("nothing in scope is a refusal")
	}
	if !out.RefusalRule.IsZero() {
		t.Fatal("no rule decided it, so no rule is cited")
	}
	if _, err := planned.Refuse(id.ID{}, "x", at); !errors.Is(err, domain.ErrRuleRequired) {
		t.Fatal("Refuse names a rule; NotInScope is the one that does not")
	}
}

// The tool decides, not this package: nuclei exits 1 when it finds nothing.
func TestSuccessIsTheToolsAnswerNotTheExitCode(t *testing.T) {
	planned, _ := domain.Plan(nonZero(1), nonZero(2), nonZero(3), nonZero(4), nonZero(5),
		0, []string{"nuclei"}, nil)
	running, err := planned.Start(at)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := running.Ended([]string{"nuclei", "-u", "x"}, "/usr/bin/nuclei", 1, true, "", at, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if ok.Phase != domain.PhaseOK {
		t.Fatalf("exit 1 that the tool calls success is ok, got %v", ok.Phase)
	}
	if !ok.HasExitCode || ok.ExitCode != 1 {
		t.Fatal("the exit code is recorded whatever it means")
	}

	bad, err := running.Ended(nil, "/usr/bin/nuclei", 1, false, "", at, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if bad.Phase != domain.PhaseFailed {
		t.Fatal("the same exit code is failed when the tool does not call it success")
	}
}

// Off PATH is `failed` with NO exit code — the one case where those travel
// together. CLAUDE.md's `health` noun exists because it otherwise looks like
// silence.
func TestAToolThatCouldNotStartIsFailedWithNoExitCode(t *testing.T) {
	planned, _ := domain.Plan(nonZero(1), nonZero(2), nonZero(3), nonZero(4), nonZero(5),
		0, []string{"nope"}, nil)
	running, _ := planned.Start(at)
	broke, err := running.Broke("executable file not found in $PATH", "", at, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if broke.Phase != domain.PhaseFailed {
		t.Fatal("a tool off PATH is a failure, not silence")
	}
	if broke.HasExitCode {
		t.Fatal("nothing exited, so there is no exit code")
	}
	if broke.Unavailable == "" {
		t.Fatal("the reason is the whole value of the record")
	}
}

func TestAPendingInvocationIsTheOnlyOneThatCanStart(t *testing.T) {
	planned, _ := domain.Plan(nonZero(1), nonZero(2), nonZero(3), nonZero(4), nonZero(5),
		0, []string{"x"}, nil)
	running, err := planned.Start(at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := running.Start(at); !errors.Is(err, domain.ErrNotRunnable) {
		t.Fatalf("want ErrNotRunnable, got %v", err)
	}
}

func TestASkipNamesWhatDidNotArrive(t *testing.T) {
	planned, _ := domain.Plan(nonZero(1), nonZero(2), nonZero(3), nonZero(4), nonZero(5),
		0, []string{"x"}, nil)
	if _, err := planned.Skip("", at); !errors.Is(err, domain.ErrReasonRequired) {
		t.Fatalf("want ErrReasonRequired, got %v", err)
	}
	skipped, err := planned.Skip(domain.SkipNothingUpstream, at)
	if err != nil {
		t.Fatal(err)
	}
	if skipped.HasExitCode {
		t.Fatal("nobody ran it, so there is no exit code")
	}
}

// Zero bytes is a result; no artifact row is nothing written.
func TestAnEmptyArtifactIsAResult(t *testing.T) {
	a, err := domain.NewArtifact(nonZero(1), nonZero(2), nonZero(3),
		domain.StreamStdout, "sha256:abc", 0, false, "application/json", at)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Empty() {
		t.Fatal("zero bytes is an empty artifact")
	}
	if _, err := domain.NewArtifact(nonZero(1), nonZero(2), nonZero(3),
		domain.StreamStdout, "", 0, false, "", at); !errors.Is(err, domain.ErrHashRequired) {
		t.Fatal("an artifact is named by the hash of its bytes")
	}
}

func TestEveryPhaseAndStateRoundTripsThroughItsName(t *testing.T) {
	for _, p := range []domain.Phase{
		domain.PhasePending, domain.PhaseRunning, domain.PhaseOK,
		domain.PhaseFailed, domain.PhaseRefused, domain.PhaseSkipped,
	} {
		back, err := domain.ParsePhase(p.String())
		if err != nil || back != p {
			t.Fatalf("%q did not round-trip: %v %v", p.String(), back, err)
		}
	}
	for _, s := range []domain.State{domain.StateRunning, domain.StateComplete, domain.StateStopped} {
		back, err := domain.ParseState(s.String())
		if err != nil || back != s {
			t.Fatalf("%q did not round-trip", s.String())
		}
	}
}

// A run that was wholly refused is COMPLETE — it is a complete answer to "may we
// look at this", and calling it stopped would make the scope proof a failure.
func TestARunFinishesOnceAndOnlyOnce(t *testing.T) {
	r, err := domain.New(nonZero(1), nonZero(2), nonZero(3), nonZero(4), nonZero(5), at)
	if err != nil {
		t.Fatal(err)
	}
	done, err := r.Finish(domain.StateComplete, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := done.Finish(domain.StateStopped, at); !errors.Is(err, domain.ErrFinished) {
		t.Fatalf("want ErrFinished, got %v", err)
	}
	if _, err := r.Finish(domain.StateRunning, at); !errors.Is(err, domain.ErrStateUnknown) {
		t.Fatal("finishing into `running` is not finishing")
	}
}

// decisions/0039: a candidate's value is FOLDED, matching `entity.fragment.value`
// and `observation.subject_value`. 0037 named the fold mismatch as its own quiet
// failure and this is the join it was about — unfolded, every coverage cell
// reads `never` and every row is otherwise correct.
func TestACandidateFoldsItsValue(t *testing.T) {
	got, err := domain.NewCandidate(nonZero(1), nonZero(2), nonZero(3), nonZero(4),
		"host", "  ACME.Test  ", at)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "host" || got.Value != "acme.test" {
		t.Fatalf("want host/acme.test, got %s/%q", got.Kind, got.Value)
	}
}

// A candidate is a kind AND a value. Neither half alone is something the gate
// can be asked about, and a half-set one would fail closed silently rather than
// naming the planner's bug.
func TestACandidateIsBothHalvesOrNothing(t *testing.T) {
	for _, tc := range []struct{ kind, value string }{
		{"", "acme.test"},
		{"host", ""},
		{"host", "   "},
		{"", ""},
	} {
		_, err := domain.NewCandidate(nonZero(1), nonZero(2), nonZero(3), nonZero(4),
			tc.kind, tc.value, at)
		if !errors.Is(err, domain.ErrCandidateEmpty) {
			t.Fatalf("%q/%q: want ErrCandidateEmpty, got %v", tc.kind, tc.value, err)
		}
	}
}

// A candidate is born NEITHER permitted nor refused, so one the gate was never
// asked about cannot read as permitted. decisions/0010 — nothing is in scope
// until a rule says so — is a default, and this is that default in a struct.
func TestAnUnaskedCandidateIsNotPermitted(t *testing.T) {
	got, err := domain.NewCandidate(nonZero(1), nonZero(2), nonZero(3), nonZero(4),
		"host", "acme.test", at)
	if err != nil {
		t.Fatal(err)
	}
	if got.Permitted {
		t.Fatal("nobody asked the gate, so nothing permitted it")
	}
	if len(domain.PermittedValues([]domain.Candidate{got})) != 0 {
		t.Fatal("an unasked candidate must never reach an argv")
	}
}

// 0010 keeps them apart: a rule EXCLUDED this, versus NOTHING permitted it. The
// second cites no rule, and a report that cited one would be citing something
// nobody wrote.
func TestARefusalWithNoRuleIsADifferentFactFromAnExclusion(t *testing.T) {
	base, err := domain.NewCandidate(nonZero(1), nonZero(2), nonZero(3), nonZero(4),
		"host", "acme.test", at)
	if err != nil {
		t.Fatal(err)
	}
	excluded, err := base.Refuse(nonZero(9), "rule *.acme.test excludes it")
	if err != nil {
		t.Fatal(err)
	}
	if excluded.RefusalRule != nonZero(9) || excluded.Permitted {
		t.Fatalf("an exclusion cites its rule: %+v", excluded)
	}
	unmatched, err := base.NotInScope("nothing in this target's scope permits it")
	if err != nil {
		t.Fatal(err)
	}
	if !unmatched.RefusalRule.IsZero() || unmatched.Permitted {
		t.Fatalf("nothing permitted it, so there is no rule to cite: %+v", unmatched)
	}
	if unmatched.RefusalReason == "" {
		t.Fatal("a refusal always says why, rule or no rule")
	}
	if _, err := base.Refuse(id.ID{}, "why"); !errors.Is(err, domain.ErrRuleRequired) {
		t.Fatalf("want ErrRuleRequired, got %v", err)
	}
	if _, err := base.NotInScope("  "); !errors.Is(err, domain.ErrReasonRequired) {
		t.Fatalf("want ErrReasonRequired, got %v", err)
	}
}

// Permitting after a refusal must leave NO trace of the refusal, or a row reads
// as both permitted and excluded — which is the invariant the schema also holds.
func TestPermittingClearsARefusal(t *testing.T) {
	base, _ := domain.NewCandidate(nonZero(1), nonZero(2), nonZero(3), nonZero(4),
		"host", "acme.test", at)
	refused, _ := base.Refuse(nonZero(9), "no")
	got := refused.Permit()
	if !got.Permitted || !got.RefusalRule.IsZero() || got.RefusalReason != "" {
		t.Fatalf("a permitted candidate carries no refusal: %+v", got)
	}
}

// 0039 §2: the argv carries the PERMITTED subset, in order, and a refused
// candidate is absent from it. That absence is the whole point of the table.
func TestOnlyPermittedCandidatesReachTheArgv(t *testing.T) {
	mk := func(n byte, value string, permitted bool) domain.Candidate {
		c, err := domain.NewCandidate(nonZero(n), nonZero(2), nonZero(3), nonZero(4),
			"host", value, at)
		if err != nil {
			t.Fatal(err)
		}
		if permitted {
			return c.Permit()
		}
		refused, err := c.Refuse(nonZero(9), "excluded")
		if err != nil {
			t.Fatal(err)
		}
		return refused
	}
	cs := []domain.Candidate{
		mk(1, "a.acme.test", true),
		mk(2, "b.acme.test", false),
		mk(3, "c.acme.test", true),
	}
	got := domain.PermittedValues(cs)
	if len(got) != 2 || got[0] != "a.acme.test" || got[1] != "c.acme.test" {
		t.Fatalf("want the two permitted values in order, got %q", got)
	}
	first, ok := domain.FirstRefusal(cs)
	if !ok || first.Value != "b.acme.test" {
		t.Fatalf("want the first refusal, got %+v (%v)", first, ok)
	}
	if _, ok := domain.FirstRefusal(cs[:1]); ok {
		t.Fatal("nothing was refused, so there is no refusal to cite")
	}
}

// 0039 §3: several feeders UNION, and the result is folded and ordered so an
// argv is a function of what was FOUND rather than of the order two feeders
// happened to finish in.
func TestFeederSubjectsUnionFoldAndOrder(t *testing.T) {
	got := domain.DistinctValues([]string{
		"B.acme.test", "a.acme.test", "  b.acme.test  ", "", "   ", "a.acme.test",
	})
	want := []string{"a.acme.test", "b.acme.test"}
	if len(got) != len(want) {
		t.Fatalf("want %q, got %q", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %q, got %q", want, got)
		}
	}
}
