package domain

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Role is what a mapping is FOR — decisions/0040. observation's own copy of
// `tool`'s three, because the two are peers; `root/vocabulary_test.go` is the
// shape that stops duplicated vocabularies drifting, and this one is three
// words long.
type Role uint8

const (
	// RoleAttribute is the zero value and the default, so a mapping that
	// arrives without a role read is the harmless one.
	RoleAttribute Role = iota
	RoleSubject
	RoleDerivedFrom

	// The two a FINDING needs — decisions/0041 §2. `signature` is what the tool
	// calls this class of problem and is half the finding's identity;
	// `severity` is the tool's own assessment, and it arrives as a `rule`
	// claimant with NO confidence because a template asserting `high` is a
	// category rather than a probability — 0004.
	RoleSignature
	RoleSeverity
)

var roleNames = map[Role]string{
	RoleAttribute: "attribute", RoleSubject: "subject", RoleDerivedFrom: "derived_from",
	RoleSignature: "signature", RoleSeverity: "severity",
}

func (r Role) String() string {
	if n, ok := roleNames[r]; ok {
		return n
	}
	return "attribute"
}

func ParseRole(s string) (Role, error) {
	for role, name := range roleNames {
		if name == s {
			return role, nil
		}
	}
	return RoleAttribute, ErrRoleUnknown
}

// Mapping is what extraction needs to know about one live mapping version,
// resolved from `tool` by the command. It is observation's OWN type, because
// `tool` is a peer this package may not import.
type Mapping struct {
	ID      id.ID
	Field   string
	Version int
	Path    Path

	// Role is what this mapping is for — 0040. It is what [Extract] resolves
	// the subject by, REPLACING the field-name match `0035` used.
	Role Role
}

// Reading is one value a mapping read out of one record, before it is an
// observation — it has no ids and no clock, so [Extract] stays a pure function
// of the bytes and the mappings.
type Reading struct {
	Mapping Mapping
	Value   string
}

// Extraction is the whole answer: the readings, the paths nobody claimed, and
// the three counts the Extraction Quality panel is built on.
type Extraction struct {
	// Subject is what every reading in this record is about — the value the
	// tool's produces-kind mapping read. Empty when the record did not carry
	// one, in which case Readings is empty too: a value with no subject is a
	// statement about nothing.
	Records []Record

	// Seen is every distinct leaf path across every record.
	Seen []string
	// Mapped is the subset a live mapping claimed. LeftAlone is the difference,
	// and the three are reported together because a ratio without its
	// denominator is the thing 0011 refuses.
	Mapped    []string
	LeftAlone []Unclaimed
}

// Record is one line of JSONL, or one element of a JSON array, with its subject
// resolved.
type Record struct {
	SubjectKind  string
	SubjectValue string
	Readings     []Reading

	// DerivedFrom is the value THIS RECORD WAS READ OUT OF, when the tool
	// declared a mapping saying where to find it — decisions/0040. `httpx`
	// writes it as `.input`: the host it was handed.
	//
	// Empty when the tool declares no such mapping, or when this particular
	// record did not carry the field. The two are the same absence here and
	// are separated one level up: a tool with no provenance mapping produces
	// no unresolved rows, and a tool WITH one that read nothing does.
	DerivedFrom string

	// DerivedLabel is the mapping's FIELD NAME, which `0003` requires an edge
	// to carry as the name of the act. Nothing new is stored — the field
	// already names what was read, and for this role that IS the act.
	DerivedLabel string

	// DerivedMapping is the version that read it, so the edge cites a mapping
	// the same way an observation does.
	DerivedMapping id.ID

	// Signature and Severity are what a FINDING is built from —
	// decisions/0041 §2. Empty on every tool that does not produce findings,
	// which is all of them but one.
	//
	// Signature is HALF THE IDENTITY of a finding: it is what the tool calls
	// this class of problem, and it is why a rescan is a sighting rather than a
	// new row. Severity is the tool's own assessment, and it becomes a `rule`
	// claimant carrying no confidence — 0004.
	Signature string
	Severity  string

	// SignatureMapping is the version that read it. A finding cites its parser
	// for the same reason an observation does.
	SignatureMapping id.ID
}

// Unclaimed is a leaf path with no mapping, with enough beside it that a person
// can decide whether to map it without opening the artifact.
type Unclaimed struct {
	Path   string
	Seen   int
	Sample string
}

// Extract reads an artifact's bytes under a set of live mappings.
//
// **It is a pure function and it takes no clock and no minter.** That is what
// lets the whole of decisions/0035's field accounting be tested against literal
// bytes, and it is why the ids and timestamps are the command's job.
//
// **THE SUBJECT IS RESOLVED BY ROLE, NOT BY NAME** — decisions/0040 §1. `0035`
// looked for the mapping whose FIELD matched the tool's produces-kind, which
// worked and worked by luck: renaming the field silently cost the tool its
// ability to say what anything was about. A role is a declaration and a name is
// a spelling, and after this a field may be called whatever reads best.
//
// `subjectKind` is still the tool's produces-kind — it is what the subject VALUE
// is a value OF, which no mapping carries and only the tool knows.
//
// `shape` is what the bytes are, from the invocation's declared media type. It
// is the parameter that was missing until 2026-09-08, and the whole line-oriented
// half of the recon corpus was unreadable without it — see [Shape].
func Extract(body []byte, mappings []Mapping, subjectKind string, shape Shape) (Extraction, error) {
	var subject, provenance, signature, severity *Mapping
	for _, m := range mappings {
		held := m
		switch m.Role {
		case RoleSignature:
			if signature == nil {
				signature = &held
			}
		case RoleSeverity:
			if severity == nil {
				severity = &held
			}
		case RoleSubject:
			// FIRST WINS, and there is only ever one: `mapping_one_subject`
			// refuses a second LIVE subject per tool. Taking the first rather
			// than the last means a store that somehow answered two produces a
			// stable choice instead of an order-dependent one.
			if subject == nil {
				subject = &held
			}
		case RoleDerivedFrom:
			if provenance == nil {
				provenance = &held
			}
		}
	}
	if subject == nil {
		// THE ONE PLACE THIS REFUSES. A tool with no mapping for its own
		// produces-kind cannot say what any of its values are about, and an
		// empty result would look identical to a tool that emitted nothing.
		return Extraction{}, ErrNoSubjectMapping
	}

	claimed := make(map[string]bool, len(mappings))
	for _, m := range mappings {
		claimed[m.Path.String()] = true
	}

	out := Extraction{}
	seen := map[string]int{}
	sample := map[string]string{}

	for _, record := range recordsOf(body, shape) {
		for _, path := range Leaves(record) {
			seen[path]++
			if _, held := sample[path]; !held {
				if values := mustPath(path).Read(record); len(values) > 0 {
					sample[path] = values[0]
				}
			}
		}

		values := subject.Path.Read(record)
		if len(values) == 0 {
			// No subject in this record. Every value in it would be a statement
			// about nothing, so none of them is kept — and the paths are still
			// counted above, because the tool did say them.
			continue
		}

		row := Record{SubjectKind: subjectKind, SubjectValue: values[0]}
		for _, m := range mappings {
			for _, value := range m.Path.Read(record) {
				row.Readings = append(row.Readings, Reading{Mapping: m, Value: value})
			}
		}
		if signature != nil {
			// HALF A FINDING'S IDENTITY. The first value only: a record naming
			// two problems is a tool this design has not met, and picking one
			// arbitrarily would make the identity depend on read order.
			if sig := signature.Path.Read(record); len(sig) > 0 {
				row.Signature = sig[0]
				row.SignatureMapping = signature.ID
			}
		}
		if severity != nil {
			if sev := severity.Path.Read(record); len(sev) > 0 {
				row.Severity = sev[0]
			}
		}
		if provenance != nil {
			// WHAT THIS RECORD WAS READ OUT OF — 0040. The FIRST value only: a
			// derivation runs between two fragments, and a record claiming two
			// inputs is a tool this design has not met. Taking the first is a
			// decision rather than an oversight, and the reading itself is
			// still stored in full above.
			if from := provenance.Path.Read(record); len(from) > 0 {
				row.DerivedFrom = from[0]
				row.DerivedLabel = provenance.Field
				row.DerivedMapping = provenance.ID
			}
		}
		out.Records = append(out.Records, row)
	}

	for path, count := range seen {
		out.Seen = append(out.Seen, path)
		if claimed[path] {
			out.Mapped = append(out.Mapped, path)
			continue
		}
		out.LeftAlone = append(out.LeftAlone, Unclaimed{
			Path: path, Seen: count, Sample: sample[path],
		})
	}
	sort.Strings(out.Seen)
	sort.Strings(out.Mapped)
	sort.Slice(out.LeftAlone, func(i, j int) bool { return out.LeftAlone[i].Path < out.LeftAlone[j].Path })
	return out, nil
}

// recordsOf splits an artifact into the things a mapping is applied to,
// according to what the bytes were DECLARED to be — never what they look like.
func recordsOf(body []byte, shape Shape) []any {
	if shape == ShapeLines {
		return lines(body)
	}
	return records(body)
}

// lines reads a TEXT artifact: one record per line, carrying that line's whole
// content at [LinePath].
//
// **A blank line is not a record.** Every tool in this corpus ends its output
// with a newline and several separate their sections with one, and an empty
// subject is a statement about nothing — [Extract] would skip it anyway, but it
// would first count `.line` as having been seen once more than it was, which is
// a number the Extraction Quality panel reports.
//
// A trailing carriage return is stripped: a tool run through a pipe on a machine
// that writes CRLF has still said `acme.test`, and keeping the `\r` would make
// the fragment, the attribution and every later comparison differ from the same
// host observed anywhere else. That is the ONE normalisation here, and it is
// about the line ENDING rather than about the value.
func lines(body []byte) []any {
	trimmed := bytes.TrimRight(body, "\r\n")
	if len(bytes.TrimSpace(trimmed)) == 0 {
		return nil
	}
	split := bytes.Split(trimmed, []byte{'\n'})
	out := make([]any, 0, len(split))
	for _, line := range split {
		text := string(bytes.TrimSpace(bytes.TrimRight(line, "\r")))
		if text == "" {
			continue
		}
		out = append(out, map[string]any{LinePath: text})
	}
	return out
}

// records splits a JSON artifact.
//
// **JSONL first**, because that is what every tool this product runs emits with
// `-json`. A single JSON object is one record; a top-level array is its
// elements. A line that does not decode is SKIPPED rather than failing the
// extraction: a tool that printed a banner before its JSON has still emitted
// findings, and refusing the whole artifact over the banner loses them.
//
// **That skip is why the shape has to be declared.** Handed a file of bare
// hostnames this returns nothing at all, and it is indistinguishable from a tool
// that emitted a banner and no findings.
func records(body []byte) []any {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil
	}

	if trimmed[0] == '[' {
		var list []any
		if err := json.Unmarshal(trimmed, &list); err == nil {
			return list
		}
	}

	out := make([]any, 0, 8)
	for _, line := range bytes.Split(trimmed, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var record any
		if err := json.Unmarshal(line, &record); err != nil {
			continue
		}
		out = append(out, record)
	}
	return out
}

// mustPath re-parses a path this package just printed. It cannot fail — Leaves
// emits the spelling ParsePath accepts — and an empty Path reads nothing, so a
// bug here loses a sample rather than producing a wrong one.
func mustPath(s string) Path {
	p, err := ParsePath(s)
	if err != nil {
		return Path{}
	}
	return p
}

// FieldOf is how a mapping's field becomes an observation's. It exists so the
// trimming rule is stated once: a field name is the MAPPING's, never the
// source's key.
func FieldOf(m Mapping) string { return strings.TrimSpace(m.Field) }
