package root

import (
	"net/http"
	"strconv"
	"time"

	entdomain "github.com/0xsj/overwatch-backend/internal/entity/domain"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// judgementResponse is the value object 0009 put on two tables. It is one shape
// on the wire because it is one shape in the domain — the duplication is in the
// columns, not in the meaning.
type judgementResponse struct {
	State string `json:"state"`
	// By, At and Reason are ABSENT while unopened. Nobody has ruled, so there is
	// nobody and no time to name.
	By     string `json:"by,omitempty"`
	At     string `json:"at,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type fragmentResponse struct {
	FragmentID string `json:"fragment_id"`
	Kind       string `json:"kind"`
	Value      string `json:"value"`
	// Origin is `observed` or `manual` — decisions/0036, resolving 0009's own
	// recorded doubt about a `/24` somebody typed in.
	Origin string `json:"origin"`
	// FirstSeen and LastSeen are ABSENT on a manual fragment. Nothing has seen
	// it, and a date there would be a zero nothing computed.
	FirstSeen    string            `json:"first_seen,omitempty"`
	LastSeen     string            `json:"last_seen,omitempty"`
	Observations int               `json:"observations"`
	Judgement    judgementResponse `json:"judgement"`

	// ReadAt and ReadBy are a HUMAN READ and are NOT the judgement — 0037.
	// Absent means nobody has looked, which is a different fact from nobody
	// having ruled.
	ReadAt string `json:"read_at,omitempty"`
	ReadBy string `json:"read_by,omitempty"`
}

type attributionResponse struct {
	AttributionID string `json:"attribution_id"`
	EntityID      string `json:"entity_id"`
	FragmentID    string `json:"fragment_id"`
	// Claimant is who proposed it and is NEVER rewritten — decisions/0008.
	Claimant    string `json:"claimant"`
	ClaimantRef string `json:"claimant_ref,omitempty"`
	// Confidence is ABSENT unless the claimant is a model. A rule's assignment
	// is a category, not a probability, and a 1.0 here would destroy the
	// distinction permanently.
	Confidence *float64 `json:"confidence,omitempty"`
	Basis      string   `json:"basis"`
	State      string   `json:"state"`
	DecidedAt  string   `json:"decided_at,omitempty"`
	// DecidedBy is ABSENT when a RULE decided — decisions/0036. Accepted with no
	// decider is not a missing field; it is "no person ruled on this".
	DecidedBy   string `json:"decided_by,omitempty"`
	DecidedNote string `json:"decided_note,omitempty"`
}

type entityResponse struct {
	EntityID string `json:"entity_id"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	// TargetID is present only on a target's ROOT entity.
	TargetID  string            `json:"target_id,omitempty"`
	Judgement judgementResponse `json:"judgement"`
}

// assetResponse is a fragment IN A ROLE — 0009. It is deliberately the fragment
// shape plus the claim that makes it one, rather than a separate noun, because
// an asset is not a different kind of thing.
type assetResponse struct {
	fragmentResponse
	AttributionID string `json:"attribution_id"`
	Claimant      string `json:"claimant"`
	Basis         string `json:"basis"`
	RootEntityID  string `json:"root_entity_id"`
	TargetID      string `json:"target_id"`
}

func asJudgement(j entdomain.Judgement) judgementResponse {
	out := judgementResponse{State: j.State.String(), Reason: j.Reason}
	if !j.By.IsZero() {
		out.By = j.By.String()
	}
	if !j.At.IsZero() {
		out.At = j.At.UTC().Format(time.RFC3339Nano)
	}
	return out
}

func asFragment(f entdomain.Fragment) fragmentResponse {
	out := fragmentResponse{
		FragmentID: f.ID.String(), Kind: f.Kind, Value: f.Value,
		Origin: f.Origin.String(), Observations: f.Observations,
		Judgement: asJudgement(f.Judgement),
	}
	if !f.FirstSeen.IsZero() {
		out.FirstSeen = f.FirstSeen.UTC().Format(time.RFC3339Nano)
	}
	if !f.LastSeen.IsZero() {
		out.LastSeen = f.LastSeen.UTC().Format(time.RFC3339Nano)
	}
	if f.HasBeenRead() {
		out.ReadAt = f.ReadAt.UTC().Format(time.RFC3339Nano)
		out.ReadBy = f.ReadBy.String()
	}
	return out
}

func asAttribution(a entdomain.Attribution) attributionResponse {
	out := attributionResponse{
		AttributionID: a.ID.String(), EntityID: a.EntityID.String(),
		FragmentID: a.FragmentID.String(), Claimant: a.Claimant.String(),
		Basis: a.Basis, State: a.State.String(), DecidedNote: a.DecidedNote,
	}
	if !a.ClaimantRef.IsZero() {
		out.ClaimantRef = a.ClaimantRef.String()
	}
	if a.HasConfidence {
		c := a.Confidence
		out.Confidence = &c
	}
	if !a.DecidedAt.IsZero() {
		out.DecidedAt = a.DecidedAt.UTC().Format(time.RFC3339Nano)
	}
	if !a.DecidedBy.IsZero() {
		out.DecidedBy = a.DecidedBy.String()
	}
	return out
}

func asEntity(e entdomain.Entity) entityResponse {
	out := entityResponse{
		EntityID: e.ID.String(), Kind: e.Kind, Label: e.Label,
		Judgement: asJudgement(e.Judgement),
	}
	if !e.TargetID.IsZero() {
		out.TargetID = e.TargetID.String()
	}
	return out
}

// listAssets reads the VIEW, which is the point of the view — decisions/0009
// asked for the word and the query to be the same object.
func (m *me) listAssets(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	var target id.ID
	if raw := r.URL.Query().Get("target"); raw != "" {
		parsed, err := id.Parse(raw)
		if err != nil {
			httpx.Fail(m.log, w, r, entdomain.ErrIDRequired)
			return
		}
		target = parsed
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	found, err := m.graph.Assets(r.Context(), workspace, target, limit)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := make([]assetResponse, 0, len(found))
	for _, a := range found {
		out = append(out, assetResponse{
			fragmentResponse: asFragment(a.Fragment),
			AttributionID:    a.AttributionID.String(), Claimant: a.Claimant.String(),
			Basis: a.Basis, RootEntityID: a.RootEntityID.String(),
			TargetID: a.TargetID.String(),
		})
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

// listFragments is EVERYTHING observed, asset or not. The difference between
// this and the asset list is the whole of 0009: a fragment nothing attributed is
// a real record of something a source said, and it is not an asset.
func (m *me) listFragments(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	found, err := m.graph.Fragments(r.Context(), workspace, r.URL.Query().Get("kind"), limit)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := make([]fragmentResponse, 0, len(found))
	for _, f := range found {
		out = append(out, asFragment(f))
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

// readFragment is the asset drawer's header plus its WHY THIS IS ATTRIBUTED
// section — the claims, with who proposed each and whether anybody has agreed.
func (m *me) readFragment(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("fragment"))
	if err != nil {
		httpx.Fail(m.log, w, r, entdomain.ErrFragmentNotFound)
		return
	}
	found, err := m.graph.Fragment(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	claims, err := m.graph.Attributions(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := struct {
		fragmentResponse
		Attributions []attributionResponse `json:"attributions"`
	}{fragmentResponse: asFragment(found)}
	out.Attributions = make([]attributionResponse, 0, len(claims))
	for _, c := range claims {
		out.Attributions = append(out.Attributions, asAttribution(c))
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

func (m *me) listEntities(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	found, err := m.graph.Entities(r.Context(), workspace, limit)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := make([]entityResponse, 0, len(found))
	for _, e := range found {
		out = append(out, asEntity(e))
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

// readCanvas is the entity graph. **The root is NOT among the nodes** — it is
// not a fragment, every attribution runs from it to one, and the client's own
// fixture draws it apart for exactly that reason.
func (m *me) readCanvas(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("entity"))
	if err != nil {
		httpx.Fail(m.log, w, r, entdomain.ErrNotFound)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	canvas, err := m.graph.Around(r.Context(), workspace, want, r.URL.Query().Get("state"), limit)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	type node struct {
		fragmentResponse
		Edge attributionResponse `json:"edge"`
	}
	out := struct {
		Root  entityResponse `json:"root"`
		Nodes []node         `json:"nodes"`
		// Truncated says the limit was reached. A canvas that silently drew half
		// a graph would look like a smaller estate, which is the one way this
		// screen can lie.
		Truncated bool `json:"truncated"`
	}{Root: asEntity(canvas.Root), Truncated: canvas.Truncated}
	out.Nodes = make([]node, 0, len(canvas.Nodes))
	for _, n := range canvas.Nodes {
		out.Nodes = append(out.Nodes, node{
			fragmentResponse: asFragment(n.Fragment), Edge: asAttribution(n.Edge),
		})
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

// coverageResponse is `CLAUDE.md`'s answerable question. The two deficits are
// TWO NUMBERS and are never summed — 0011: "never and stale are different
// failures, nobody asked versus the answer is old", and the mock's own caption
// already dropped one of them.
//
// There is NO PERCENTAGE. `fresh` and `pairs` are both here and the client does
// its own arithmetic, because a `0/0` rounded to `0%` is exactly the zero
// nothing computed that §Scope refuses.
type coverageResponse struct {
	Assets int `json:"assets"`
	// Pairs is the RAGGED denominator, and it is a claim: it asserts that this
	// many questions exist. It is never assets x checks.
	Pairs int           `json:"pairs"`
	Fresh int           `json:"fresh"`
	Stale int           `json:"stale"`
	Never int           `json:"never"`
	Rows  []coverageRow `json:"rows"`
}

type coverageRow struct {
	FragmentID string `json:"fragment_id"`
	Kind       string `json:"kind"`
	Value      string `json:"value"`
	// Cells carries ONLY the applicable checks. An inapplicable pair is not a
	// cell — 0011: "n/a renders as no square" — so the grid is ragged and a
	// client that right-pads it with dashed squares has re-introduced the bug
	// this whole record exists to remove.
	Cells []coverageCell `json:"cells"`
}

type coverageCell struct {
	CheckID   string `json:"check_id"`
	CheckName string `json:"check_name"`
	State     string `json:"state"`
	// At is absent when never. For the human check it is when somebody READ it,
	// which is not when they ruled on it.
	At string `json:"at,omitempty"`
}

// readCoverage answers "what have I not looked at" — the question PRODUCT.md
// says is not answerable in any competitor.
func (m *me) readCoverage(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	var target id.ID
	if raw := r.URL.Query().Get("target"); raw != "" {
		parsed, err := id.Parse(raw)
		if err != nil {
			httpx.Fail(m.log, w, r, entdomain.ErrIDRequired)
			return
		}
		target = parsed
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	got, err := m.graph.Compute(r.Context(), workspace, target, time.Now(), limit)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	got.Sort()
	out := coverageResponse{
		Assets: got.Summary.Assets, Pairs: got.Summary.Pairs,
		Fresh: got.Summary.Fresh, Stale: got.Summary.Stale, Never: got.Summary.Never,
		Rows: make([]coverageRow, 0, len(got.Rows)),
	}
	for _, row := range got.Rows {
		cells := make([]coverageCell, 0, len(row.Cells))
		for _, c := range row.Cells {
			cell := coverageCell{
				CheckID: c.CheckID.String(), CheckName: c.CheckName, State: c.State.String(),
			}
			if !c.At.IsZero() {
				cell.At = c.At.UTC().Format(time.RFC3339Nano)
			}
			cells = append(cells, cell)
		}
		out.Rows = append(out.Rows, coverageRow{
			FragmentID: row.Asset.ID.String(), Kind: row.Asset.Kind,
			Value: row.Asset.Value, Cells: cells,
		})
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

// markRead records that a person LOOKED. It is the `READ BY YOU` coverage row,
// and it deliberately does NOT touch the judgement — decisions/0037 §3.
//
// `0011`: READ BY YOU being a check is "what keeps never read and no judgement
// separable". A client that wants both makes two calls, because they are two
// acts: a person can read something and decline to rule on it, which is the
// commonest thing an analyst does.
func (m *me) markRead(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("fragment"))
	if err != nil {
		httpx.Fail(m.log, w, r, entdomain.ErrFragmentNotFound)
		return
	}
	read, err := m.rulings.MarkRead(r.Context(), workspace, want, caller)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asFragment(read))
}

type judgeRequest struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
}

// judgeFragment is the write that makes "what have I never looked at" an
// answerable question — PRODUCT.md's third differentiator, and the one it says
// is not answerable in any competitor.
//
// `write` on the engagement: recording that a person looked is ordinary work.
func (m *me) judgeFragment(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("fragment"))
	if err != nil {
		httpx.Fail(m.log, w, r, entdomain.ErrFragmentNotFound)
		return
	}
	var in judgeRequest
	if !decodeBody(w, r, &in) {
		return
	}
	state, err := entdomain.ParseJudgement(in.State)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	ruled, err := m.rulings.JudgeFragment(r.Context(), workspace, want, caller, state, in.Reason)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asFragment(ruled))
}

func (m *me) judgeEntity(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("entity"))
	if err != nil {
		httpx.Fail(m.log, w, r, entdomain.ErrNotFound)
		return
	}
	var in judgeRequest
	if !decodeBody(w, r, &in) {
		return
	}
	state, err := entdomain.ParseJudgement(in.State)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	ruled, err := m.rulings.JudgeEntity(r.Context(), workspace, want, caller, state, in.Reason)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asEntity(ruled))
}

type decideRequest struct {
	State string `json:"state"`
	Note  string `json:"note"`
}

// decideAttribution is a person ruling on a proposed claim. It NEVER rewrites
// the claimant — decisions/0008's title — and today only a model proposes, so
// this has no producer of work yet: it is built because the review queue is what
// makes a model's claims safe to accept at all.
//
// `admin`, not `write`: accepting a claim puts something on a client's asset
// list, and that is nearer editing scope than it is to ordinary work.
func (m *me) decideAttribution(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelAdmin)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("attribution"))
	if err != nil {
		httpx.Fail(m.log, w, r, entdomain.ErrAttributionNotFound)
		return
	}
	var in decideRequest
	if !decodeBody(w, r, &in) {
		return
	}
	state, err := entdomain.ParseClaimState(in.State)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	ruled, err := m.rulings.Decide(r.Context(), workspace, want, caller, state, in.Note)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asAttribution(ruled))
}
