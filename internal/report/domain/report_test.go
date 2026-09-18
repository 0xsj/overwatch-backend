// Author-written, from decisions/0042's Verification block, which was written
// before this code.
package domain_test

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/report/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var at = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func an(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}

func opened(t *testing.T) domain.Report {
	t.Helper()
	r, err := domain.New(an(1), an(2), an(3), an(4), "Acme Q3", "Northbeam Security",
		time.Time{}, time.Time{}, at)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// **The two that ship OFF are exactly the two capabilities the members matrix
// withholds from a client**, which is why they had to be two toggles and not
// one.
func TestANewReportEnablesFiveSectionsAndDisablesTheTwoWithheldOnes(t *testing.T) {
	r := opened(t)
	enabled := r.Enabled()
	// DERIVED, not hardcoded. This file said "five" until 0043 added an eighth
	// section, and four tests broke for a reason unrelated to what they assert
	// — the same failure `root/vocabulary_test.go` was rewritten to avoid.
	want := 0
	for _, s := range domain.All {
		if s.DefaultOn() {
			want++
		}
	}
	if len(enabled) != want {
		t.Fatalf("want %d sections on, got %d", want, len(enabled))
	}
	for _, s := range []domain.Section{domain.SectionInvocations, domain.SectionArtifacts} {
		if r.On(s) {
			t.Fatalf("%s ships OFF", s)
		}
		if !s.Withheld() {
			t.Fatalf("%s is one of the two withheld capabilities", s)
		}
		if s.Warning() == "" {
			t.Fatalf("%s must say what turning it off costs", s)
		}
	}
	// And the five that ship on carry no warning, because omitting them costs
	// nothing the record can name.
	for _, s := range enabled {
		if s.Warning() != "" {
			t.Fatalf("%s ships on and should carry no warning", s)
		}
	}
}

// **The product's thesis, enforced.** "This section exists because a report that
// omits it implies a completeness nobody achieved."
func TestCoverageCannotBeTurnedOff(t *testing.T) {
	r := opened(t)
	if _, err := r.Toggle(domain.SectionCoverage, false, at); !errors.Is(err, domain.ErrSectionMandatory) {
		t.Fatalf("want ErrSectionMandatory, got %v", err)
	}
	// Turning it ON is a no-op and must not be refused — a client sending the
	// state it is already in is not an error.
	if _, err := r.Toggle(domain.SectionCoverage, true, at); err != nil {
		t.Fatalf("enabling the mandatory section: %v", err)
	}
	// And it is the ONLY one.
	for _, s := range domain.All {
		if s == domain.SectionCoverage {
			continue
		}
		if s.Mandatory() {
			t.Fatalf("%s must be optional", s)
		}
	}
}

// Section numbers renumber over the ENABLED set and are never stored — a stored
// number would be wrong the moment a toggle moved.
func TestSectionNumbersRenumberOverTheEnabledSet(t *testing.T) {
	r := opened(t)
	before := r.Enabled()
	if before[1] != domain.SectionAttribution {
		t.Fatalf("second section: %s", before[1])
	}
	next, err := r.Toggle(domain.SectionAttribution, false, at)
	if err != nil {
		t.Fatal(err)
	}
	after := next.Enabled()
	if len(after) != len(before)-1 {
		t.Fatalf("want %d, got %d", len(before)-1, len(after))
	}
	// What was third is now second. Nothing stored moved.
	if after[1] != domain.SectionAssets {
		t.Fatalf("the rest renumber: %s", after[1])
	}
	// THE ORIGINAL IS UNTOUCHED. A map is a reference, and a Toggle that
	// aliased its receiver's sections would edit a report somebody else holds.
	if !r.On(domain.SectionAttribution) {
		t.Fatal("Toggle mutated its receiver")
	}
}

// A section MISSING from the stored map reads as its default rather than as off:
// a row written before a section existed must not silently disable it.
func TestAnAbsentSectionReadsAsItsDefault(t *testing.T) {
	r := opened(t)
	r.Sections = map[domain.Section]bool{}
	if !r.On(domain.SectionCoverage) {
		t.Fatal("an absent default-on section reads as on")
	}
	if r.On(domain.SectionArtifacts) {
		t.Fatal("an absent default-off section reads as off")
	}
	want := 0
	for _, s := range domain.All {
		if s.DefaultOn() {
			want++
		}
	}
	if len(r.Enabled()) != want {
		t.Fatalf("the default set is %d: %v", want, r.Enabled())
	}
}

// A revision this system cannot address is not a deliverable — the same rule
// 0003 applies to an edge and 0041 to a finding.
func TestARevisionCannotBeBuiltWithoutItsHash(t *testing.T) {
	sections := []domain.Section{domain.SectionCoverage}
	if _, err := domain.NewRevision(an(1), an(2), an(3), an(4), 1, "  ", 10,
		sections, at); !errors.Is(err, domain.ErrHashRequired) {
		t.Fatalf("want ErrHashRequired, got %v", err)
	}
	if _, err := domain.NewRevision(an(1), an(2), an(3), an(4), 0, "sha256:x", 10,
		sections, at); !errors.Is(err, domain.ErrRevisionNumber) {
		t.Fatalf("want ErrRevisionNumber, got %v", err)
	}
	if _, err := domain.NewRevision(an(1), an(2), an(3), an(4), 1, "sha256:x", 10,
		nil, at); !errors.Is(err, domain.ErrNoSections) {
		t.Fatalf("a report with no sections is not a document: %v", err)
	}
}

// **A revision COPIES the section list.** It is what that client received, and a
// slice shared with the report's own would change under them when a toggle moved.
func TestARevisionKeepsItsOwnCopyOfWhatItContained(t *testing.T) {
	sections := []domain.Section{domain.SectionCoverage, domain.SectionAssets}
	rev, err := domain.NewRevision(an(1), an(2), an(3), an(4), 1, "sha256:x", 10,
		sections, at)
	if err != nil {
		t.Fatal(err)
	}
	sections[0] = domain.SectionArtifacts
	if rev.Sections[0] != domain.SectionCoverage {
		t.Fatal("the caller's slice changed what a client received")
	}
}

// Issuing bumps the count and does NOT freeze the report. The revision is the
// frozen thing; the configuration stays editable, or a firm clones a report to
// change one toggle.
func TestIssuingCountsAndLeavesTheConfigurationEditable(t *testing.T) {
	r := opened(t)
	issued, err := r.Issued(at)
	if err != nil {
		t.Fatal(err)
	}
	if issued.Revisions != 1 {
		t.Fatalf("revisions: %d", issued.Revisions)
	}
	if _, err := issued.Toggle(domain.SectionArtifacts, true, at); err != nil {
		t.Fatalf("an issued report is still editable: %v", err)
	}
}

func TestAReportNeedsATitleAndAnOrderedPeriod(t *testing.T) {
	if _, err := domain.New(an(1), an(2), an(3), an(4), "  ", "", time.Time{},
		time.Time{}, at); !errors.Is(err, domain.ErrTitleRequired) {
		t.Fatalf("want ErrTitleRequired, got %v", err)
	}
	from := at
	to := at.Add(-24 * time.Hour)
	if _, err := domain.New(an(1), an(2), an(3), an(4), "Q3", "", from, to,
		at); !errors.Is(err, domain.ErrPeriodBackwards) {
		t.Fatalf("want ErrPeriodBackwards, got %v", err)
	}
}

func TestEverySectionRoundTripsThroughItsName(t *testing.T) {
	for _, s := range domain.All {
		got, err := domain.ParseSection(s.String())
		if err != nil || got != s {
			t.Fatalf("%s round-tripped to %s (%v)", s, got, err)
		}
		if s.Title() == "" {
			t.Fatalf("%s has no title", s)
		}
	}
	if _, err := domain.ParseSection("executive_summary"); !errors.Is(err, domain.ErrSectionUnknown) {
		// There is no free-text section, and inventing one by name must fail.
		t.Fatalf("want ErrSectionUnknown, got %v", err)
	}
	// The COUNT is asserted deliberately: a section appearing or vanishing
	// should break one test on purpose, and this is that test. Everything else
	// in this file derives it.
	if len(domain.All) != 8 {
		t.Fatalf("eight sections — 0042's seven plus 0043's notes: %d", len(domain.All))
	}
}
