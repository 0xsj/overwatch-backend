package root

import (
	"net/http"
	"strconv"
	"time"

	obsquery "github.com/0xsj/overwatch-backend/internal/observation/app/query"
	obsdomain "github.com/0xsj/overwatch-backend/internal/observation/domain"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type observationResponse struct {
	ObservationID string `json:"observation_id"`
	SubjectKind   string `json:"subject_kind"`
	SubjectValue  string `json:"subject_value"`
	Field         string `json:"field"`
	Value         string `json:"value"`

	InvocationID   string `json:"invocation_id"`
	ArtifactID     string `json:"artifact_id"`
	MappingID      string `json:"mapping_id"`
	MappingVersion int    `json:"mapping_version"`

	// Both times, always. `observed_at` is when the TOOL ran and `recorded_at`
	// is when this row was written; a re-extraction moves the second and never
	// the first, and a client showing only one cannot tell them apart.
	ObservedAt string `json:"observed_at"`
	RecordedAt string `json:"recorded_at"`
}

type unmappedResponse struct {
	Path   string `json:"path"`
	Seen   int    `json:"seen"`
	Sample string `json:"sample,omitempty"`
}

// qualityResponse is the Extraction Quality panel. The three numbers travel
// TOGETHER because a ratio without its denominator is what `0011` refuses — and
// `fields_seen` is computed from the other two so it cannot drift.
type qualityResponse struct {
	// FIELDS SEEN = MAPPED + LEFT ALONE, and all three are counted in PATHS so
	// that identity holds. `observations` is the separate question — one
	// flattened path can become many observations.
	FieldsSeen   int `json:"fields_seen"`
	Mapped       int `json:"mapped"`
	LeftAlone    int `json:"left_alone"`
	Observations int `json:"observations"`
	Fields       int `json:"fields"`
}

type subjectResponse struct {
	SubjectKind  string `json:"subject_kind"`
	SubjectValue string `json:"subject_value"`
	Observations int    `json:"observations"`
	Fields       int    `json:"fields"`
	LastSeen     string `json:"last_seen"`
}

func asObservation(o obsdomain.Observation) observationResponse {
	return observationResponse{
		ObservationID: o.ID.String(), SubjectKind: o.SubjectKind,
		SubjectValue: o.SubjectValue, Field: o.Field, Value: o.Value,
		InvocationID: o.InvocationID.String(), ArtifactID: o.ArtifactID.String(),
		MappingID: o.MappingID.String(), MappingVersion: o.MappingVersion,
		ObservedAt: o.ObservedAt.UTC().Format(time.RFC3339Nano),
		RecordedAt: o.RecordedAt.UTC().Format(time.RFC3339Nano),
	}
}

// listSubjects stands in for an asset list until `fragment` exists. It is a
// GROUP BY over observations rather than a table, which is deliberate: that is
// exactly what a fragment will be a dedup of.
func (m *me) listSubjects(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	found, err := m.observed.Subjects(r.Context(), workspace, limit)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := make([]subjectResponse, 0, len(found))
	for _, s := range found {
		out = append(out, subjectResponse{
			SubjectKind: s.Kind, SubjectValue: s.Value,
			Observations: s.Observations, Fields: s.Fields,
			LastSeen: s.LastSeen.UTC().Format(time.RFC3339Nano),
		})
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

// listObservations is the asset drawer's read when `subject` is given, and the
// run detail's when `invocation` is.
//
// **It does not deduplicate.** Two runs a day apart are two statements and
// collapsing them loses the second date; "the state of each field" is the first
// row per field, which the ordering already hands the client.
func (m *me) listObservations(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))

	var (
		found []obsdomain.Observation
		err   error
	)
	switch {
	case q.Get("invocation") != "":
		invocation, parseErr := id.Parse(q.Get("invocation"))
		if parseErr != nil {
			httpx.Fail(m.log, w, r, obsdomain.ErrNotFound)
			return
		}
		found, err = m.observed.ForInvocation(r.Context(), workspace, invocation, limit)
	case q.Get("subject") != "":
		found, err = m.observed.ForSubject(r.Context(), workspace,
			q.Get("kind"), q.Get("subject"), limit)
	default:
		httpx.Fail(m.log, w, r, obsdomain.ErrSubjectRequired)
		return
	}
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := make([]observationResponse, 0, len(found))
	for _, o := range found {
		out = append(out, asObservation(o))
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

// readExtraction is *"how much of what the tools printed actually became an
// observation"* — the number the compounding loop lives or dies on, and one
// nobody has ever recorded.
//
// The unmapped paths come back with it, because a ratio a person cannot act on
// is a metric rather than a tool: the paths ARE the work.
func (m *me) readExtraction(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	invocation, err := id.Parse(r.PathValue("invocation"))
	if err != nil {
		httpx.Fail(m.log, w, r, obsdomain.ErrNotFound)
		return
	}
	quality, err := m.observed.Quality(r.Context(), workspace, invocation)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	left, err := m.observed.Unmapped(r.Context(), workspace, invocation)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	paths := make([]unmappedResponse, 0, len(left))
	for _, u := range left {
		paths = append(paths, unmappedResponse{Path: u.Path, Seen: u.Seen, Sample: u.Sample})
	}
	httpx.WriteJSON(w, r, http.StatusOK, struct {
		qualityResponse
		LeftAlonePaths []unmappedResponse `json:"left_alone_paths"`
	}{
		qualityResponse: qualityResponse{
			FieldsSeen: quality.Seen(), Mapped: quality.Mapped,
			LeftAlone: quality.LeftAlone, Observations: quality.Observations,
			Fields: quality.Fields,
		},
		LeftAlonePaths: paths,
	})
}

type lineageResponse struct {
	Observation observationResponse `json:"observation"`

	// Each step is a POINTER and absent when genuinely missing. An invocation
	// that nothing refused has no rule to cite, and rendering that as an error
	// would make the commonest case look broken.
	Mapping    *lineageMapping    `json:"mapping,omitempty"`
	Artifact   *lineageArtifact   `json:"artifact,omitempty"`
	Invocation *lineageInvocation `json:"invocation,omitempty"`
	Rule       *lineageRule       `json:"rule,omitempty"`
}

type lineageMapping struct {
	MappingID  string `json:"mapping_id"`
	Field      string `json:"field"`
	Expression string `json:"expression"`
	Version    int    `json:"version"`
	State      string `json:"state"`
}

type lineageArtifact struct {
	ArtifactID string `json:"artifact_id"`
	Stream     string `json:"stream"`
	Hash       string `json:"hash"`
	Bytes      int64  `json:"bytes"`
	Truncated  bool   `json:"truncated"`
}

type lineageInvocation struct {
	InvocationID string   `json:"invocation_id"`
	RunID        string   `json:"run_id"`
	ToolID       string   `json:"tool_id"`
	Argv         []string `json:"argv"`
	Binary       string   `json:"binary,omitempty"`
	State        string   `json:"state"`
	Exit         *int     `json:"exit,omitempty"`
	StartedAt    string   `json:"started_at,omitempty"`
}

type lineageRule struct {
	RuleID   string `json:"rule_id"`
	Pattern  string `json:"pattern"`
	Polarity string `json:"polarity"`
	Gate     string `json:"gate"`
	// Superseded is TRUE and the rule is still returned. 0030 keeps a rule
	// forever precisely so a citation made at the time still resolves — a
	// lineage that dropped it would be the citation dangling.
	Superseded bool `json:"superseded"`
}

// readLineage is PRODUCT.md's central claim, as an endpoint:
//
//	"Every value walks backwards to the parser version, the raw bytes, the
//	 exact command, and the scope rule that allowed the command to run."
//
// The observation is read first and that is the tenancy check; every other step
// is reached through ids the observation itself carries.
func (m *me) readLineage(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("observation"))
	if err != nil {
		httpx.Fail(m.log, w, r, obsdomain.ErrNotFound)
		return
	}
	walked, err := m.observed.Lineage(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asLineage(walked))
}

func asLineage(l obsquery.Lineage) lineageResponse {
	out := lineageResponse{Observation: asObservation(l.Observation)}
	if l.Mapping != nil {
		out.Mapping = &lineageMapping{
			MappingID: l.Mapping.MappingID.String(), Field: l.Mapping.Field,
			Expression: l.Mapping.Expression, Version: l.Mapping.Version,
			State: l.Mapping.State,
		}
	}
	if l.Artifact != nil {
		out.Artifact = &lineageArtifact{
			ArtifactID: l.Artifact.ArtifactID.String(), Stream: l.Artifact.Stream,
			Hash: l.Artifact.Hash, Bytes: l.Artifact.Bytes, Truncated: l.Artifact.Truncated,
		}
	}
	if l.Invocation != nil {
		step := &lineageInvocation{
			InvocationID: l.Invocation.InvocationID.String(),
			RunID:        l.Invocation.RunID.String(),
			ToolID:       l.Invocation.ToolID.String(),
			Argv:         l.Invocation.Argv, Binary: l.Invocation.Binary,
			State: l.Invocation.Phase, Exit: l.Invocation.ExitCode,
		}
		if !l.Invocation.StartedAt.IsZero() {
			step.StartedAt = l.Invocation.StartedAt.UTC().Format(time.RFC3339Nano)
		}
		out.Invocation = step
	}
	if l.Rule != nil {
		out.Rule = &lineageRule{
			RuleID: l.Rule.RuleID.String(), Pattern: l.Rule.Pattern,
			Polarity: l.Rule.Polarity, Gate: l.Rule.Gate, Superseded: l.Rule.Superseded,
		}
	}
	return out
}
