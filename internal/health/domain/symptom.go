package domain

import (
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Kind is a way this machinery goes quiet. Every one of them looks, from the
// outside, like an estate that has nothing wrong with it.
//
// `CLAUDE.md` justifies this whole noun with one clause — *"a tool off PATH
// looks like silence"* — and the list is the set of silences this system can
// currently tell apart from a genuinely clean scan.
type Kind uint8

const (
	// ToolUnavailable is `execx` reporting a tool it could not start at all —
	// off PATH, not executable. The run finished, the record is complete, and
	// nothing was looked at.
	ToolUnavailable Kind = iota

	// ToolUnread is a tool with NO LIVE MAPPINGS. It spawns, its bytes are
	// stored and citable, and nothing is read out of them — which is a true and
	// ordinary state for a tool nobody has taught this system yet, and
	// indistinguishable from one that found nothing.
	ToolUnread

	// ToolNoSignature is a finding-producing tool with no `signature` mapping —
	// decisions/0041 §2 names it as the failure that looks like a clean scan.
	ToolNoSignature

	// CheckUnrunnable is a check that is enabled, has an interval, and has no
	// chain. The scheduler skips it every tick, forever, and a coverage grid
	// counts its column as never attempted without ever saying why.
	CheckUnrunnable

	// EventBuried is an outbox row that gave up — owed item B. `pkg/outbox`'s
	// own doc says *"a buried outbox row is the alarm"*, and until now nothing
	// read them: the alarm was one ERROR line in a log nobody queries.
	EventBuried

	// FieldUnmapped is a tool emitting paths no mapping claims. Usually a tool
	// grew fields after an upgrade, which is the case `0035` records rather than
	// guesses at — and which nothing surfaces until somebody opens one
	// invocation.
	FieldUnmapped
)

var kindNames = map[Kind]string{
	ToolUnavailable: "tool_unavailable",
	ToolUnread:      "tool_unread",
	ToolNoSignature: "tool_no_signature",
	CheckUnrunnable: "check_unrunnable",
	EventBuried:     "event_buried",
	FieldUnmapped:   "field_unmapped",
}

// All is the canonical order, worst first. It is also the list a [Report] must
// account for: every kind gets a probe whether or not it found anything.
var All = []Kind{
	ToolUnavailable, ToolNoSignature, EventBuried,
	CheckUnrunnable, ToolUnread, FieldUnmapped,
}

func (k Kind) String() string { return kindNames[k] }

// Says is the sentence this kind of silence tells. It is here rather than in the
// client because it is an argument about what the absence MEANS, and a copy of
// it in the other repository would drift from this one.
func (k Kind) Says() string {
	switch k {
	case ToolUnavailable:
		return "the tool could not start, so nothing was looked at — and the run finished cleanly"
	case ToolUnread:
		return "the tool ran and nothing reads its output, so it produces no observations"
	case ToolNoSignature:
		return "this tool produces findings and declares no signature, so every scan looks clean"
	case CheckUnrunnable:
		return "the check is enabled on a clock and has no chain, so it is skipped every tick"
	case EventBuried:
		return "an event gave up after its retries and is skipped forever"
	case FieldUnmapped:
		return "the tool is saying something nobody has taught this system to read"
	}
	return ""
}

// Symptom is one thing that is quietly not working.
type Symptom struct {
	Kind Kind

	// Subject is what it is about, named the way a person would say it — a
	// tool's name, a check's. An id alone is a symptom nobody can act on.
	Subject   string
	SubjectID id.ID

	// Detail is the specific reason, verbatim from whatever recorded it:
	// `execx`'s error, the path nobody mapped, the handler that gave up.
	Detail string

	// Count is how many times. One tool off PATH across forty invocations is
	// ONE symptom with a count, not forty — the same argument `0041` makes for
	// a finding.
	Count int

	// Since is the OLDEST occurrence, not the newest. "This has been broken
	// since Tuesday" is the sentence somebody needs; "last seen a minute ago"
	// is true of everything that is still broken.
	Since time.Time
}

// Probe is WHAT WAS LOOKED AT, and it is the half of this report that makes the
// other half mean anything.
//
// **A clean bill of health that does not say what it examined is
// indistinguishable from a report nobody ran.** That is `CLAUDE.md`'s own rule
// about counts — *"an unmeasured total renders as `–` and never as `0`, because
// a zero nothing computed is not a zero"* — applied to the screen whose entire
// job is telling silence apart from health.
type Probe struct {
	Kind Kind

	// Measured is FALSE when this probe could not run — its read failed, its
	// port is not wired. It is not the same as looking and finding nothing, and
	// collapsing the two is the exact failure this noun exists to prevent.
	Measured bool

	// Because names why it could not be measured. Empty when it was.
	Because string

	// Looked is how many rows were considered and Found how many were symptoms.
	// Both are meaningless unless Measured.
	Looked int
	Found  int
}

// Report is the whole answer: every probe, and whatever they found.
type Report struct {
	At time.Time

	// Probes carries one entry PER KIND, always, in canonical order. A kind
	// missing from this list is a bug in the assembler, not a clean result.
	Probes []Probe

	// Symptoms is worst-kind-first, then by count. Empty is a legitimate answer
	// ONLY when every probe was measured — see [Report.Trustworthy].
	Symptoms []Symptom
}

// Trustworthy says whether "nothing is wrong" is a claim this report can make.
//
// If any probe could not run, a clean symptom list means *"we found nothing in
// the places we could look"*, which is a different sentence and must be rendered
// as a different one.
func (r Report) Trustworthy() bool {
	for _, p := range r.Probes {
		if !p.Measured {
			return false
		}
	}
	return true
}

// Unmeasured is which probes could not run, for the sentence above.
func (r Report) Unmeasured() []Probe {
	out := make([]Probe, 0, len(r.Probes))
	for _, p := range r.Probes {
		if !p.Measured {
			out = append(out, p)
		}
	}
	return out
}
