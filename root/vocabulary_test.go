// Author-written, and it is a CONTRACT rather than a unit test — decisions/0034.
//
// Three domains hold three copies of one kind vocabulary. `U1` forbids sharing
// the type — they are peers — and `U3` forbids putting a recon asset vocabulary
// in `pkg/`, which is feature-free. `internal/fragment` does not exist and
// inventing it for a list would be a package with no table.
//
// So the duplication stays and is ENFORCED here, at the composition root, which
// is the one place allowed to see all three. The list below is a fourth copy,
// deliberately — **a copy whose only job is to disagree loudly**, which is a
// different thing from a copy that gets used.
package root

import (
	"sort"
	"testing"

	checkdomain "github.com/0xsj/overwatch-backend/internal/check/domain"
	scopedomain "github.com/0xsj/overwatch-backend/internal/scope/domain"
	tooldomain "github.com/0xsj/overwatch-backend/internal/tool/domain"
)

// theVocabulary is decisions/0034 §1: `0009`'s thirteen fragment kinds plus
// `url`. Transcribed from the record, not from any of the three packages — a
// copy taken from one of them would agree with it by construction.
var theVocabulary = []string{
	"org", "person", "cert", "asn", "host", "cidr", "ip", "repo",
	"email", "account", "key", "whois", "document", "url",
}

// theTargetable is 0034 §3: what a fragment of this kind can be an ASSET —
// `0009`'s targetable set, plus `url`.
var theTargetable = []string{"host", "cidr", "ip", "asn", "url", "repo", "email"}

// theSpawnable is 0010's gate partition, widened by 0034 to admit `asn` and
// `url` because asnmap and whois take an ASN and nuclei takes a URL.
var theSpawnable = []string{"host", "cidr", "ip", "asn", "url"}

func set(of []string) map[string]bool {
	out := make(map[string]bool, len(of))
	for _, s := range of {
		out[s] = true
	}
	return out
}

func names[T interface{ String() string }](of []T) []string {
	out := make([]string, 0, len(of))
	for _, v := range of {
		out = append(out, v.String())
	}
	sort.Strings(out)
	return out
}

func sorted(of []string) []string {
	out := append([]string(nil), of...)
	sort.Strings(out)
	return out
}

func same(t *testing.T, what string, got, want []string) {
	t.Helper()
	g, w := set(got), set(want)
	for _, name := range want {
		if !g[name] {
			t.Errorf("%s is missing %q", what, name)
		}
	}
	for _, name := range got {
		if !w[name] {
			t.Errorf("%s has %q, which is not in the record", what, name)
		}
	}
}

// scope holds the canonical copy, so this is the one that pins the record.
func TestScopeKindIsTheVocabulary(t *testing.T) {
	same(t, "scope.Kind", names(scopedomain.Every()), theVocabulary)
}

// 0034 §4: check's subjects are EXACTLY the targetable facet, because coverage
// is over assets. Not a subset — equal. A missing one is a coverage cell nobody
// can ask for.
func TestCheckSubjectsAreExactlyTheTargetableFacet(t *testing.T) {
	same(t, "check.Subject", names(checkdomain.EverySubject()), theTargetable)
}

// 0034 §4: tool feeds are the vocabulary plus `finding`, and `finding` is the
// only member that is not a kind.
func TestToolFeedsAreTheVocabularyPlusFinding(t *testing.T) {
	same(t, "tool.Feed", names(tooldomain.EveryFeed()), append(sorted(theVocabulary), "finding"))
}

// The relationships, asserted against the PACKAGES rather than against the
// record — so a change that updates the record and one package but not the
// others still fails.
func TestEveryCheckSubjectIsAScopeKindAndIsTargetable(t *testing.T) {
	for _, subject := range checkdomain.EverySubject() {
		kind, err := scopedomain.ParseKind(subject.String())
		if err != nil {
			t.Errorf("check subject %q is not a scope kind", subject)
			continue
		}
		if !kind.Targetable() {
			t.Errorf("check subject %q is not targetable — coverage is over assets", subject)
		}
	}
}

func TestEveryToolFeedButFindingIsAScopeKind(t *testing.T) {
	for _, feed := range tooldomain.EveryFeed() {
		if feed == tooldomain.FeedFinding {
			continue
		}
		if _, err := scopedomain.ParseKind(feed.String()); err != nil {
			t.Errorf("tool feed %q is not a scope kind", feed)
		}
	}
	// And `finding` really is the exception rather than an oversight: it must
	// NOT parse as a kind, or the superset claim is vacuous.
	if _, err := scopedomain.ParseKind("finding"); err == nil {
		t.Error("a finding is not a fragment and must not be a scope kind")
	}
}

// 0034 §3, stated as an invariant rather than a coincidence: you cannot spawn
// against something that is not an asset.
func TestSpawnableIsASubsetOfTargetable(t *testing.T) {
	for _, kind := range scopedomain.Every() {
		if kind.Gate() == scopedomain.GateSpawn && !kind.Targetable() {
			t.Errorf("%q is spawnable and not targetable — you cannot aim a process at a non-asset", kind)
		}
	}
	same(t, "the spawn gate", spawnable(), theSpawnable)
}

func spawnable() []string {
	out := make([]string, 0)
	for _, kind := range scopedomain.Every() {
		if kind.Gate() == scopedomain.GateSpawn {
			out = append(out, kind.String())
		}
	}
	sort.Strings(out)
	return out
}

// `domain` was deleted on 2026-09-07 — a domain is a HOST IN A ROLE, which is
// 0009's argument about assets applied one level down. This test exists because
// re-adding it is the single most likely way this reconciliation is undone, and
// it would be undone one package at a time.
func TestNoVocabularyHasADomain(t *testing.T) {
	if _, err := scopedomain.ParseKind("domain"); err == nil {
		t.Error("scope.Kind has `domain` — a domain is a host in a role, 0034")
	}
	if _, err := checkdomain.ParseSubject("domain"); err == nil {
		t.Error("check.Subject has `domain` — a domain is a host in a role, 0034")
	}
	if feed, err := tooldomain.ParseFeed("domain"); err == nil && feed != tooldomain.FeedNone {
		t.Error("tool.Feed has `domain` — a domain is a host in a role, 0034")
	}
}

// Every kind round-trips through its spelling, in every package. An enum
// crossing a module boundary travels as its SPELLING and never as an ordinal —
// which is what makes three copies comparable at all.
func TestEveryKindRoundTripsInEveryPackage(t *testing.T) {
	for _, kind := range scopedomain.Every() {
		back, err := scopedomain.ParseKind(kind.String())
		if err != nil || back != kind {
			t.Errorf("scope %q did not round-trip", kind)
		}
	}
	for _, subject := range checkdomain.EverySubject() {
		back, err := checkdomain.ParseSubject(subject.String())
		if err != nil || back != subject {
			t.Errorf("check %q did not round-trip", subject)
		}
	}
	for _, feed := range tooldomain.EveryFeed() {
		back, err := tooldomain.ParseFeed(feed.String())
		if err != nil || back != feed {
			t.Errorf("tool %q did not round-trip", feed)
		}
	}
}
