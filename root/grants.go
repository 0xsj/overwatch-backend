package root

import (
	"encoding/json"
	"net/http"
	"time"

	identityhttp "github.com/0xsj/overwatch-backend/internal/identity/transport/http"
	orgquery "github.com/0xsj/overwatch-backend/internal/org/app/query"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type setGrantRequest struct {
	Level string `json:"level"`
}

type seatResponse struct {
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Access    string `json:"access"`
}

// workspaceMembers is one row of the members × workspaces grid: who is on this
// engagement, and at what level.
//
// **`read` is enough to see it.** Colleagues on the same engagement can see each
// other; somebody who cannot see the engagement cannot see its people, which
// follows from the workspace being absent rather than refused — decisions/0023.
func (m *me) workspaceMembers(w http.ResponseWriter, r *http.Request) {
	// A RECORD read: who was on a closed engagement is part of what it kept.
	caller, workspace, org, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	_ = caller

	seats, err := m.access.OnWorkspace(r.Context(), org, workspace)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	accounts := make([]id.ID, 0, len(seats))
	for _, s := range seats {
		accounts = append(accounts, s.AccountID)
	}
	people, err := m.people.Named(r.Context(), accounts)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}

	out := make([]seatResponse, 0, len(seats))
	for _, s := range seats {
		person := people[s.AccountID]
		out = append(out, seatResponse{
			AccountID: s.AccountID.String(),
			Email:     person.Email.String(),
			Name:      person.Name,
			Role:      s.Role.String(),
			Access:    s.Level.String(),
		})
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

// setGrant is PUT, because the screen is a dropdown and the honest verb for
// "set this person to write" is the idempotent one — decisions/0023.
func (m *me) setGrant(w http.ResponseWriter, r *http.Request) {
	caller, workspace, org, ok := m.onWorkspace(w, r, orgdomain.LevelRead)
	if !ok {
		return
	}
	account, err := id.Parse(r.PathValue("account"))
	if err != nil {
		httpx.WriteError(w, r, errors.New(errors.Invalid, "that is not an account id"))
		return
	}
	var in setGrantRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		httpx.WriteError(w, r, errors.Wrap(err, errors.Invalid, "the request body could not be read"))
		return
	}
	level, err := orgdomain.ParseLevel(in.Level)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}

	// The command runs its OWN admin check. This handler resolved read only, to
	// turn the workspace into an org; re-deciding the write rule here would be a
	// second copy of an authorisation rule, which is how the two drift.
	granted, err := m.grants.Set(r.Context(), caller, org, workspace, account, level)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]string{
		"workspace_id": granted.WorkspaceID.String(),
		"account_id":   granted.AccountID.String(),
		"access":       granted.Level.String(),
		"updated_at":   granted.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

func (m *me) revokeGrant(w http.ResponseWriter, r *http.Request) {
	caller, workspace, org, ok := m.onWorkspace(w, r, orgdomain.LevelRead)
	if !ok {
		return
	}
	account, err := id.Parse(r.PathValue("account"))
	if err != nil {
		httpx.WriteError(w, r, errors.New(errors.Invalid, "that is not an account id"))
		return
	}
	if err := m.grants.Revoke(r.Context(), caller, org, workspace, account); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// onWorkspace is for ACTING in an engagement, and refuses a closed one —
// decisions/0027. Everything that writes goes through it.
//
// **It also refuses a CLIENT** — decisions/0042. `0019` said the oddity of that
// role lives in the role rather than on the ladder, and this is what that means
// concretely: a client's ceiling stays `read` and the intersection stays a
// `min()`, while the ROLE removes routes.
//
// **Resolving the org from the workspace is the whole point** — orgquery.Reach's
// precondition. Walking the caller's orgs and asking each one instead is what
// made every engagement in the system readable to anybody who owned an org.
func (m *me) onWorkspace(w http.ResponseWriter, r *http.Request, least orgdomain.Level) (
	caller, workspace, org id.ID, ok bool) {
	caller, workspace, org, closed, client, ok := m.reachWorkspace(w, r, least)
	if !ok {
		return id.ID{}, id.ID{}, id.ID{}, false
	}
	if client {
		// 404, NEVER 403 — CLAUDE.md's rule about `none` applies here with more
		// force, not less: a client learning that an invocation log exists is a
		// client learning what was run against them.
		//
		// **UNREACHABLE TODAY, and kept deliberately.** A mutation round could
		// not kill this line, and it is an equivalent mutant rather than a gap:
		// every acting route either asks for a level a client's ceiling forbids
		// (`write`, `admin`) or asks for `read` and then requires admin inside
		// the command. So the ladder or the command gets there first, always.
		//
		// It stays because "unreachable because of another rule" is exactly how
		// a gate stops holding when that other rule moves — the same reasoning
		// `reopenEngagement` carries. The day a route asks `onWorkspace(read)`
		// and does its own thing, this is what stops it.
		httpx.Fail(m.log, w, r, orgquery.ErrNoAccess)
		return id.ID{}, id.ID{}, id.ID{}, false
	}
	if closed {
		// Conflict and not NotFound: the caller can see this engagement and
		// already knows it exists, so there is nothing left to disclose and what
		// they need is the reason.
		httpx.WriteError(w, r, errors.New(errors.Conflict,
			"that engagement is closed — reopen it first"))
		return id.ID{}, id.ID{}, id.ID{}, false
	}
	return caller, workspace, org, true
}

// onWorkspaceRecord is for READING an engagement's record, and permits a closed
// one — decisions/0027. **It refuses a client**, like its sibling. The record is kept precisely to be read afterwards:
// "who widened the scope" is asked after an engagement, not during it.
//
// It is a second helper rather than a boolean on the first, because a boolean at
// a call site is the thing that gets passed wrong.
func (m *me) onWorkspaceRecord(w http.ResponseWriter, r *http.Request) (
	caller, workspace, org id.ID, ok bool) {
	caller, workspace, org, _, client, ok := m.reachWorkspace(w, r, orgdomain.LevelRead)
	if !ok {
		return id.ID{}, id.ID{}, id.ID{}, false
	}
	if client {
		httpx.Fail(m.log, w, r, orgquery.ErrNoAccess)
		return id.ID{}, id.ID{}, id.ID{}, false
	}
	return caller, workspace, org, true
}

// onDeliverable is the ONE gate a client passes — decisions/0042 §5. Only the
// report routes use it.
//
// **The default is closed and the opt-in is explicit**, which is the only safe
// direction: a route added tomorrow excludes clients without anybody remembering
// to think about it, and the day one should be shared, saying so is one word.
//
// It permits a CLOSED engagement, like [me.onWorkspaceRecord]: a report about
// last quarter's work is exactly the thing somebody asks for after it ends.
func (m *me) onDeliverable(w http.ResponseWriter, r *http.Request, least orgdomain.Level) (
	caller, workspace, org id.ID, ok bool) {
	caller, workspace, org, _, _, ok = m.reachWorkspace(w, r, least)
	return caller, workspace, org, ok
}

func (m *me) reachWorkspace(w http.ResponseWriter, r *http.Request, least orgdomain.Level) (
	caller, workspace, org id.ID, closed, client, ok bool) {
	found, err := m.sessions.Authenticate(r.Context(), identityhttp.Presented(r))
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return id.ID{}, id.ID{}, id.ID{}, false, false, false
	}
	workspace, err = id.Parse(r.PathValue("workspace"))
	if err != nil {
		httpx.Fail(m.log, w, r, orgquery.ErrNoAccess)
		return id.ID{}, id.ID{}, id.ID{}, false, false, false
	}
	org, closed, err = m.workspaces.OrgOf(r.Context(), workspace)
	if err != nil {
		httpx.Fail(m.log, w, r, orgquery.ErrNoAccess)
		return id.ID{}, id.ID{}, id.ID{}, false, false, false
	}
	reach, err := m.access.In(r.Context(), found.AccountID, org)
	if err != nil {
		httpx.Fail(m.log, w, r, orgquery.ErrNoAccess)
		return id.ID{}, id.ID{}, id.ID{}, false, false, false
	}
	if !reach.Allows(workspace, least) {
		httpx.Fail(m.log, w, r, orgquery.ErrNoAccess)
		return id.ID{}, id.ID{}, id.ID{}, false, false, false
	}
	return found.AccountID, workspace, org, closed, reach.Role == orgdomain.RoleClient, true
}
