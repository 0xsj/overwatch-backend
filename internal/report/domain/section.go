package domain

// Section is one part of a report, and **a section is a capability** —
// `CLAUDE.md`, and the reason this is an enum rather than data a person types.
//
// There is no free-text section. A report whose strongest claim is prose nobody
// can source is the document this product exists to be an alternative to, and an
// engagement summary a person writes belongs in `note` — which can then be a
// sourced section like any other.
type Section uint8

const (
	SectionScope Section = iota
	SectionAttribution
	SectionAssets
	SectionFindings
	SectionCoverage
	SectionInvocations
	SectionArtifacts

	// SectionNotes is decisions/0043, and it is `0042` executing its own stated
	// future rather than reversing it. That record refused a section a person
	// types into — *"a report whose strongest claim is prose nobody can source
	// is the document this product exists to be an alternative to"* — and said
	// an engagement summary belongs in `note`, *"which can then be a sourced
	// section like any other"*.
	//
	// **This section RENDERS notes; it does not accept prose.** Each one names
	// its author and when they wrote it, which is the same standard the other
	// seven meet. If it ever takes typed text directly, 0042 has been reversed
	// and that is where the argument belongs.
	SectionNotes
)

// All is the canonical ORDER. Section numbers are derived from it over the
// ENABLED set — disable the second and the rest renumber — so a number is never
// stored, because a stored one is wrong the moment a toggle moves.
var All = []Section{
	SectionScope, SectionAttribution, SectionAssets, SectionFindings,
	SectionCoverage, SectionNotes, SectionInvocations, SectionArtifacts,
}

var sectionNames = map[Section]string{
	SectionScope:       "scope_and_method",
	SectionAttribution: "attribution_evidence",
	SectionAssets:      "asset_inventory",
	SectionFindings:    "findings_by_severity",
	SectionCoverage:    "coverage",
	SectionInvocations: "invocation_log",
	SectionArtifacts:   "raw_artifacts",
	SectionNotes:       "engagement_notes",
}

var sectionTitles = map[Section]string{
	SectionScope:       "Scope and method",
	SectionAttribution: "Attribution evidence",
	SectionAssets:      "Asset inventory",
	SectionFindings:    "Findings, by severity",
	SectionCoverage:    "Coverage — what was not tested",
	SectionInvocations: "Invocation log, including refusals",
	SectionArtifacts:   "Raw artifacts appendix",
	SectionNotes:       "Engagement notes",
}

func (s Section) String() string {
	if n, ok := sectionNames[s]; ok {
		return n
	}
	return ""
}

func (s Section) Title() string { return sectionTitles[s] }

func ParseSection(s string) (Section, error) {
	for section, name := range sectionNames {
		if name == s {
			return section, nil
		}
	}
	return SectionScope, ErrSectionUnknown
}

// DefaultOn is what a NEW report enables. **The two that ship off are exactly
// the two capabilities the members matrix withholds from a client** — which is
// why they had to be two toggles and not one.
func (s Section) DefaultOn() bool {
	return s != SectionInvocations && s != SectionArtifacts
}

// Mandatory says a section cannot be turned off, and exactly one is.
//
// **Coverage is the product's thesis**: *"this section exists because a report
// that omits it implies a completeness nobody achieved."* A findings report with
// no coverage section is the document every other tool produces, and the one
// this product exists to refuse. Making it a toggle would make refusing it
// optional.
func (s Section) Mandatory() bool { return s == SectionCoverage }

// Warning is what turning a section OFF costs, in the words the screens draft
// wrote. Empty for the five that cost nothing to omit.
//
// It lives here rather than in the client because it is an argument about the
// record rather than a label: the same sentence has to appear wherever a toggle
// does, and a copy in one repository drifts from a copy in the other.
func (s Section) Warning() string {
	switch s {
	case SectionInvocations:
		return "removes the SCOPE PROOF. It is the record of what did not run, " +
			"and \"did you stay in scope\" is answered from it"
	case SectionArtifacts:
		return "removes REPLAY. Without the bytes, a claim in this report cannot " +
			"be re-derived by the reader"
	}
	return ""
}

// Withheld says a section carries one of the two capabilities a `client` is not
// given — `CLAUDE.md`'s *"generate a report vs receive its artifacts"*.
//
// It is NOT enforced here: a firm may deliberately send a client the invocation
// log, and `0019`'s pair is about what the role grants by default rather than
// what a report may contain. It is exposed so a screen can say which toggles are
// the ones that matter.
func (s Section) Withheld() bool {
	return s == SectionInvocations || s == SectionArtifacts
}
