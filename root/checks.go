package root

import (
	"context"
	"net/http"
	"time"

	checkcmd "github.com/0xsj/overwatch-backend/internal/check/app/command"
	checkdomain "github.com/0xsj/overwatch-backend/internal/check/domain"
	toolquery "github.com/0xsj/overwatch-backend/internal/tool/app/query"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type checkRequest struct {
	Name      string   `json:"name"`
	Question  string   `json:"question"`
	AppliesTo []string `json:"applies_to"`

	// IntervalSeconds is 0 or absent for "when somebody asks", which is a kind
	// of check rather than an unset field.
	//
	// SECONDS and not an ISO-8601 duration, deliberately: `P1M` is not a fixed
	// length of time, and a check interval that means "a month" is ambiguous at
	// exactly the boundary where staleness is decided. See ALIGNMENT.
	IntervalSeconds int  `json:"interval_seconds"`
	Enabled         bool `json:"enabled"`

	// Human is `READ BY YOU`: a person reading the thing is the whole act, so
	// nothing spawns. It is a FLAG and not "has no chain" — a check nobody has
	// wired a chain to yet is also chainless, and deriving it made an unfinished
	// check report coverage it did not have (0037 §3).
	Human bool `json:"human"`
}

type checkResponse struct {
	CheckID   string   `json:"check_id"`
	OrgID     string   `json:"org_id"`
	Name      string   `json:"name"`
	Question  string   `json:"question"`
	AppliesTo []string `json:"applies_to"`
	// Omitted when the check runs on demand. Absent means "no clock", and a
	// zero would read as an interval of no length.
	IntervalSeconds int    `json:"interval_seconds,omitempty"`
	Enabled         bool   `json:"enabled"`
	Human           bool   `json:"human"`
	Archived        bool   `json:"archived"`
	CreatedAt       string `json:"created_at"`
}

type stepRequest struct {
	// StepID is empty for a new step. A browser cannot mint an id and should
	// not have to.
	StepID string `json:"step_id"`
	ToolID string `json:"tool_id"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Pinned bool   `json:"pinned"`
}

// flowRequest names endpoints by id, or by INDEX into the step list when the
// endpoint is a step being created in this same save. The editor draws an edge
// between two nodes it has just added, and neither has an id yet.
type flowRequest struct {
	From    string `json:"from"`
	To      string `json:"to"`
	FromNew *int   `json:"from_new"`
	ToNew   *int   `json:"to_new"`
}

type chainRequest struct {
	Steps []stepRequest `json:"steps"`
	Flows []flowRequest `json:"flows"`
}

type chainStepResponse struct {
	StepID string `json:"step_id"`
	ToolID string `json:"tool_id"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Pinned bool   `json:"pinned"`
}

type chainFlowResponse struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type chainResponse struct {
	Steps []chainStepResponse `json:"steps"`
	Flows []chainFlowResponse `json:"flows"`
	// Sources are the steps nothing feeds — seeded from the target's scope
	// rather than from another tool. Computed here so the editor does not
	// reimplement it and disagree.
	Sources []string `json:"sources"`
}

func asCheck(c checkdomain.Check) checkResponse {
	return checkResponse{
		CheckID: c.ID.String(), OrgID: c.OrgID.String(), Name: c.Name,
		Question: c.Question, AppliesTo: checkdomain.Names(c.AppliesTo),
		IntervalSeconds: int(c.Interval / time.Second),
		Enabled:         c.Enabled, Archived: c.Archived(),
		CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func asChain(c checkdomain.Chain) chainResponse {
	out := chainResponse{
		Steps:   make([]chainStepResponse, 0, len(c.Steps)),
		Flows:   make([]chainFlowResponse, 0, len(c.Flows)),
		Sources: make([]string, 0, len(c.Steps)),
	}
	for _, s := range c.Steps {
		out.Steps = append(out.Steps, chainStepResponse{
			StepID: s.ID.String(), ToolID: s.ToolID.String(),
			X: s.X, Y: s.Y, Pinned: s.Pinned,
		})
	}
	for _, f := range c.Flows {
		out.Flows = append(out.Flows, chainFlowResponse{From: f.From.String(), To: f.To.String()})
	}
	for _, s := range c.Sources() {
		out.Sources = append(out.Sources, s.ID.String())
	}
	return out
}

func checkDraft(in checkRequest) (checkdomain.Draft, error) {
	applies := make([]checkdomain.Subject, 0, len(in.AppliesTo))
	for _, name := range in.AppliesTo {
		s, err := checkdomain.ParseSubject(name)
		if err != nil {
			return checkdomain.Draft{}, err
		}
		applies = append(applies, s)
	}
	return checkdomain.Draft{
		Name: in.Name, Question: in.Question, AppliesTo: applies,
		Interval: time.Duration(in.IntervalSeconds) * time.Second,
		Enabled:  in.Enabled, Human: in.Human,
	}, nil
}

// toolbox satisfies checkcmd.Tools, and it is the only place `check` and `tool`
// meet. They are peers, so neither may import the other — the composition root
// is where two domains are allowed to know about each other, and the adapter is
// four lines because the port asks the narrowest question there is.
type toolbox struct{ tools *toolquery.Tools }

func (t toolbox) Exists(ctx context.Context, org, tool id.ID) (bool, error) {
	found, err := t.tools.ByID(ctx, org, tool)
	if err != nil {
		if errors.IsKind(err, errors.NotFound) {
			// Not found is an ANSWER here, not a failure. Returning the error
			// would make a step naming a deleted tool a 404 on the whole save
			// rather than a message about that step.
			return false, nil
		}
		return false, err
	}
	// An ARCHIVED tool is not available to a new chain. It stays in the chains
	// that already name it — every run that happened names it too — but a check
	// edited today may not reach for one.
	return !found.Archived(), nil
}

func (m *me) listChecks(w http.ResponseWriter, r *http.Request) {
	_, org, _, ok := m.inFirm(w, r, false)
	if !ok {
		return
	}
	found, err := m.checks.ForOrg(r.Context(), org, r.URL.Query().Get("archived") != "")
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := make([]checkResponse, 0, len(found))
	for _, c := range found {
		out = append(out, asCheck(c))
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

func (m *me) addCheck(w http.ResponseWriter, r *http.Request) {
	caller, org, _, ok := m.inFirm(w, r, true)
	if !ok {
		return
	}
	var in checkRequest
	if !decodeBody(w, r, &in) {
		return
	}
	draft, err := checkDraft(in)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	added, err := m.checksCmd.Add(r.Context(), org, caller, draft)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, asCheck(added))
}

func (m *me) updateCheck(w http.ResponseWriter, r *http.Request) {
	_, org, want, ok := m.onCheck(w, r, true)
	if !ok {
		return
	}
	var in checkRequest
	if !decodeBody(w, r, &in) {
		return
	}
	draft, err := checkDraft(in)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	updated, err := m.checksCmd.Update(r.Context(), org, want, draft)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asCheck(updated))
}

func (m *me) archiveCheck(w http.ResponseWriter, r *http.Request) {
	_, org, want, ok := m.onCheck(w, r, true)
	if !ok {
		return
	}
	if _, err := m.checksCmd.Archive(r.Context(), org, want); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *me) readChain(w http.ResponseWriter, r *http.Request) {
	_, org, want, ok := m.onCheck(w, r, false)
	if !ok {
		return
	}
	chain, err := m.checks.Chain(r.Context(), org, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asChain(chain))
}

// saveChain is a PUT because it replaces the graph. There is no PATCH on a
// chain: an editor holds the whole thing and sends the whole thing, and a
// partial update would need an operation vocabulary nobody has asked for.
func (m *me) saveChain(w http.ResponseWriter, r *http.Request) {
	_, org, want, ok := m.onCheck(w, r, true)
	if !ok {
		return
	}
	var in chainRequest
	if !decodeBody(w, r, &in) {
		return
	}

	steps := make([]checkcmd.StepDraft, 0, len(in.Steps))
	for _, s := range in.Steps {
		draft := checkcmd.StepDraft{X: s.X, Y: s.Y, Pinned: s.Pinned}
		if s.StepID != "" {
			parsed, err := id.Parse(s.StepID)
			if err != nil {
				httpx.Fail(m.log, w, r, checkdomain.ErrStepUnknown)
				return
			}
			draft.ID = parsed
		}
		tool, err := id.Parse(s.ToolID)
		if err != nil {
			httpx.Fail(m.log, w, r, checkdomain.ErrToolRequired)
			return
		}
		draft.ToolID = tool
		steps = append(steps, draft)
	}

	flows := make([]checkcmd.FlowDraft, 0, len(in.Flows))
	for _, f := range in.Flows {
		draft := checkcmd.FlowDraft{FromNew: -1, ToNew: -1}
		if f.FromNew != nil {
			draft.FromNew = *f.FromNew
		} else if parsed, err := id.Parse(f.From); err == nil {
			draft.From = parsed
		} else {
			httpx.Fail(m.log, w, r, checkdomain.ErrStepUnknown)
			return
		}
		if f.ToNew != nil {
			draft.ToNew = *f.ToNew
		} else if parsed, err := id.Parse(f.To); err == nil {
			draft.To = parsed
		} else {
			httpx.Fail(m.log, w, r, checkdomain.ErrStepUnknown)
			return
		}
		flows = append(flows, draft)
	}

	saved, err := m.checksCmd.SaveChain(r.Context(), org, want, steps, flows)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asChain(saved))
}

func (m *me) onCheck(w http.ResponseWriter, r *http.Request, writing bool) (caller, org, check id.ID, ok bool) {
	caller, org, _, ok = m.inFirm(w, r, writing)
	if !ok {
		return id.ID{}, id.ID{}, id.ID{}, false
	}
	check, err := id.Parse(r.PathValue("check"))
	if err != nil {
		httpx.Fail(m.log, w, r, checkdomain.ErrNotFound)
		return id.ID{}, id.ID{}, id.ID{}, false
	}
	return caller, org, check, true
}
