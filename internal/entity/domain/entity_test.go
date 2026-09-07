// Author-written, from decisions/0009's Verification block — which was written
// on 2026-09-06, before this package existed, and is as close to an independent
// oracle as domain code in this tree gets — plus 0036's.
package domain_test

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/entity/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var at = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

func nonZero(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}

// ---- 0009's judgement checklist, line by line -----------------------------

func TestAJudgementStateOutsideTheFourIsRefused(t *testing.T) {
	if _, err := domain.ParseJudgement("finding"); !errors.Is(err, domain.ErrStateUnknown) {
		t.Fatal("a finding is NOT a judgement — 0009")
	}
	for _, name := range []string{"unopened", "triaged", "watching", "dismissed"} {
		if _, err := domain.ParseJudgement(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	// An empty column is `unopened`: a row nobody has ruled on is exactly that.
	if got, err := domain.ParseJudgement(""); err != nil || got != domain.Unopened {
		t.Errorf(`"" should be unopened, got %v %v`, got, err)
	}
}

func TestDismissingNeedsAReason(t *testing.T) {
	for _, reason := range []string{"", "   "} {
		_, err := domain.Rule(domain.Dismissed, nonZero(1), reason, at)
		if !errors.Is(err, domain.ErrDismissalReason) {
			t.Fatalf("a dismissal with no reason reads as `never looked at` in six months: %v", err)
		}
	}
	if _, err := domain.Rule(domain.Dismissed, nonZero(1), "third-party docs host", at); err != nil {
		t.Fatal(err)
	}
}

// "an initial triage with an absent reason IS accepted — the asymmetry with
// dismissal is the rule, so a test asserting the stricter form would be wrong"
func TestTriageDoesNotNeedAReasonAndThatAsymmetryIsTheRule(t *testing.T) {
	for _, state := range []domain.JudgementState{domain.Triaged, domain.Watching} {
		if _, err := domain.Rule(state, nonZero(1), "", at); err != nil {
			t.Errorf("%v with no reason must be accepted: %v", state, err)
		}
	}
}

func TestUnopenedNamesNobody(t *testing.T) {
	if _, err := domain.Rule(domain.Unopened, nonZero(1), "", at); !errors.Is(err, domain.ErrJudgeOnUnopened) {
		t.Fatalf("somebody ruling on it is what stops it being unopened: %v", err)
	}
	got, err := domain.Rule(domain.Unopened, id.ID{}, "", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Opened() {
		t.Fatal("unopened is not opened")
	}
}

func TestARuledJudgementNamesWhoAndWhen(t *testing.T) {
	if _, err := domain.Rule(domain.Triaged, id.ID{}, "", at); !errors.Is(err, domain.ErrJudgeRequired) {
		t.Error("triaged with no `by` is refused")
	}
	if _, err := domain.Rule(domain.Triaged, nonZero(1), "", time.Time{}); !errors.Is(err, domain.ErrJudgeRequired) {
		t.Error("a judgement with `by` set and `at` absent is refused")
	}
}

// Valid restates the same rules over a LOADED row, so a store and a constraint
// cannot disagree. A row assembled field-by-field bypasses the constructor.
func TestValidRefusesTheSameShapesTheConstructorDoes(t *testing.T) {
	for _, tc := range []struct {
		name string
		j    domain.Judgement
		want error
	}{
		{"unopened with a decider", domain.Judgement{State: domain.Unopened, By: nonZero(1)}, domain.ErrJudgeOnUnopened},
		{"triaged with none", domain.Judgement{State: domain.Triaged}, domain.ErrJudgeRequired},
		{"dismissed with no reason", domain.Judgement{State: domain.Dismissed, By: nonZero(1), At: at}, domain.ErrDismissalReason},
	} {
		if err := tc.j.Valid(); !errors.Is(err, tc.want) {
			t.Errorf("%s: want %v, got %v", tc.name, tc.want, err)
		}
	}
}

// ---- 0003 and 0008, as amended by 0036 -----------------------------------

func propose(t *testing.T, claimant domain.Claimant, ref id.ID, conf float64, has bool) (domain.Attribution, error) {
	t.Helper()
	return domain.Propose(nonZero(1), nonZero(2), nonZero(3), nonZero(4),
		claimant, ref, conf, has, "TLS SAN shares *.acme.test", at)
}

// "a rule's assignment is a category, not a probability, and storing 1.0
// destroys the distinction permanently" — 0003.
func TestOnlyAModelCarriesConfidence(t *testing.T) {
	for _, claimant := range []domain.Claimant{domain.ByRule, domain.ByHuman} {
		if _, err := propose(t, claimant, nonZero(9), 1.0, true); !errors.Is(err, domain.ErrConfidenceOnRule) {
			t.Errorf("%v with a confidence must be refused: %v", claimant, err)
		}
		if _, err := propose(t, claimant, nonZero(9), 0, false); err != nil {
			t.Errorf("%v with none: %v", claimant, err)
		}
	}
	if _, err := propose(t, domain.ByModel, id.ID{}, 0, false); !errors.Is(err, domain.ErrConfidenceMissing) {
		t.Error("a model's claim carries a confidence")
	}
	if _, err := propose(t, domain.ByModel, id.ID{}, 1.7, true); !errors.Is(err, domain.ErrConfidenceRange) {
		t.Error("a confidence is between 0 and 1")
	}
}

// 0036 §3: human and rule are born accepted; only a model proposes.
func TestOnlyAModelProposes(t *testing.T) {
	for _, tc := range []struct {
		claimant  domain.Claimant
		wantState domain.ClaimState
		wantBy    bool
	}{
		{domain.ByRule, domain.Accepted, false},
		{domain.ByHuman, domain.Accepted, true},
		{domain.ByModel, domain.Proposed, false},
	} {
		t.Run(tc.claimant.String(), func(t *testing.T) {
			got, err := propose(t, tc.claimant, nonZero(9),
				0.7, tc.claimant == domain.ByModel)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != tc.wantState {
				t.Fatalf("state %v, want %v", got.State, tc.wantState)
			}
			if tc.wantState == domain.Proposed {
				if !got.DecidedAt.IsZero() || !got.DecidedBy.IsZero() {
					t.Fatal("an undecided attribution names nobody and no time")
				}
				return
			}
			if got.DecidedAt.IsZero() {
				t.Fatal("a decided attribution says WHEN")
			}
			if got.DecidedBy.IsZero() == tc.wantBy {
				t.Fatalf("decided_by set = %v, want %v", !got.DecidedBy.IsZero(), tc.wantBy)
			}
		})
	}
}

// THE amendment: an accepted attribution by a RULE has no decider, and that is
// legal. 0008's invariant said decided_by and decided_at move together.
func TestARuleDecidesWithoutAPerson(t *testing.T) {
	got, err := propose(t, domain.ByRule, nonZero(9), 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Attributed() {
		t.Fatal("a rule's attribution is accepted — otherwise nothing is ever an asset")
	}
	if !got.DecidedBy.IsZero() {
		t.Fatal("a rule has no account behind it")
	}
	if err := got.Valid(); err != nil {
		t.Fatalf("and that shape is valid: %v", err)
	}
	// The rule that decided is still recorded — as the CLAIMANT REF, which is
	// where "on what basis" points.
	if got.ClaimantRef != nonZero(9) {
		t.Fatal("the rule that matched is the claimant ref")
	}
}

// "acceptance never rewrites claimant" — 0008's title.
func TestDecidingNeverRewritesTheClaimant(t *testing.T) {
	proposed, err := propose(t, domain.ByModel, id.ID{}, 0.71, true)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := proposed.Decide(domain.Accepted, nonZero(5), "SAN corroborates", at)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Claimant != domain.ByModel {
		t.Fatal("the claimant is who said it FIRST — a shape that overwrites it retains only the claims the model got wrong")
	}
	if accepted.DecidedBy != nonZero(5) {
		t.Fatal("the decider is recorded separately")
	}
	if !accepted.HasConfidence || accepted.Confidence != 0.71 {
		t.Fatal("the model's confidence survives the ruling")
	}
	if _, err := accepted.Decide(domain.Rejected, nonZero(6), "", at); !errors.Is(err, domain.ErrAlreadyDecided) {
		t.Fatal("ruling twice is refused")
	}
}

func TestABasisIsAlwaysRequired(t *testing.T) {
	_, err := domain.Propose(nonZero(1), nonZero(2), nonZero(3), nonZero(4),
		domain.ByRule, nonZero(9), 0, false, "  ", at)
	if !errors.Is(err, domain.ErrBasisRequired) {
		t.Fatalf("want ErrBasisRequired, got %v", err)
	}
}

func TestAttributionValidRefusesLoadedRowsTheConstructorWouldNot(t *testing.T) {
	for _, tc := range []struct {
		name string
		a    domain.Attribution
		want error
	}{
		{"proposed with a decision", domain.Attribution{
			Claimant: domain.ByModel, HasConfidence: true,
			State: domain.Proposed, DecidedAt: at}, domain.ErrDecidedOnProposed},
		{"accepted with no when", domain.Attribution{
			Claimant: domain.ByRule, State: domain.Accepted}, domain.ErrUndecided},
		{"a rule with a confidence", domain.Attribution{
			Claimant: domain.ByRule, HasConfidence: true,
			State: domain.Accepted, DecidedAt: at}, domain.ErrConfidenceOnRule},
	} {
		if err := tc.a.Valid(); !errors.Is(err, tc.want) {
			t.Errorf("%s: want %v, got %v", tc.name, tc.want, err)
		}
	}
}

// ---- 0036's fragment rules ------------------------------------------------

// 0009's own doubt, resolved: a `/24` typed into a scope rule is a fragment
// nobody observed.
func TestAManualFragmentHasNoSeenDates(t *testing.T) {
	got, err := domain.NewFragment(nonZero(1), nonZero(2), "cidr", "198.51.100.0/24", domain.Manual, at)
	if err != nil {
		t.Fatal(err)
	}
	if !got.FirstSeen.IsZero() || !got.LastSeen.IsZero() {
		t.Fatal("nothing has seen it — rendering a date would be a zero nothing computed")
	}
	if got.Observations != 0 {
		t.Fatal("no observations either")
	}
	observed, err := domain.NewFragment(nonZero(3), nonZero(2), "host", "acme.test", domain.Observed, at)
	if err != nil {
		t.Fatal(err)
	}
	if observed.FirstSeen.IsZero() || observed.Observations != 1 {
		t.Fatal("an observed fragment carries both")
	}
}

// `ACME.test` and `acme.test` are one host; a fragment admitting both would put
// the same asset on the list twice.
func TestAFragmentValueIsFolded(t *testing.T) {
	got, err := domain.NewFragment(nonZero(1), nonZero(2), "host", "  ACME.Test  ", domain.Observed, at)
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "acme.test" {
		t.Fatalf("want acme.test, got %q", got.Value)
	}
}

// LastSeen moves forward and never backward: a re-extraction of an old artifact
// must not make a fragment look stale.
func TestSeenNeverMovesLastSeenBackward(t *testing.T) {
	f, _ := domain.NewFragment(nonZero(1), nonZero(2), "host", "acme.test", domain.Observed, at)
	older := at.Add(-72 * time.Hour)
	got := f.Seen(older, 1)
	if !got.LastSeen.Equal(at) {
		t.Fatalf("last_seen moved backward to %v", got.LastSeen)
	}
	if !got.FirstSeen.Equal(older) {
		t.Fatalf("first_seen should move BACK to the earliest: %v", got.FirstSeen)
	}
	if got.Observations != 2 {
		t.Fatalf("observations: %d", got.Observations)
	}
}

// A manual fragment a tool later finds becomes observed — somebody typed it in
// and then a tool found it, which is a stronger claim than either alone.
func TestAManualFragmentBecomesObservedWhenSomethingSeesIt(t *testing.T) {
	f, _ := domain.NewFragment(nonZero(1), nonZero(2), "cidr", "198.51.100.0/24", domain.Manual, at)
	got := f.Seen(at, 1)
	if got.Origin != domain.Observed {
		t.Fatal("a tool found it")
	}
	if got.FirstSeen.IsZero() {
		t.Fatal("and now it has a first_seen")
	}
}
