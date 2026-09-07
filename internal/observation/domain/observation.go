package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	MaxFieldLength = 200
	MaxValueLength = 4000
	MaxPathLength  = 400
)

// Observation is what ONE SOURCE SAID about one field, at the time the tool ran.
//
// **Never a fact.** Every column here is a record of a statement, not of the
// world: `Value` is what the source emitted, and nothing in this package
// promises it is true.
type Observation struct {
	ID          id.ID
	WorkspaceID id.ID

	// SubjectKind and SubjectValue are what the observation is ABOUT, carried
	// inline rather than as a fragment reference — decisions/0035. `fragment`
	// is UNBUILT and a dedup over exactly these two columns, so it will add a
	// column here rather than change what a row means.
	SubjectKind  string
	SubjectValue string

	// Field is what was read, named by the MAPPING and never inferred from the
	// source's own key. A guessed name is a value nobody was asked for.
	Field string
	Value string

	// The lineage — decisions/0035 §7. Every value walks backwards to the
	// parser version, the raw bytes and the exact command; the scope rule that
	// permitted the command hangs off the invocation.
	InvocationID id.ID
	ArtifactID   id.ID
	MappingID    id.ID

	// MappingVersion is captured HERE as well as reachable through MappingID,
	// because it is what a correction changes and a reader comparing two
	// observations of one field wants the number without a second read.
	MappingVersion int

	// ObservedAt is when the TOOL RAN, not when extraction happened. Re-reading
	// a three-month-old artifact under a corrected mapping must not make it
	// look fresh.
	ObservedAt time.Time

	// RecordedAt is when this row was written. The pair is `observed time is not
	// event time`, and keeping both is what makes a re-extraction visible.
	RecordedAt time.Time
}

func New(newID, workspace, invocation, artifact, mapping id.ID, version int,
	subjectKind, subjectValue, field, value string, observedAt, recordedAt time.Time) (Observation, error) {
	if newID.IsZero() || invocation.IsZero() || artifact.IsZero() || mapping.IsZero() {
		return Observation{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Observation{}, ErrWorkspaceRequired
	}
	if observedAt.IsZero() || recordedAt.IsZero() {
		return Observation{}, ErrTimeRequired
	}
	subjectKind = strings.TrimSpace(subjectKind)
	subjectValue = strings.TrimSpace(subjectValue)
	if subjectKind == "" || subjectValue == "" {
		return Observation{}, ErrSubjectRequired
	}
	field = strings.TrimSpace(field)
	if field == "" || len(field) > MaxFieldLength {
		return Observation{}, ErrFieldRequired
	}
	// The VALUE is not trimmed and not length-checked downward: it is what the
	// source said, and trimming it would be this package editing a statement it
	// is only supposed to hold. Only the upper bound applies, and it is the
	// column's.
	if value == "" || len(value) > MaxValueLength {
		return Observation{}, ErrValueRequired
	}
	return Observation{
		ID: newID, WorkspaceID: workspace,
		SubjectKind: subjectKind, SubjectValue: subjectValue,
		Field: field, Value: value,
		InvocationID: invocation, ArtifactID: artifact,
		MappingID: mapping, MappingVersion: version,
		ObservedAt: observedAt, RecordedAt: recordedAt,
	}, nil
}

// Unmapped is a leaf path the source emitted that no live mapping claimed.
//
// **It is a record, not a failure.** A tool that grew two fields after an
// upgrade has not started lying; it has started saying something nobody has
// taught this system to read. Inferring a field name from a JSON key is exactly
// the guess that turns an observation into a fact, one level down.
type Unmapped struct {
	ID           id.ID
	WorkspaceID  id.ID
	InvocationID id.ID
	ArtifactID   id.ID

	// Path is the leaf's spelling, the same one a mapping expression would use
	// — so the fix is copy-and-paste rather than translation.
	Path string

	// Seen is how many records in this artifact carried it. One occurrence in
	// ten thousand records is a different question from ten thousand.
	Seen int

	// Sample is ONE value, kept so a person can tell what the field is without
	// opening the artifact. It is capped hard and it is deliberately a sample
	// rather than a set: a distinct-value list is a summary of a field nobody
	// has decided to keep.
	Sample string

	RecordedAt time.Time
}

func NewUnmapped(newID, workspace, invocation, artifact id.ID,
	path, sample string, seen int, at time.Time) (Unmapped, error) {
	if newID.IsZero() || invocation.IsZero() || artifact.IsZero() {
		return Unmapped{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Unmapped{}, ErrWorkspaceRequired
	}
	if at.IsZero() {
		return Unmapped{}, ErrTimeRequired
	}
	path = strings.TrimSpace(path)
	if path == "" || len(path) > MaxPathLength {
		return Unmapped{}, ErrPathRequired
	}
	if seen < 1 {
		seen = 1
	}
	if len(sample) > 200 {
		sample = sample[:200]
	}
	return Unmapped{
		ID: newID, WorkspaceID: workspace, InvocationID: invocation,
		ArtifactID: artifact, Path: path, Seen: seen, Sample: sample,
		RecordedAt: at,
	}, nil
}

const (
	EventObservationsCreated = "extract.observation.created"
	EventFieldUnmapped       = "extract.field.unmapped"

	SubjectKind = "workspace"
)

// Created carries the three numbers the Extraction Quality panel is built on —
// a subscriber cannot compute them (0013), and they are the only measurement of
// the correction loop that exists.
type Created struct {
	WorkspaceID  string `json:"workspace_id"`
	InvocationID string `json:"invocation_id"`
	ArtifactID   string `json:"artifact_id"`
	Records      int    `json:"records"`
	FieldsSeen   int    `json:"fields_seen"`
	Mapped       int    `json:"mapped"`
	LeftAlone    int    `json:"left_alone"`
}

type FieldUnmapped struct {
	WorkspaceID  string   `json:"workspace_id"`
	InvocationID string   `json:"invocation_id"`
	ArtifactID   string   `json:"artifact_id"`
	Paths        []string `json:"paths"`
}

// Subject is a distinct thing observations have been made about, with how much
// is known. It stands in for an asset list until `fragment` exists — and it is
// a GROUP BY rather than a table on purpose, because that is exactly what a
// fragment will be a dedup of.
type Subject struct {
	Kind         string
	Value        string
	Observations int
	Fields       int
	LastSeen     time.Time
}

// Quality is the Extraction Quality panel's three numbers. They travel together
// because a ratio without its denominator is what 0011 refuses — and this is the
// only measurement of the correction loop that exists.
type Quality struct {
	// Mapped and LeftAlone are counted in PATHS, so that Seen() is a real
	// identity. Counting Mapped in observations made `FIELDS SEEN` wrong the
	// moment one flattened path produced two values — decisions/0035's own walk
	// found it.
	Mapped    int
	LeftAlone int

	// Observations is the other number, kept separately rather than conflated:
	// "how many paths became observations" and "how many observations" are
	// different questions and a flatten separates them.
	Observations int

	Fields int
}

// Seen is the denominator. It is computed rather than stored, so it cannot drift
// from its two parts — which is the whole reason those two are path counts.
func (q Quality) Seen() int { return q.Mapped + q.LeftAlone }

// SubjectSeen is one invocation having said something about one subject. It is
// what coverage joins against to catch DISCOVERY — a tool aimed at a seed
// produces subjects nobody aimed at, and the check that found them has plainly
// looked at them.
type SubjectSeen struct {
	InvocationID id.ID
	Kind         string
	Value        string
	At           time.Time
}
