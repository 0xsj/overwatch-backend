package root

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	runquery "github.com/0xsj/overwatch-backend/internal/run/app/query"
	rundomain "github.com/0xsj/overwatch-backend/internal/run/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type startRunRequest struct {
	TargetID string `json:"target_id"`
	CheckID  string `json:"check_id"`
}

type runResponse struct {
	RunID       string `json:"run_id"`
	WorkspaceID string `json:"workspace_id"`
	TargetID    string `json:"target_id"`
	CheckID     string `json:"check_id"`
	State       string `json:"state"`
	// Absent when a schedule started it. "No person" is a fact, not a missing
	// value, and `null` would be the client's cue to render it as unknown.
	StartedBy  string `json:"started_by,omitempty"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at,omitempty"`
}

type invocationResponse struct {
	InvocationID string   `json:"invocation_id"`
	StepID       string   `json:"step_id"`
	ToolID       string   `json:"tool_id"`
	Sequence     int      `json:"sequence"`
	State        string   `json:"state"`
	Argv         []string `json:"argv"`
	Binary       string   `json:"binary,omitempty"`

	// Exit is a POINTER. Absent means NO PROCESS EVER EXISTED — the client's
	// own fixture renders that as "no process ever started" rather than as a
	// blank, because a blank reads as data loss and a dash reads as unknown.
	Exit   *int   `json:"exit,omitempty"`
	Signal string `json:"signal,omitempty"`

	// Refusal names the RULE rather than apologising. A refusal is an answer.
	// RefusalRule is absent when NOTHING permitted the spawn, which is a
	// different fact from a rule having excluded it — decisions/0010.
	RefusalRule string `json:"refusal_rule,omitempty"`
	// PermitRule is the other half: which rule ALLOWED this spawn. Only one of
	// the two is ever set, and the lineage walks whichever it is.
	PermitRule     string `json:"permit_rule,omitempty"`
	Refusal        string `json:"refusal,omitempty"`
	SkippedBecause string `json:"skipped_because,omitempty"`
	Unavailable    string `json:"unavailable,omitempty"`

	StartedAt  string `json:"started_at,omitempty"`
	DurationMS int64  `json:"duration_ms"`

	Artifacts []artifactResponse `json:"artifacts"`
}

type artifactResponse struct {
	ArtifactID string `json:"artifact_id"`
	Stream     string `json:"stream"`
	Hash       string `json:"hash"`
	// Bytes is a plain number and NOT omitempty: zero means it ran and wrote an
	// empty artifact, which is a result. "Nothing was written" is the absence of
	// the whole object.
	Bytes     int64  `json:"bytes"`
	Truncated bool   `json:"truncated"`
	MediaType string `json:"media_type,omitempty"`
}

type runDetailResponse struct {
	runResponse
	Invocations []invocationResponse `json:"invocations"`
}

type runPageResponse struct {
	Runs []runResponse `json:"runs"`
	Next string        `json:"next,omitempty"`
}

func asRun(r rundomain.Run) runResponse {
	out := runResponse{
		RunID: r.ID.String(), WorkspaceID: r.WorkspaceID.String(),
		TargetID: r.TargetID.String(), CheckID: r.CheckID.String(),
		State: r.State.String(), StartedAt: r.StartedAt.UTC().Format(time.RFC3339Nano),
	}
	if !r.StartedBy.IsZero() {
		out.StartedBy = r.StartedBy.String()
	}
	if !r.FinishedAt.IsZero() {
		out.FinishedAt = r.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	return out
}

func asInvocation(i rundomain.Invocation, artifacts []rundomain.Artifact) invocationResponse {
	out := invocationResponse{
		InvocationID: i.ID.String(), StepID: i.StepID.String(), ToolID: i.ToolID.String(),
		Sequence: i.Sequence, State: i.Phase.String(), Argv: i.Argv, Binary: i.Binary,
		Signal: i.Signal, Refusal: i.RefusalReason, SkippedBecause: i.SkippedBecause,
		Unavailable: i.Unavailable, DurationMS: i.DurationMS,
		Artifacts: make([]artifactResponse, 0, len(artifacts)),
	}
	if i.HasExitCode {
		code := i.ExitCode
		out.Exit = &code
	}
	if !i.RefusalRule.IsZero() {
		out.RefusalRule = i.RefusalRule.String()
	}
	if !i.PermitRule.IsZero() {
		out.PermitRule = i.PermitRule.String()
	}
	if !i.StartedAt.IsZero() {
		out.StartedAt = i.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	for _, a := range artifacts {
		out.Artifacts = append(out.Artifacts, artifactResponse{
			ArtifactID: a.ID.String(), Stream: a.Stream.String(), Hash: a.Hash,
			Bytes: a.Bytes, Truncated: a.Truncated, MediaType: a.MediaType,
		})
	}
	return out
}

// startRun is the first endpoint in this product that causes a PROCESS to run.
//
// The gate is `write`, raised to `admin` when any step of the chain is loud —
// decisions/0033 §7 and 0019's ladder. Starting a passive run is the ordinary
// work of an engagement; authorising a loud scan against a client is not.
//
// It answers 202 with the PLAN. Nothing has spawned yet: the executor claims the
// run, which is what keeps a scan that takes minutes off a request that does not.
func (m *me) startRun(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in startRunRequest
	if !decodeBody(w, r, &in) {
		return
	}
	target, err := id.Parse(in.TargetID)
	if err != nil {
		httpx.Fail(m.log, w, r, rundomain.ErrTargetRequired)
		return
	}
	check, err := id.Parse(in.CheckID)
	if err != nil {
		httpx.Fail(m.log, w, r, rundomain.ErrCheckRequired)
		return
	}

	if !m.mayRunHere(w, r, workspace, check) {
		return
	}

	planned, err := m.runsCmd.Start(r.Context(), workspace, target, check, caller)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusAccepted, renderPlan(planned.Run, planned.Invocations))
}

// previewRun answers what the spawn gate WOULD say, without writing anything.
//
// This is the thing the client's fixture calls out as *"what n8n structurally
// cannot do, because n8n has no notion of scope"* — and it is the same function
// startRun uses, deliberately. Two implementations of one gate walk drift, and
// the one that drifts is the preview, which is what a person reads before
// authorising a scan against a client.
//
// It is a POST because it takes a body and is not cacheable, and it needs the
// same reach as starting: seeing which commands would run against a client is
// the disclosure, not the spawning.
func (m *me) previewRun(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in startRunRequest
	if !decodeBody(w, r, &in) {
		return
	}
	target, err := id.Parse(in.TargetID)
	if err != nil {
		httpx.Fail(m.log, w, r, rundomain.ErrTargetRequired)
		return
	}
	check, err := id.Parse(in.CheckID)
	if err != nil {
		httpx.Fail(m.log, w, r, rundomain.ErrCheckRequired)
		return
	}
	if !m.mayRunHere(w, r, workspace, check) {
		return
	}
	planned, err := m.runsCmd.Preview(r.Context(), workspace, target, check)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, renderPlan(planned.Run, planned.Invocations))
}

// mayRunHere raises the gate to `admin` when the chain contains a loud tool.
// The caller already holds `write`; this asks the second question, and it asks
// it from the chain rather than from a flag nobody maintains.
func (m *me) mayRunHere(w http.ResponseWriter, r *http.Request, workspace, check id.ID) bool {
	loud, err := m.runsCmd.Loud(r.Context(), workspace, check)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return false
	}
	if !loud {
		return true
	}
	if _, _, _, ok := m.onWorkspace(w, r, orgdomain.LevelAdmin); !ok {
		return false
	}
	return true
}

func renderPlan(of rundomain.Run, invocations []rundomain.Invocation) runDetailResponse {
	out := runDetailResponse{
		runResponse: asRun(of),
		Invocations: make([]invocationResponse, 0, len(invocations)),
	}
	for _, i := range invocations {
		out.Invocations = append(out.Invocations, asInvocation(i, nil))
	}
	return out
}

// listRuns is keyset-paged and answers NO TOTAL. This list only grows.
func (m *me) listRuns(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	var target id.ID
	if raw := r.URL.Query().Get("target"); raw != "" {
		parsed, err := id.Parse(raw)
		if err != nil {
			httpx.Fail(m.log, w, r, rundomain.ErrTargetRequired)
			return
		}
		target = parsed
	}
	before, beforeID := runCursor(r)
	size, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	// DEFAULT BEFORE THE +1. `size` is 0 when no limit is given, and `size+1`
	// is 1 — so the list returned exactly one run and reported no next page.
	// The query layer clamps a non-positive size, and 1 is positive, so nothing
	// downstream could catch it. Found by a scheduler walk that had started four
	// runs and could see one.
	if size <= 0 {
		size = runquery.DefaultPage
	}

	// One more than asked for, so "is there another page" is answered by the
	// read rather than by a count. A count over an append-only table is a scan
	// whose answer is stale before it renders.
	found, err := m.runs.Page(r.Context(), workspace, target, before, beforeID, size+1)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := runPageResponse{Runs: make([]runResponse, 0, len(found))}
	more := len(found) > size
	if more {
		found = found[:size]
	}
	for _, run := range found {
		out.Runs = append(out.Runs, asRun(run))
	}
	if more && len(found) > 0 {
		last := found[len(found)-1]
		out.Next = last.StartedAt.UTC().Format(time.RFC3339Nano) + "," + last.ID.String()
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

// readRun is one call for the whole Executions view, because that screen draws
// the graph at once and three round trips to fill one screen is three chances
// for the parts to disagree.
func (m *me) readRun(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("run"))
	if err != nil {
		httpx.Fail(m.log, w, r, rundomain.ErrNotFound)
		return
	}
	detail, err := m.runs.Detail(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := runDetailResponse{
		runResponse: asRun(detail.Run),
		Invocations: make([]invocationResponse, 0, len(detail.Invocations)),
	}
	for _, i := range detail.Invocations {
		out.Invocations = append(out.Invocations, asInvocation(i, detail.Artifacts[i.ID]))
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

// readArtifact streams the bytes, VERBATIM.
//
// The row is read first and that is the tenancy check. Reaching pkg/blob with a
// hash alone would let anybody who guessed a content address read another
// client's evidence — and a content address is exactly the kind of thing that
// ends up in a log.
//
// `Content-Disposition: attachment` and `X-Content-Type-Options: nosniff`
// because these bytes are a scanner's output pointed at a hostile target: served
// inline and sniffed, an artifact is stored XSS on the analyst's own origin.
func (m *me) readArtifact(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("artifact"))
	if err != nil {
		httpx.Fail(m.log, w, r, rundomain.ErrArtifactNotFound)
		return
	}
	found, body, err := m.runs.Open(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	defer body.Close()

	media := found.MediaType
	if media == "" {
		media = "application/octet-stream"
	}
	w.Header().Set("Content-Type", media)
	w.Header().Set("Content-Length", strconv.FormatInt(found.Bytes, 10))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+found.ID.String()+"\"")
	w.Header().Set("X-Artifact-Hash", found.Hash)
	if found.Truncated {
		// The cap is a fact about the artifact, and a reader that only has the
		// bytes cannot tell a short output from a truncated one.
		w.Header().Set("X-Artifact-Truncated", "true")
	}
	if _, err := io.Copy(w, body); err != nil {
		// The status is already sent, so this cannot become an error response.
		// Logging it is the only honest thing left.
		m.log.ErrorContext(r.Context(), "artifact stream cut short",
			"artifact", found.ID.String(), "cause", err)
	}
}

// listRefusals answers which spawns one scope rule refused. decisions/0030 keeps
// a rule append-only because three surfaces cite its id; this is the read that
// makes keeping it worth the rows.
func (m *me) listRefusals(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	rule, err := id.Parse(r.PathValue("rule"))
	if err != nil {
		httpx.WriteError(w, r, errors.New(errors.NotFound, "rule"))
		return
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	found, err := m.runs.Refusals(r.Context(), workspace, rule, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := make([]invocationResponse, 0, len(found))
	for _, i := range found {
		out = append(out, asInvocation(i, nil))
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

// runCursor reads the cursor a previous page handed back. A malformed one is
// treated as ABSENT rather than refused, the same rule the audit ledger uses:
// the worst it can do is start the reader at the head.
func runCursor(r *http.Request) (time.Time, id.ID) {
	stamp, rest, ok := strings.Cut(r.URL.Query().Get("after"), ",")
	if !ok {
		return time.Time{}, id.ID{}
	}
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return time.Time{}, id.ID{}
	}
	last, err := id.Parse(rest)
	if err != nil {
		return time.Time{}, id.ID{}
	}
	return at, last
}
