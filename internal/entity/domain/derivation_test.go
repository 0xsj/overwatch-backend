// Author-written, from decisions/0040's Verification block and 0003's Decision
// section, both written before this code.
package domain_test

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/entity/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var when = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func an(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}

func drawn(t *testing.T) domain.Derivation {
	t.Helper()
	d, err := domain.NewDerivation(an(1), an(2), an(3), an(4), "input",
		an(5), an(6), an(7), when)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// **THE test of this file.** `0003`: an edge without an invocation and an
// artifact is a similarity edge wearing a costume, and `CLAUDE.md`
// §out_of_scope bans those outright. There is deliberately no constructor that
// omits either.
func TestADerivationCannotBeBuiltWithoutItsSource(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		invocation, artifact id.ID
	}{
		{"no invocation", id.ID{}, an(6)},
		{"no artifact", an(5), id.ID{}},
		{"neither", id.ID{}, id.ID{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := domain.NewDerivation(an(1), an(2), an(3), an(4), "input",
				tc.invocation, tc.artifact, an(7), when)
			if !errors.Is(err, domain.ErrUnsourced) {
				t.Fatalf("want ErrUnsourced, got %v", err)
			}
		})
	}
}

// `0003`: the two shapes are DISJOINT rather than one shape with optional
// fields. This is that, checked structurally — a derivation has nowhere to put
// a claimant, a confidence or a state, so it cannot acquire one.
func TestADerivationCarriesNothingToAgreeWith(t *testing.T) {
	d := drawn(t)
	// A compile-time claim rather than a runtime one: if any of these fields is
	// ever added, this stops building and somebody has to reread 0003.
	var _ = struct {
		ID, WorkspaceID, From, To     id.ID
		Label                         string
		Invocation, Artifact, Mapping id.ID
		CreatedAt                     time.Time
	}(d)
}

// A fragment is not read out of itself. No tool does this on purpose; a mapping
// pointed at its own subject does it on every record, which is exactly the
// quiet failure 0040's Verification block names.
func TestAFragmentIsNotReadOutOfItself(t *testing.T) {
	_, err := domain.NewDerivation(an(1), an(2), an(3), an(3), "input",
		an(5), an(6), an(7), when)
	if !errors.Is(err, domain.ErrDerivationToSelf) {
		t.Fatalf("want ErrDerivationToSelf, got %v", err)
	}
}

// 0003 requires a label naming the act, and 0040 makes it the mapping's field.
func TestADerivationNamesTheAct(t *testing.T) {
	for _, label := range []string{"", "   "} {
		_, err := domain.NewDerivation(an(1), an(2), an(3), an(4), label,
			an(5), an(6), an(7), when)
		if !errors.Is(err, domain.ErrLabelRequired) {
			t.Fatalf("%q: want ErrLabelRequired, got %v", label, err)
		}
	}
	d, err := domain.NewDerivation(an(1), an(2), an(3), an(4), "  SAN entry  ",
		an(5), an(6), an(7), when)
	if err != nil {
		t.Fatal(err)
	}
	if d.Label != "SAN entry" {
		t.Fatalf("the label is trimmed and otherwise verbatim: %q", d.Label)
	}
}

// 0040 §5: an unresolved provenance keeps BOTH forms of the value. A fold
// mismatch is one of the two reasons this row exists, and storing only the
// folded form would hide it.
func TestAnUnresolvedProvenanceKeepsWhatTheToolWroteAndWhatWasLookedUp(t *testing.T) {
	u, err := domain.NewUnresolved(an(1), an(2), an(3), an(4), an(5),
		"host", "  ACME.Test  ", "input", when)
	if err != nil {
		t.Fatal(err)
	}
	if u.FromValue != "acme.test" {
		t.Fatalf("the looked-up value is folded: %q", u.FromValue)
	}
	if u.FromRaw != "ACME.Test" {
		t.Fatalf("what the tool wrote is kept: %q", u.FromRaw)
	}
	if u.To != an(5) {
		t.Fatal("the half that DID resolve is still recorded")
	}
}

// The fold is ONE function, shared with the fragment table. A derivation
// resolves its `from` by looking a value up in that table, so the lookup and the
// row must agree about what "the same host" means — 0037 named the fold mismatch
// as its own quiet failure and this is another join that would suffer it.
func TestTheFoldIsTheSameOneTheFragmentTableUses(t *testing.T) {
	f, err := domain.NewFragment(an(1), an(2), "host", "  ACME.Test  ",
		domain.Observed, when)
	if err != nil {
		t.Fatal(err)
	}
	if f.Value != domain.Fold("ACME.Test") {
		t.Fatalf("a fragment's value and the derivation lookup must fold alike: %q vs %q",
			f.Value, domain.Fold("ACME.Test"))
	}
}

func TestAnUnresolvedProvenanceIsAKindAndAValue(t *testing.T) {
	for _, tc := range []struct{ kind, value string }{
		{"", "acme.test"},
		{"host", ""},
		{"host", "   "},
	} {
		_, err := domain.NewUnresolved(an(1), an(2), an(3), an(4), an(5),
			tc.kind, tc.value, "input", when)
		if !errors.Is(err, domain.ErrSubjectRequired) {
			t.Fatalf("%q/%q: want ErrSubjectRequired, got %v", tc.kind, tc.value, err)
		}
	}
}
