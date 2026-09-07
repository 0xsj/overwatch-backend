package domain

import (
	"net/netip"
	"strings"
)

// Verdict is what an evaluation answers. There are three, and the third is the
// one worth naming: nothing matched.
type Verdict uint8

const (
	// NotInScope is the DEFAULT — decisions/0030. A target with no rules
	// permits nothing. "Permit unless excluded" turns a forgotten rule set into
	// an authorisation to touch anything, and that failure is silent.
	NotInScope Verdict = iota
	Permitted
	Refused
)

func (v Verdict) String() string {
	switch v {
	case Permitted:
		return "permitted"
	case Refused:
		return "refused"
	default:
		return "not_in_scope"
	}
}

// Candidate is the thing being asked about.
type Candidate struct {
	Kind  Kind
	Value string

	// Intensity qualifies a SPAWN question and is ignored on a claim one. A
	// range in scope for passive collection is not thereby in scope for a loud
	// scan.
	Intensity Intensity
}

// Decision carries the verdict AND every rule that matched — decisions/0010
// requires the losing rule as well as the winner: *"r1 matches this host too;
// r3 wins."* A verdict alone cannot answer a client asking why, and
// reconstructing it later means replaying a rule set that may have changed.
type Decision struct {
	Verdict Verdict

	// Winner is the rule that decided it: the exclude that refused, or the
	// include that permitted. Zero when nothing matched.
	Winner Rule

	// Matched is every rule that matched the candidate, winner included, in the
	// order they were supplied.
	Matched []Rule
}

// Decide is a pure function of a rule set and a candidate.
//
// **Exclude beats include, on both gates.** A superseded rule is not evaluated;
// callers pass live rules, and this refuses to evaluate one anyway rather than
// trusting them.
func Decide(rules []Rule, gate Gate, c Candidate) Decision {
	out := Decision{Verdict: NotInScope}
	var include *Rule

	for i := range rules {
		r := rules[i]
		if r.Superseded() || r.Gate != gate {
			continue
		}
		if !r.matchesKind(c.Kind) || !r.allowsIntensity(c.Intensity) {
			continue
		}
		if !matches(r, c) {
			continue
		}
		out.Matched = append(out.Matched, r)

		if r.Polarity == PolarityExclude {
			// Exclude wins immediately and is never overturned — but the loop
			// continues, because the OTHER rules that matched are part of the
			// answer.
			if out.Verdict != Refused {
				out.Verdict = Refused
				out.Winner = r
			}
			continue
		}
		if include == nil {
			held := r
			include = &held
		}
	}

	if out.Verdict != Refused && include != nil {
		out.Verdict = Permitted
		out.Winner = *include
	}
	return out
}

// matches is the per-kind semantics — decisions/0030.
func matches(r Rule, c Candidate) bool {
	value := strings.ToLower(strings.TrimSpace(c.Value))
	if value == "" {
		return false
	}
	switch r.Kinds[0].Gate() {
	case GateSpawn:
		return matchesSpawn(r, c.Kind, value)
	default:
		// repo · email · account · document · org · person: exact, folded. No
		// wildcard until something needs one — 0030 refuses to invent per-kind
		// wildcard semantics ahead of a caller, because they are agony to change.
		return r.Pattern == value
	}
}

func matchesSpawn(r Rule, kind Kind, value string) bool {
	// A CIDR rule is a statement about every address inside it, so it matches an
	// IP candidate. The reverse is refused: "10.1.2.3 is in scope" says nothing
	// about the range containing it.
	if prefix, err := netip.ParsePrefix(r.Pattern); err == nil {
		switch kind {
		case KindIP:
			addr, err := netip.ParseAddr(value)
			return err == nil && prefix.Contains(addr.Unmap())
		case KindCIDR:
			candidate, err := netip.ParsePrefix(value)
			if err != nil {
				return false
			}
			// A candidate range is in scope only if the rule's range contains
			// ALL of it — a partial overlap is not a statement about the whole.
			return prefix.Contains(candidate.Masked().Addr()) &&
				prefix.Bits() <= candidate.Bits()
		default:
			return false
		}
	}

	if addr, err := netip.ParseAddr(r.Pattern); err == nil {
		// Address comparison, never text: 10.0.0.1 and ::ffff:10.0.0.1 are the
		// same host wearing different spellings.
		if kind != KindIP {
			return false
		}
		other, err := netip.ParseAddr(value)
		return err == nil && addr.Unmap() == other.Unmap()
	}

	// A host pattern. `*.example.com` does NOT match the apex and DOES match any
	// depth below it — 0010, sealed because the safer reading never silently
	// widens.
	//
	// The wildcard is HOST-ONLY. Extending it to `asn` or `url` would be
	// inventing per-kind wildcard semantics ahead of a caller, which 0030
	// refuses on the ground that they are agony to change once written.
	if suffix, ok := strings.CutPrefix(r.Pattern, "*."); ok {
		return kind == KindHost &&
			strings.HasSuffix(value, "."+suffix) &&
			len(value) > len(suffix)+1
	}

	// `asn` and `url` joined the spawn gate on 2026-09-07 (decisions/0034), and
	// this line said `kind == KindHost` — so a rule naming either was ACCEPTED
	// and could never fire. A rule that silently never matches is worse than a
	// refused one: the author believes they widened the scope and they did not.
	//
	// Exact and folded, the same treatment the claim-gated kinds get, and for
	// the same reason.
	return (kind == KindHost || kind == KindASN || kind == KindURL) &&
		r.Pattern == value
}
