package domain_test

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/scope/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var at = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

func nonZero(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}

func rule(t *testing.T, n byte, pattern string, pol domain.Polarity, gate domain.Gate,
	kinds []domain.Kind, tools []domain.Intensity) domain.Rule {
	t.Helper()
	r, err := domain.NewRule(nonZero(n), nonZero(90), nonZero(91), nonZero(92),
		pattern, pol, gate, kinds, tools, at)
	if err != nil {
		t.Fatalf("rule %q: %v", pattern, err)
	}
	return r
}

func host(v string) domain.Candidate {
	return domain.Candidate{Kind: domain.KindHost, Value: v}
}

// ── the checklist decisions/0010 left, in its own order ────────────────────

func TestAClaimRuleCarryingToolsIsRefused(t *testing.T) {
	_, err := domain.NewRule(nonZero(1), nonZero(90), nonZero(91), nonZero(92),
		"acme/api", domain.PolarityInclude, domain.GateClaim,
		[]domain.Kind{domain.KindRepo}, []domain.Intensity{domain.IntensityPassive}, at)
	if !errors.Is(err, domain.ErrToolsOnClaim) {
		t.Errorf("a claim rule with tools was accepted: %v", err)
	}
}

func TestASpawnRuleWithAClaimKindIsRefused(t *testing.T) {
	for _, k := range []domain.Kind{
		domain.KindRepo, domain.KindEmail, domain.KindAccount,
		domain.KindDocument, domain.KindOrg, domain.KindPerson,
	} {
		_, err := domain.NewRule(nonZero(1), nonZero(90), nonZero(91), nonZero(92),
			"x", domain.PolarityInclude, domain.GateSpawn, []domain.Kind{k}, nil, at)
		if !errors.Is(err, domain.ErrKindWrongGate) {
			t.Errorf("spawn accepted kind %s: %v", k, err)
		}
	}
	// And the reverse: a spawn kind on a claim rule.
	for _, k := range []domain.Kind{domain.KindHost, domain.KindCIDR, domain.KindIP} {
		_, err := domain.NewRule(nonZero(1), nonZero(90), nonZero(91), nonZero(92),
			"x", domain.PolarityInclude, domain.GateClaim, []domain.Kind{k}, nil, at)
		if !errors.Is(err, domain.ErrKindWrongGate) {
			t.Errorf("claim accepted kind %s: %v", k, err)
		}
	}
}

// Exclude beats include, on BOTH gates — and the refusal names the loser.
func TestExcludeBeatsIncludeOnBothGates(t *testing.T) {
	spawnKinds := []domain.Kind{domain.KindHost}
	include := rule(t, 1, "*.vertexlabs.example", domain.PolarityInclude, domain.GateSpawn, spawnKinds, nil)
	exclude := rule(t, 3, "vpn.vertexlabs.example", domain.PolarityExclude, domain.GateSpawn, spawnKinds, nil)

	got := domain.Decide([]domain.Rule{include, exclude}, domain.GateSpawn,
		host("vpn.vertexlabs.example"))
	if got.Verdict != domain.Refused {
		t.Fatalf("verdict %s, want refused", got.Verdict)
	}
	if got.Winner.ID != exclude.ID {
		t.Errorf("winner is %v, want the exclude", got.Winner.ID)
	}
	// 0010: "r1 matches this host too; r3 wins." A refusal naming ONE rule
	// where two matched is refused.
	if len(got.Matched) != 2 {
		t.Fatalf("matched %d rules, want 2 — the loser is part of the answer", len(got.Matched))
	}

	// Order must not decide it: the exclude second, then first.
	if got := domain.Decide([]domain.Rule{exclude, include}, domain.GateSpawn,
		host("vpn.vertexlabs.example")); got.Verdict != domain.Refused {
		t.Errorf("order changed the verdict: %s", got.Verdict)
	}

	// The same on the claim gate.
	claimKinds := []domain.Kind{domain.KindRepo}
	ci := rule(t, 4, "acme/api", domain.PolarityInclude, domain.GateClaim, claimKinds, nil)
	ce := rule(t, 5, "acme/api", domain.PolarityExclude, domain.GateClaim, claimKinds, nil)
	got = domain.Decide([]domain.Rule{ci, ce}, domain.GateClaim,
		domain.Candidate{Kind: domain.KindRepo, Value: "acme/api"})
	if got.Verdict != domain.Refused || got.Winner.ID != ce.ID {
		t.Errorf("claim gate: %s, winner %v", got.Verdict, got.Winner.ID)
	}
}

// The default, and decisions/0030's substance: nothing is in scope until a rule
// says so.
func TestNothingIsInScopeWithoutARule(t *testing.T) {
	for _, gate := range []domain.Gate{domain.GateSpawn, domain.GateClaim} {
		got := domain.Decide(nil, gate, host("anything.example"))
		if got.Verdict != domain.NotInScope {
			t.Errorf("%s with no rules: %s, want not_in_scope", gate, got.Verdict)
		}
		if len(got.Matched) != 0 {
			t.Errorf("%s matched %d rules from an empty set", gate, len(got.Matched))
		}
	}
	// A rule that matches nothing leaves it out of scope, rather than refusing.
	r := rule(t, 1, "other.example", domain.PolarityInclude, domain.GateSpawn,
		[]domain.Kind{domain.KindHost}, nil)
	if got := domain.Decide([]domain.Rule{r}, domain.GateSpawn, host("a.example")); got.Verdict != domain.NotInScope {
		t.Errorf("a non-matching rule gave %s", got.Verdict)
	}
}

// Sealed in 0010 because wildcard semantics are agony to change once rules exist.
func TestWildcardDoesNotMatchTheApexAndMatchesAnyDepth(t *testing.T) {
	r := rule(t, 1, "*.example.com", domain.PolarityInclude, domain.GateSpawn,
		[]domain.Kind{domain.KindHost}, nil)
	set := []domain.Rule{r}

	for value, want := range map[string]domain.Verdict{
		"example.com":           domain.NotInScope, // the apex. NEVER silently widened
		"a.example.com":         domain.Permitted,
		"a.b.example.com":       domain.Permitted,
		"notexample.com":        domain.NotInScope, // suffix without the dot
		"example.com.evil.test": domain.NotInScope,
	} {
		if got := domain.Decide(set, domain.GateSpawn, host(value)); got.Verdict != want {
			t.Errorf("%q: %s, want %s", value, got.Verdict, want)
		}
	}
}

func TestAClaimRuleNeverRefusesASpawnQuestion(t *testing.T) {
	// A claim rule is invisible to a spawn evaluation, whatever it says.
	r := rule(t, 1, "acme/api", domain.PolarityExclude, domain.GateClaim,
		[]domain.Kind{domain.KindRepo}, nil)
	for _, c := range []domain.Candidate{
		host("acme/api"),
		{Kind: domain.KindIP, Value: "10.0.0.1"},
		{Kind: domain.KindRepo, Value: "acme/api"},
	} {
		if got := domain.Decide([]domain.Rule{r}, domain.GateSpawn, c); got.Verdict == domain.Refused {
			t.Errorf("a claim rule refused a spawn question about %+v", c)
		}
	}
}

// decisions/0030: a cidr rule matches an ip inside it; an ip rule never matches
// a cidr; and comparison is on the ADDRESS, never the text.
func TestAddressMatching(t *testing.T) {
	cidr := rule(t, 1, "10.0.0.0/8", domain.PolarityInclude, domain.GateSpawn,
		[]domain.Kind{domain.KindIP, domain.KindCIDR}, nil)
	exact := rule(t, 2, "10.0.0.1", domain.PolarityInclude, domain.GateSpawn,
		[]domain.Kind{domain.KindIP, domain.KindCIDR}, nil)

	ip := func(v string) domain.Candidate { return domain.Candidate{Kind: domain.KindIP, Value: v} }
	block := func(v string) domain.Candidate { return domain.Candidate{Kind: domain.KindCIDR, Value: v} }

	if got := domain.Decide([]domain.Rule{cidr}, domain.GateSpawn, ip("10.1.2.3")); got.Verdict != domain.Permitted {
		t.Errorf("a cidr rule did not match an ip inside it: %s", got.Verdict)
	}
	if got := domain.Decide([]domain.Rule{cidr}, domain.GateSpawn, ip("11.1.2.3")); got.Verdict != domain.NotInScope {
		t.Errorf("a cidr rule matched an ip outside it: %s", got.Verdict)
	}
	// A range is in scope only if the rule contains ALL of it.
	if got := domain.Decide([]domain.Rule{cidr}, domain.GateSpawn, block("10.1.0.0/16")); got.Verdict != domain.Permitted {
		t.Errorf("a cidr rule did not contain a narrower range: %s", got.Verdict)
	}
	if got := domain.Decide([]domain.Rule{cidr}, domain.GateSpawn, block("10.0.0.0/4")); got.Verdict != domain.NotInScope {
		t.Errorf("a cidr rule matched a WIDER range: %s", got.Verdict)
	}
	// An ip rule never matches a cidr: "10.0.0.1 is in scope" says nothing
	// about the range containing it.
	if got := domain.Decide([]domain.Rule{exact}, domain.GateSpawn, block("10.0.0.0/8")); got.Verdict != domain.NotInScope {
		t.Errorf("an ip rule matched a cidr: %s", got.Verdict)
	}
	// The address, not the text.
	if got := domain.Decide([]domain.Rule{exact}, domain.GateSpawn, ip("::ffff:10.0.0.1")); got.Verdict != domain.Permitted {
		t.Errorf("an ipv4-mapped address was treated as a different host: %s", got.Verdict)
	}
}

// A range in scope for passive collection is not thereby in scope for a loud
// scan — the reason a spawn rule carries intensities at all.
func TestIntensityQualifiesASpawnRule(t *testing.T) {
	r := rule(t, 1, "*.example.com", domain.PolarityInclude, domain.GateSpawn,
		[]domain.Kind{domain.KindHost},
		[]domain.Intensity{domain.IntensityPassive, domain.IntensityLight})
	set := []domain.Rule{r}

	for i, want := range map[domain.Intensity]domain.Verdict{
		domain.IntensityPassive: domain.Permitted,
		domain.IntensityLight:   domain.Permitted,
		domain.IntensityLoud:    domain.NotInScope,
	} {
		c := domain.Candidate{Kind: domain.KindHost, Value: "a.example.com", Intensity: i}
		if got := domain.Decide(set, domain.GateSpawn, c); got.Verdict != want {
			t.Errorf("%s: %s, want %s", i, got.Verdict, want)
		}
	}
	// An unqualified spawn rule means every intensity, spelled out — an empty
	// list reads as "none" to anything that filters and "all" to anything that
	// does not.
	open := rule(t, 2, "*.example.com", domain.PolarityInclude, domain.GateSpawn,
		[]domain.Kind{domain.KindHost}, nil)
	if len(open.Tools) != 3 {
		t.Errorf("an unqualified spawn rule carries %d intensities, want 3", len(open.Tools))
	}
}

// A superseded rule is not evaluated, and Supersede keeps the id — decisions/0030.
func TestASupersededRuleIsNotEvaluated(t *testing.T) {
	r := rule(t, 1, "*.example.com", domain.PolarityInclude, domain.GateSpawn,
		[]domain.Kind{domain.KindHost}, nil)
	if got := domain.Decide([]domain.Rule{r}, domain.GateSpawn, host("a.example.com")); got.Verdict != domain.Permitted {
		t.Fatalf("live rule: %s", got.Verdict)
	}
	gone, err := r.Supersede(nonZero(93), at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if gone.ID != r.ID {
		t.Error("superseding changed the rule id — three surfaces cite it")
	}
	if got := domain.Decide([]domain.Rule{gone}, domain.GateSpawn, host("a.example.com")); got.Verdict != domain.NotInScope {
		t.Errorf("a superseded rule was evaluated: %s", got.Verdict)
	}
	if _, err := gone.Supersede(nonZero(93), at.Add(2*time.Hour)); !errors.Is(err, domain.ErrSuperseded) {
		t.Errorf("superseding twice: %v", err)
	}
}

func TestARuleThatNamesNoKindsIsRefused(t *testing.T) {
	_, err := domain.NewRule(nonZero(1), nonZero(90), nonZero(91), nonZero(92),
		"x", domain.PolarityInclude, domain.GateSpawn, nil, nil, at)
	if !errors.Is(err, domain.ErrKindsRequired) {
		t.Errorf("a rule matching nothing was accepted: %v", err)
	}
}

// decisions/0034 put `asn` and `url` on the spawn gate. `matchesSpawn` ended in
// `kind == KindHost && r.Pattern == value`, so a rule naming either was accepted
// by every validation and could never match — the author believes they widened
// the scope and they did not.
//
// Found by the live walk in 0034's own Verification block, which is what that
// block is for.
func TestASpawnRuleNamingAnASNOrAURLCanActuallyMatch(t *testing.T) {
	for _, tc := range []struct {
		kind  domain.Kind
		value string
	}{
		{domain.KindASN, "as64511"},
		{domain.KindURL, "https://acme.test/login"},
	} {
		t.Run(tc.kind.String(), func(t *testing.T) {
			r := rule(t, 1, tc.value, domain.PolarityInclude, domain.GateSpawn,
				[]domain.Kind{tc.kind}, []domain.Intensity{domain.IntensityPassive})
			got := domain.Decide([]domain.Rule{r}, domain.GateSpawn, domain.Candidate{
				Kind: tc.kind, Value: tc.value, Intensity: domain.IntensityPassive,
			})
			if got.Verdict != domain.Permitted {
				t.Fatalf("a rule naming %s did not match its own value: %v", tc.kind, got.Verdict)
			}
		})
	}
}

// The wildcard stays HOST-ONLY. 0030 refuses to invent per-kind wildcard
// semantics ahead of a caller, so `*.acme.test` on a url rule matches nothing
// rather than guessing at what a URL wildcard would mean.
func TestTheWildcardIsHostOnly(t *testing.T) {
	for _, kind := range []domain.Kind{domain.KindASN, domain.KindURL} {
		r := rule(t, 1, "*.acme.test", domain.PolarityInclude, domain.GateSpawn,
			[]domain.Kind{kind}, []domain.Intensity{domain.IntensityPassive})
		got := domain.Decide([]domain.Rule{r}, domain.GateSpawn, domain.Candidate{
			Kind: kind, Value: "x.acme.test", Intensity: domain.IntensityPassive,
		})
		if got.Verdict != domain.NotInScope {
			t.Errorf("a %s wildcard invented semantics nobody asked for: %v", kind, got.Verdict)
		}
	}
}
