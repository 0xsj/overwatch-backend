// Author-written, from decisions/0041's Verification block and 0004's Decision
// section, both written before this code.
package domain_test

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/finding/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var (
	monday = time.Date(2026, 9, 8, 2, 0, 0, 0, time.UTC)
	friday = time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC)
)

func an(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}

func opened(t *testing.T) domain.Finding {
	t.Helper()
	f, err := domain.New(an(1), an(2), an(3), an(4), "CVE-2021-44228",
		"url", "https://a.acme.test/", domain.SeverityCritical, "reported by nuclei",
		an(5), an(6), an(7), monday, monday)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// **THE test of this record.** A rescan is a SIGHTING. A human's ruling is about
// the problem, not about the run that noticed it, and a rescan that reset
// `triaged` to `open` would make triage impossible to keep.
func TestARescanNeverUndoesARuling(t *testing.T) {
	triaged, err := opened(t).Triage(an(9), monday)
	if err != nil {
		t.Fatal(err)
	}
	again, err := triaged.Seen(an(10), an(11), an(12), friday)
	if err != nil {
		t.Fatal(err)
	}
	if again.State != domain.StateTriaged {
		t.Fatalf("a rescan must not reopen a triaged finding, got %s", again.State)
	}
	if again.Sightings != 2 {
		t.Fatalf("two sightings are two sightings, got %d", again.Sightings)
	}
	if !again.LastSeen.Equal(friday) {
		t.Fatalf("last_seen moves: %v", again.LastSeen)
	}
	if !again.FirstSeen.Equal(monday) {
		t.Fatalf("first_seen NEVER moves forward: %v", again.FirstSeen)
	}
	// The citation follows the NEWEST evidence.
	if again.Invocation != an(10) || again.Artifact != an(11) {
		t.Fatalf("the citation follows the newest run: %+v", again)
	}
}

// A re-extraction of an OLDER artifact must not make a stale run look like
// tonight's, and it legitimately moves `first_seen` back.
func TestARescanOfAnOlderArtifactMovesFirstSeenAndNotTheCitation(t *testing.T) {
	f := opened(t)
	earlier := monday.Add(-72 * time.Hour)
	again, err := f.Seen(an(10), an(11), an(12), earlier)
	if err != nil {
		t.Fatal(err)
	}
	if !again.FirstSeen.Equal(earlier) {
		t.Fatalf("an older sighting moves first_seen back: %v", again.FirstSeen)
	}
	if !again.LastSeen.Equal(monday) {
		t.Fatalf("but never last_seen: %v", again.LastSeen)
	}
	if again.Invocation != an(5) {
		t.Fatal("a stale run must not become the citation")
	}
}

// A rescan does not re-assert severity either: the tool says `critical` again
// tonight, and overwriting a human's override with the template's opinion every
// night is the same failure one field over.
func TestARescanDoesNotReassertSeverity(t *testing.T) {
	lowered, err := opened(t).Reassess(domain.SeverityLow, domain.Assessment{
		Claimant: domain.ByHuman, Actor: an(9),
		Basis: "the endpoint is behind auth", At: monday,
	})
	if err != nil {
		t.Fatal(err)
	}
	again, err := lowered.Seen(an(10), an(11), an(12), friday)
	if err != nil {
		t.Fatal(err)
	}
	if again.Severity != domain.SeverityLow {
		t.Fatalf("a rescan must not overwrite a human override: %s", again.Severity)
	}
	if again.Assessment.Claimant != domain.ByHuman {
		t.Fatalf("nor its claimant: %s", again.Assessment.Claimant)
	}
}

// 0041's accepted cost, pinned so it is a documented limit rather than a
// surprise: without a `regressed` state a fix that did not hold returns to
// `open`. The evidence a reader has is an old first_seen beside a large
// sightings count.
func TestAResolvedFindingSeenAgainReopensAndKeepsItsHistory(t *testing.T) {
	fixed, err := opened(t).Resolve(an(9), "", monday)
	if err != nil {
		t.Fatal(err)
	}
	again, err := fixed.Seen(an(10), an(11), an(12), friday)
	if err != nil {
		t.Fatal(err)
	}
	if again.State != domain.StateOpen {
		t.Fatalf("a fix that did not hold is open again, got %s", again.State)
	}
	if !again.DecidedAt.IsZero() || !again.DecidedBy.IsZero() || again.Reason != "" {
		t.Fatalf("the old ruling is cleared, not kept as a lie: %+v", again)
	}
	if !again.FirstSeen.Equal(monday) || again.Sightings != 2 {
		t.Fatalf("and the history survives: %v / %d", again.FirstSeen, again.Sightings)
	}
}

// 0041 §3. The pair that must never collapse: one is a change to the world, the
// other is a change of mind, and a client report cites them differently.
func TestDismissingNeedsAReasonAndResolvingDoesNot(t *testing.T) {
	if _, err := opened(t).Dismiss(an(9), "   ", monday); !errors.Is(err, domain.ErrReasonRequired) {
		t.Fatalf("want ErrReasonRequired, got %v", err)
	}
	dismissed, err := opened(t).Dismiss(an(9), "the host is a honeypot", monday)
	if err != nil {
		t.Fatal(err)
	}
	if dismissed.State != domain.StateDismissed || dismissed.Reason == "" {
		t.Fatalf("a dismissal keeps its reason: %+v", dismissed)
	}
	// A FIX NEEDS NO ARGUMENT. The thing is gone.
	fixed, err := opened(t).Resolve(an(9), "", monday)
	if err != nil {
		t.Fatalf("resolving must not require a reason: %v", err)
	}
	if fixed.State != domain.StateResolved {
		t.Fatalf("state: %s", fixed.State)
	}
}

// Nothing closes a finding automatically — 0041. There is no system path, so
// every transition names a person.
func TestEveryRulingNamesAPerson(t *testing.T) {
	f := opened(t)
	for _, tc := range []struct {
		name string
		call func() (domain.Finding, error)
	}{
		{"triage", func() (domain.Finding, error) { return f.Triage(id.ID{}, monday) }},
		{"resolve", func() (domain.Finding, error) { return f.Resolve(id.ID{}, "", monday) }},
		{"dismiss", func() (domain.Finding, error) { return f.Dismiss(id.ID{}, "why", monday) }},
	} {
		if _, err := tc.call(); !errors.Is(err, domain.ErrActorRequired) {
			t.Fatalf("%s: want ErrActorRequired, got %v", tc.name, err)
		}
	}
}

// 0004: a rule's assessment is a CATEGORY, not a probability. Storing 1.0
// destroys the distinction permanently.
func TestOnlyAModelCarriesConfidence(t *testing.T) {
	for _, tc := range []struct {
		name string
		a    domain.Assessment
		want error
	}{
		{"a rule with confidence", domain.Assessment{
			Claimant: domain.ByRule, Confidence: 1, HasConfidence: true}, domain.ErrConfidenceOnRule},
		{"a human with confidence", domain.Assessment{
			Claimant: domain.ByHuman, Actor: an(9), Confidence: 0.9, HasConfidence: true}, domain.ErrConfidenceOnHuman},
		{"a model without", domain.Assessment{
			Claimant: domain.ByModel}, domain.ErrConfidenceMissing},
		{"a model out of range", domain.Assessment{
			Claimant: domain.ByModel, Confidence: 1.4, HasConfidence: true}, domain.ErrConfidenceRange},
		{"a human with no actor", domain.Assessment{
			Claimant: domain.ByHuman}, domain.ErrActorRequired},
	} {
		if err := tc.a.Valid(false); !errors.Is(err, tc.want) {
			t.Fatalf("%s: want %v, got %v", tc.name, tc.want, err)
		}
	}
	// And the tool's own assessment, which is what extraction writes, is valid.
	if err := (domain.Assessment{Claimant: domain.ByRule, At: monday}).Valid(false); err != nil {
		t.Fatalf("a rule assessment with no confidence is the normal case: %v", err)
	}
}

// 0004: an override NEVER deletes what it overrode, and it must say why.
func TestAnOverrideKeepsWhatItOverrodeAndSaysWhy(t *testing.T) {
	f := opened(t)
	if _, err := f.Reassess(domain.SeverityLow, domain.Assessment{
		Claimant: domain.ByHuman, Actor: an(9), At: monday,
	}); !errors.Is(err, domain.ErrBasisRequired) {
		t.Fatalf("want ErrBasisRequired, got %v", err)
	}
	got, err := f.Reassess(domain.SeverityLow, domain.Assessment{
		Claimant: domain.ByHuman, Actor: an(9),
		Basis: "behind auth", At: monday,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Severity != domain.SeverityLow {
		t.Fatalf("severity: %s", got.Severity)
	}
	if got.Superseded == nil {
		t.Fatal("an override never deletes what it overrode")
	}
	if got.SupersededSeverity != domain.SeverityCritical {
		t.Fatalf("the prior SEVERITY is kept too: %s", got.SupersededSeverity)
	}
	if got.Superseded.Claimant != domain.ByRule {
		t.Fatalf("and the prior claimant: %s", got.Superseded.Claimant)
	}
}

// A finding this system cannot source would arrive looking trustworthy — 0003's
// rule one noun over.
func TestAFindingCannotBeBuiltWithoutItsSource(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		invocation, artifact, mapping id.ID
	}{
		{"no invocation", id.ID{}, an(6), an(7)},
		{"no artifact", an(5), id.ID{}, an(7)},
		{"no mapping", an(5), an(6), id.ID{}},
	} {
		_, err := domain.New(an(1), an(2), an(3), an(4), "sig", "url", "u",
			domain.SeverityHigh, "", tc.invocation, tc.artifact, tc.mapping, monday, monday)
		if !errors.Is(err, domain.ErrUnsourced) {
			t.Fatalf("%s: want ErrUnsourced, got %v", tc.name, err)
		}
	}
}

// Half an identity is not a finding — 0041 §1.
func TestAFindingNamesWhatTheToolCalledTheProblem(t *testing.T) {
	_, err := domain.New(an(1), an(2), an(3), an(4), "   ", "url", "u",
		domain.SeverityHigh, "", an(5), an(6), an(7), monday, monday)
	if !errors.Is(err, domain.ErrSignatureRequired) {
		t.Fatalf("want ErrSignatureRequired, got %v", err)
	}
}

// An unrecognised severity is an ERROR and never a default. Quietly filing an
// unknown word as `info` would hide the loudest thing a scanner said.
func TestAnUnknownSeverityIsRefusedRatherThanDowngraded(t *testing.T) {
	for _, in := range []string{"CRITICAL", " High ", "medium"} {
		if _, err := domain.ParseSeverity(in); err != nil {
			t.Fatalf("%q is a severity tools really write: %v", in, err)
		}
	}
	got, err := domain.ParseSeverity("catastrophic")
	if !errors.Is(err, domain.ErrSeverityUnknown) {
		t.Fatalf("want ErrSeverityUnknown, got %v", err)
	}
	if got != domain.SeverityInfo {
		t.Fatalf("the zero value is info, and the CALLER must not use it: %s", got)
	}
}

// 0009's badge counts the live ones, and it is one function so the badge and the
// board cannot disagree about what "live" means.
func TestOnlyOpenAndTriagedCountAgainstTheEstate(t *testing.T) {
	for state, want := range map[domain.State]bool{
		domain.StateOpen: true, domain.StateTriaged: true,
		domain.StateResolved: false, domain.StateDismissed: false,
	} {
		if state.Live() != want {
			t.Fatalf("%s.Live() = %v, want %v", state, state.Live(), want)
		}
	}
}

// The order lives in SQL because the board sorts on it; this is the one place it
// is read back, so the two cannot drift about which end is worst.
func TestSeverityOrderRoundTrips(t *testing.T) {
	for order, want := range map[int]domain.Severity{
		0: domain.SeverityCritical, 1: domain.SeverityHigh, 2: domain.SeverityMedium,
		3: domain.SeverityLow, 4: domain.SeverityInfo,
	} {
		if got := domain.SeverityAt(order); got != want {
			t.Fatalf("SeverityAt(%d) = %s, want %s", order, got, want)
		}
	}
}

func TestMovingToTheStateItIsAlreadyInIsRefused(t *testing.T) {
	triaged, err := opened(t).Triage(an(9), monday)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := triaged.Triage(an(9), friday); !errors.Is(err, domain.ErrAlreadyThere) {
		t.Fatalf("want ErrAlreadyThere, got %v", err)
	}
}
