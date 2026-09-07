package root

import (
	"net/http"
	"strings"
	"time"

	identityhttp "github.com/0xsj/overwatch-backend/internal/identity/transport/http"
	orgquery "github.com/0xsj/overwatch-backend/internal/org/app/query"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	toolcmd "github.com/0xsj/overwatch-backend/internal/tool/app/command"
	tooldomain "github.com/0xsj/overwatch-backend/internal/tool/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type addToolRequest struct {
	Name      string `json:"name"`
	Argv      string `json:"argv"`
	Intensity string `json:"intensity"`
	// Empty means "nothing upstream" — a SOURCE tool, seeded from the target's
	// scope. It is not "any kind", and the client must not send "*".
	Consumes string `json:"consumes"`
	Produces string `json:"produces"`
	// Which exit codes mean the tool answered — decisions/0033. Absent means
	// {0}; nuclei wants [0, 1], because it exits 1 when it finds nothing.
	SuccessExitCodes []int `json:"success_exit_codes"`
}

type updateToolRequest struct {
	Argv             string `json:"argv"`
	Intensity        string `json:"intensity"`
	Consumes         string `json:"consumes"`
	Produces         string `json:"produces"`
	SuccessExitCodes []int  `json:"success_exit_codes"`
}

type toolResponse struct {
	ToolID    string `json:"tool_id"`
	OrgID     string `json:"org_id"`
	Name      string `json:"name"`
	Argv      string `json:"argv"`
	Intensity string `json:"intensity"`
	// Omitted when the tool is a source. `omitempty` and not `null`, so the
	// client's optional-field shape matches: absent means nothing upstream.
	Consumes         string `json:"consumes,omitempty"`
	Produces         string `json:"produces,omitempty"`
	SuccessExitCodes []int  `json:"success_exit_codes"`
	Archived         bool   `json:"archived"`
	CreatedAt        string `json:"created_at"`
}

type addMappingRequest struct {
	Field      string `json:"field"`
	Expression string `json:"expression"`
	Promote    bool   `json:"promote"`
}

type mappingResponse struct {
	MappingID  string `json:"mapping_id"`
	ToolID     string `json:"tool_id"`
	Field      string `json:"field"`
	Expression string `json:"expression"`
	Version    int    `json:"version"`
	State      string `json:"state"`
	CreatedAt  string `json:"created_at"`
}

func asTool(t tooldomain.Tool) toolResponse {
	return toolResponse{
		ToolID: t.ID.String(), OrgID: t.OrgID.String(), Name: t.Name,
		Argv: t.Argv, Intensity: t.Intensity.String(),
		Consumes: t.Consumes.String(), Produces: t.Produces.String(),
		SuccessExitCodes: t.SuccessExitCodes,
		Archived:         t.Archived(),
		CreatedAt:        t.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func asMapping(m tooldomain.Mapping) mappingResponse {
	return mappingResponse{
		MappingID: m.ID.String(), ToolID: m.ToolID.String(), Field: m.Field,
		Expression: m.Expression, Version: m.Version, State: m.State.String(),
		CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// toolDefinition parses the three enum-shaped fields in one place, so the two
// write routes cannot disagree about what an empty `consumes` means.
func toolDefinition(name, argv, intensity, consumes, produces string, success []int) (toolcmd.Definition, error) {
	i, err := tooldomain.ParseIntensity(intensity)
	if err != nil {
		return toolcmd.Definition{}, err
	}
	c, err := tooldomain.ParseFeed(consumes)
	if err != nil {
		return toolcmd.Definition{}, err
	}
	p, err := tooldomain.ParseFeed(produces)
	if err != nil {
		return toolcmd.Definition{}, err
	}
	return toolcmd.Definition{
		Name: name, Argv: argv, Intensity: i, Consumes: c, Produces: p, Success: success,
	}, nil
}

// listTools is the first product read gated on an ORG ROLE rather than a grant
// — decisions/0031. There is nothing per-engagement to narrow: a tool is the
// firm's, so any member of the firm sees it.
func (m *me) listTools(w http.ResponseWriter, r *http.Request) {
	_, org, _, ok := m.inFirm(w, r, false)
	if !ok {
		return
	}
	found, err := m.tools.ForOrg(r.Context(), org, r.URL.Query().Get("archived") != "")
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := make([]toolResponse, 0, len(found))
	for _, t := range found {
		out = append(out, asTool(t))
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

func (m *me) addTool(w http.ResponseWriter, r *http.Request) {
	caller, org, _, ok := m.inFirm(w, r, true)
	if !ok {
		return
	}
	var in addToolRequest
	if !decodeBody(w, r, &in) {
		return
	}
	def, err := toolDefinition(in.Name, in.Argv, in.Intensity, in.Consumes, in.Produces, in.SuccessExitCodes)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	added, err := m.toolsCmd.Add(r.Context(), org, caller, def)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, asTool(added))
}

func (m *me) updateTool(w http.ResponseWriter, r *http.Request) {
	_, org, tool, ok := m.onTool(w, r, true)
	if !ok {
		return
	}
	var in updateToolRequest
	if !decodeBody(w, r, &in) {
		return
	}
	def, err := toolDefinition("", in.Argv, in.Intensity, in.Consumes, in.Produces, in.SuccessExitCodes)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	updated, err := m.toolsCmd.Update(r.Context(), org, tool, def)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asTool(updated))
}

// archiveTool takes a tool out of use and keeps the record, because every
// invocation that ever ran names it. There is no reopen route yet — the name is
// released, so one would need the same collision refusal an engagement's does.
//
// **It is REFUSED while a live check still runs it**, and the refusal NAMES the
// checks. This is the one question decisions/0032 built the chain out of rows to
// answer, and it is asked HERE because `tool` and `check` are peers: neither may
// import the other, so the composition root is the only place both are visible.
//
// Archiving anyway would leave a chain pointing at a tool nothing can run, and
// the failure would surface at run time as a missing binary rather than now, as
// a list of things to fix first.
func (m *me) archiveTool(w http.ResponseWriter, r *http.Request) {
	_, org, tool, ok := m.onTool(w, r, true)
	if !ok {
		return
	}
	using, err := m.checks.UsingTool(r.Context(), org, tool)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	if len(using) > 0 {
		// NAME them. "You cannot archive this tool" with no reason is a dead
		// end; the fix exists and is theirs — the same shape closing an account
		// uses when somebody would be stranded.
		names := make([]string, 0, len(using))
		for _, u := range using {
			names = append(names, u.Name)
		}
		httpx.WriteError(w, r, errors.New(errors.Conflict,
			"this tool is still used by "+strings.Join(names, ", ")))
		return
	}
	if _, err := m.toolsCmd.Archive(r.Context(), org, tool); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listMappings answers with EVERY version, retired ones included. They are the
// history the citations point at.
func (m *me) listMappings(w http.ResponseWriter, r *http.Request) {
	_, org, tool, ok := m.onTool(w, r, false)
	if !ok {
		return
	}
	found, err := m.tools.Mappings(r.Context(), org, tool)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := make([]mappingResponse, 0, len(found))
	for _, mapped := range found {
		out = append(out, asMapping(mapped))
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

// addMapping is also what a CORRECTION is — it gets no route of its own,
// because correcting a mapping is adding a version whose author is a person,
// and the author is already the caller.
func (m *me) addMapping(w http.ResponseWriter, r *http.Request) {
	caller, org, tool, ok := m.onTool(w, r, true)
	if !ok {
		return
	}
	var in addMappingRequest
	if !decodeBody(w, r, &in) {
		return
	}
	added, err := m.mappingsCmd.Draft(r.Context(), org, tool, caller,
		in.Field, in.Expression, in.Promote)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, asMapping(added))
}

func (m *me) promoteMapping(w http.ResponseWriter, r *http.Request) {
	_, org, _, ok := m.onTool(w, r, true)
	if !ok {
		return
	}
	mapping, err := id.Parse(r.PathValue("mapping"))
	if err != nil {
		httpx.Fail(m.log, w, r, tooldomain.ErrMappingGone)
		return
	}
	live, err := m.mappingsCmd.Promote(r.Context(), org, mapping)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asMapping(live))
}

// inFirm authenticates and resolves the caller's ORG ROLE — the gate for
// anything the firm owns rather than an engagement records. It is not
// onWorkspace: there is no workspace here, and reusing that helper would have
// forced a tool to belong to one.
//
// Membership reads; `owner` or `admin` writes — decisions/0019 already places
// "tool definitions" on that rung. **An admin with no grant on any engagement
// can edit a parser every engagement uses**, which is the same reach 0019 gives
// them over people.
//
// A non-member gets ErrNoAccess, which is NotFound: naming an org the caller
// cannot see is the disclosure `none` exists to prevent.
func (m *me) inFirm(w http.ResponseWriter, r *http.Request, writing bool) (caller, org id.ID, reach orgquery.Reach, ok bool) {
	found, err := m.sessions.Authenticate(r.Context(), identityhttp.Presented(r))
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return id.ID{}, id.ID{}, orgquery.Reach{}, false
	}
	org, err = id.Parse(r.PathValue("org"))
	if err != nil {
		httpx.Fail(m.log, w, r, orgquery.ErrNoAccess)
		return id.ID{}, id.ID{}, orgquery.Reach{}, false
	}
	reach, err = m.access.In(r.Context(), found.AccountID, org)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return id.ID{}, id.ID{}, orgquery.Reach{}, false
	}
	if writing {
		switch reach.Role {
		case orgdomain.RoleOwner, orgdomain.RoleAdmin:
		default:
			// Forbidden and not NotFound: the caller is a member and already
			// knows this org exists, so there is nothing left to disclose and
			// what they need is the reason.
			httpx.WriteError(w, r, errors.New(errors.Forbidden,
				"only an owner or an admin can change what the firm runs"))
			return id.ID{}, id.ID{}, orgquery.Reach{}, false
		}
	}
	return found.AccountID, org, reach, true
}

func (m *me) onTool(w http.ResponseWriter, r *http.Request, writing bool) (caller, org, tool id.ID, ok bool) {
	caller, org, _, ok = m.inFirm(w, r, writing)
	if !ok {
		return id.ID{}, id.ID{}, id.ID{}, false
	}
	tool, err := id.Parse(r.PathValue("tool"))
	if err != nil {
		httpx.Fail(m.log, w, r, tooldomain.ErrNotFound)
		return id.ID{}, id.ID{}, id.ID{}, false
	}
	return caller, org, tool, true
}
