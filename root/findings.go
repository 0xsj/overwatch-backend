package root

import (
	"net/http"
	"strconv"
	"time"

	findingdomain "github.com/0xsj/overwatch-backend/internal/finding/domain"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// findingResponse is `0004`'s shape on the wire: `severity` is a SCALAR beside
// its claim rather than nested inside it, because it is the sort key and the
// filter key for the board.
type findingResponse struct {
	FindingID string `json:"finding_id"`
	ToolID    string `json:"tool_id"`

	// Signature is what the TOOL calls this class of problem. It is half the
	// identity — decisions/0041 §1 — and it is what makes a rescan a sighting
	// rather than a new row.
	Signature string `json:"signature"`

	// The fragment the problem is ON, carried whole so a board renders without
	// a second request.
	FragmentID    string `json:"fragment_id"`
	FragmentKind  string `json:"fragment_kind"`
	FragmentValue string `json:"fragment_value"`

	State string `json:"state"`
	// Reason is required on a dismissal and optional on a resolution — a fix
	// needs no argument, because the thing is gone.
	Reason    string `json:"reason,omitempty"`
	DecidedBy string `json:"decided_by,omitempty"`
	DecidedAt string `json:"decided_at,omitempty"`

	Severity   string             `json:"severity"`
	SeverityBy assessmentResponse `json:"severity_by"`

	// FirstSeen, LastSeen and Sightings are the history one row carries instead
	// of one row per scan. Without a `regressed` state they are also the only
	// evidence that a fix did not hold — an old first_seen beside a large
	// sightings count.
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
	Sightings int    `json:"sightings"`

	// The run that MOST RECENTLY saw it. Never absent: a finding this system
	// cannot source would arrive looking trustworthy.
	InvocationID string `json:"invocation_id"`
	ArtifactID   string `json:"artifact_id"`
}

// assessmentResponse is `0004`'s `severity_by`, and the ABSENCES are the point.
// `confidence` is omitted for a rule and a human because only machines carry
// one; `superseded` is omitted unless a claimant overrode another.
type assessmentResponse struct {
	Claimant string `json:"claimant"`
	Actor    string `json:"actor,omitempty"`
	// A POINTER, so `null` never stands in for "this claimant does not carry a
	// confidence". Absent is the claim.
	Confidence *float64 `json:"confidence,omitempty"`
	Basis      string   `json:"basis,omitempty"`
	At         string   `json:"at"`

	Superseded *supersededResponse `json:"superseded,omitempty"`
}

type supersededResponse struct {
	Severity   string   `json:"severity"`
	Claimant   string   `json:"claimant"`
	Actor      string   `json:"actor,omitempty"`
	Confidence *float64 `json:"confidence,omitempty"`
	Basis      string   `json:"basis,omitempty"`
	At         string   `json:"at"`
}

type findingDetailResponse struct {
	findingResponse
	Details []detailResponse `json:"details"`
}

type detailResponse struct {
	Field      string `json:"field"`
	Value      string `json:"value"`
	MappingID  string `json:"mapping_id"`
	ArtifactID string `json:"artifact_id"`
}

func asAssessment(a findingdomain.Assessment) assessmentResponse {
	out := assessmentResponse{
		Claimant: a.Claimant.String(), Basis: a.Basis,
		At: a.At.UTC().Format(time.RFC3339Nano),
	}
	if !a.Actor.IsZero() {
		out.Actor = a.Actor.String()
	}
	if a.HasConfidence {
		c := a.Confidence
		out.Confidence = &c
	}
	return out
}

func asFinding(f findingdomain.Finding) findingResponse {
	out := findingResponse{
		FindingID: f.ID.String(), ToolID: f.ToolID.String(), Signature: f.Signature,
		FragmentID: f.FragmentID.String(), FragmentKind: f.FragmentKind,
		FragmentValue: f.FragmentValue,
		State:         f.State.String(), Reason: f.Reason,
		Severity: f.Severity.String(), SeverityBy: asAssessment(f.Assessment),
		FirstSeen:    f.FirstSeen.UTC().Format(time.RFC3339Nano),
		LastSeen:     f.LastSeen.UTC().Format(time.RFC3339Nano),
		Sightings:    f.Sightings,
		InvocationID: f.Invocation.String(), ArtifactID: f.Artifact.String(),
	}
	if !f.DecidedBy.IsZero() {
		out.DecidedBy = f.DecidedBy.String()
		out.DecidedAt = f.DecidedAt.UTC().Format(time.RFC3339Nano)
	}
	if f.Superseded != nil {
		prior := asAssessment(*f.Superseded)
		out.SeverityBy.Superseded = &supersededResponse{
			Severity: f.SupersededSeverity.String(), Claimant: prior.Claimant,
			Actor: prior.Actor, Confidence: prior.Confidence,
			Basis: prior.Basis, At: prior.At,
		}
	}
	return out
}

// listFindings is the board. `state` empty means EVERY state, closed ones
// included — a report cites what was resolved and what was dismissed as well as
// what is open.
func (m *me) listFindings(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	var fragment id.ID
	if raw := r.URL.Query().Get("fragment"); raw != "" {
		parsed, err := id.Parse(raw)
		if err != nil {
			httpx.WriteJSON(w, r, http.StatusOK, []findingResponse{})
			return
		}
		fragment = parsed
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	found, err := m.findings.Board(r.Context(), workspace,
		r.URL.Query().Get("state"), fragment, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := make([]findingResponse, 0, len(found))
	for _, f := range found {
		out = append(out, asFinding(f))
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

func (m *me) readFinding(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("finding"))
	if err != nil {
		httpx.Fail(m.log, w, r, findingdomain.ErrNotFound)
		return
	}
	detail, err := m.findings.Detail(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := findingDetailResponse{
		findingResponse: asFinding(detail.Finding),
		Details:         make([]detailResponse, 0, len(detail.Details)),
	}
	for _, d := range detail.Details {
		out.Details = append(out.Details, detailResponse{
			Field: d.Field, Value: d.Value,
			MappingID: d.MappingID.String(), ArtifactID: d.ArtifactID.String(),
		})
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

type decideFindingRequest struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
}

// decideFinding is a PERSON ruling on a client's problem. `write` and not
// `read`, and there is deliberately no system path: nothing closes a finding
// automatically, because absence of a match is not evidence of a fix.
func (m *me) decideFinding(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("finding"))
	if err != nil {
		httpx.Fail(m.log, w, r, findingdomain.ErrNotFound)
		return
	}
	var in decideFindingRequest
	if !decodeBody(w, r, &in) {
		return
	}
	to, err := findingdomain.ParseState(in.State)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	next, err := m.findingsCmd.Decide(r.Context(), workspace, want, caller, to, in.Reason)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asFinding(next))
}

type reassessRequest struct {
	Severity string `json:"severity"`
	Basis    string `json:"basis"`
}

// reassessFinding overrides a severity and KEEPS what it overrode — `0004`. A
// basis is required, because replacing somebody else's assessment is a
// disagreement and one with no stated reason records that somebody disagreed
// without saying why they were right.
func (m *me) reassessFinding(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("finding"))
	if err != nil {
		httpx.Fail(m.log, w, r, findingdomain.ErrNotFound)
		return
	}
	var in reassessRequest
	if !decodeBody(w, r, &in) {
		return
	}
	to, err := findingdomain.ParseSeverity(in.Severity)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	next, err := m.findingsCmd.Reassess(r.Context(), workspace, want, caller, to, in.Basis)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asFinding(next))
}
