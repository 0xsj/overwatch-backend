package domain

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Mapping is what extraction needs to know about one live mapping version,
// resolved from `tool` by the command. It is observation's OWN type, because
// `tool` is a peer this package may not import.
type Mapping struct {
	ID      id.ID
	Field   string
	Version int
	Path    Path
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
// `subjectField` is the tool's produces-kind spelled as a field name — 0035 §2.
// The mapping with that field yields the subject; every other mapping yields an
// attribute of it.
func Extract(body []byte, mappings []Mapping, subjectKind, subjectField string) (Extraction, error) {
	var subject *Mapping
	byField := make(map[string]Mapping, len(mappings))
	for _, m := range mappings {
		held := m
		byField[m.Field] = held
		if m.Field == subjectField {
			subject = &held
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

	for _, record := range records(body) {
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

// records splits an artifact into the things a mapping is applied to.
//
// **JSONL first**, because that is what every tool this product runs emits with
// `-json`. A single JSON object is one record; a top-level array is its
// elements. A line that does not decode is SKIPPED rather than failing the
// extraction: a tool that printed a banner before its JSON has still emitted
// findings, and refusing the whole artifact over the banner loses them.
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
