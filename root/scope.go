package root

import (
	"net/http"
	"time"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	scopecmd "github.com/0xsj/overwatch-backend/internal/scope/app/command"
	scopedomain "github.com/0xsj/overwatch-backend/internal/scope/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type addRuleRequest struct {
	Pattern  string   `json:"pattern"`
	Polarity string   `json:"polarity"`
	Gate     string   `json:"gate"`
	Kinds    []string `json:"kinds"`
	Tools    []string `json:"tools"`
}

type ruleResponse struct {
	RuleID       string   `json:"rule_id"`
	Pattern      string   `json:"pattern"`
	Polarity     string   `json:"polarity"`
	Gate         string   `json:"gate"`
	Kinds        []string `json:"kinds"`
	Tools        []string `json:"tools,omitempty"`
	CreatedAt    string   `json:"created_at"`
	SupersededAt string   `json:"superseded_at,omitempty"`
}

// listRules is a RECORD read — `read` is enough, and superseded rules come back
// under ?all=1 because they are the history an invocation refusal cites.
func (m *me) listRules(w http.ResponseWriter, r *http.Request) {
	_, workspace, target, ok := m.onTargetRecord(w, r)
	if !ok {
		return
	}
	read := m.rules.Live
	if r.URL.Query().Get("all") != "" {
		read = m.rules.All
	}
	found, err := read(r.Context(), workspace, target)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := make([]ruleResponse, 0, len(found))
	for _, rule := range found {
		out = append(out, renderRule(rule))
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

// addRule needs ADMIN — 0019 puts "edit scope" at that rung in as many words,
// and this does not reinterpret it.
func (m *me) addRule(w http.ResponseWriter, r *http.Request) {
	caller, workspace, target, ok := m.onTarget(w, r, orgdomain.LevelAdmin)
	if !ok {
		return
	}
	var in addRuleRequest
	if !decodeBody(w, r, &in) {
		return
	}
	draft, err := draftFrom(in)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	added, err := m.rulesCmd.Add(r.Context(), workspace, target, caller, draft)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, renderRule(added))
}

// supersedeRule is DELETE at the wire and a supersede underneath — decisions/0030.
// There is no edit, and the row keeps its id forever, because an invocation
// refusal and a finding's scope proof both cite it.
func (m *me) supersedeRule(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onTarget(w, r, orgdomain.LevelAdmin)
	if !ok {
		return
	}
	rule, err := id.Parse(r.PathValue("rule"))
	if err != nil {
		httpx.WriteError(w, r, errors.New(errors.NotFound, "scope rule"))
		return
	}
	if _, err := m.rulesCmd.Supersede(r.Context(), workspace, rule, caller); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// onTargetRecord is the record helper plus a target id — reading a target's
// scope is a read, not an act, so a CLOSED engagement still answers.
func (m *me) onTargetRecord(w http.ResponseWriter, r *http.Request) (
	caller, workspace, target id.ID, ok bool) {
	caller, workspace, _, ok = m.onWorkspaceRecord(w, r)
	if !ok {
		return id.ID{}, id.ID{}, id.ID{}, false
	}
	target, err := id.Parse(r.PathValue("target"))
	if err != nil {
		httpx.WriteError(w, r, errors.New(errors.NotFound, "target"))
		return id.ID{}, id.ID{}, id.ID{}, false
	}
	return caller, workspace, target, true
}

func draftFrom(in addRuleRequest) (scopecmd.Draft, error) {
	polarity, err := scopedomain.ParsePolarity(in.Polarity)
	if err != nil {
		return scopecmd.Draft{}, err
	}
	gate, err := scopedomain.ParseGate(in.Gate)
	if err != nil {
		return scopecmd.Draft{}, err
	}
	kinds := make([]scopedomain.Kind, 0, len(in.Kinds))
	for _, name := range in.Kinds {
		k, err := scopedomain.ParseKind(name)
		if err != nil {
			return scopecmd.Draft{}, err
		}
		kinds = append(kinds, k)
	}
	var tools []scopedomain.Intensity
	for _, name := range in.Tools {
		t, err := scopedomain.ParseIntensity(name)
		if err != nil {
			return scopecmd.Draft{}, err
		}
		tools = append(tools, t)
	}
	return scopecmd.Draft{
		Pattern: in.Pattern, Polarity: polarity, Gate: gate,
		Kinds: kinds, Tools: tools,
	}, nil
}

func renderRule(r scopedomain.Rule) ruleResponse {
	kinds := make([]string, 0, len(r.Kinds))
	for _, k := range r.Kinds {
		kinds = append(kinds, k.String())
	}
	var tools []string
	for _, t := range r.Tools {
		tools = append(tools, t.String())
	}
	out := ruleResponse{
		RuleID: r.ID.String(), Pattern: r.Pattern,
		Polarity: r.Polarity.String(), Gate: r.Gate.String(),
		Kinds: kinds, Tools: tools,
		CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
	}
	if r.Superseded() {
		out.SupersededAt = r.SupersededAt.UTC().Format(time.RFC3339)
	}
	return out
}
