package root

import (
	"encoding/json"
	"net/http"
	"strings"

	identityhttp "github.com/0xsj/overwatch-backend/internal/identity/transport/http"
	orgquery "github.com/0xsj/overwatch-backend/internal/org/app/query"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

type changeRoleRequest struct {
	Role string `json:"role"`
}

type renameOrgRequest struct {
	Name string `json:"name"`
}

func (m *me) changeRole(w http.ResponseWriter, r *http.Request) {
	caller, org, account, ok := m.inOrg(w, r)
	if !ok {
		return
	}
	var in changeRoleRequest
	if !decodeBody(w, r, &in) {
		return
	}
	role, err := orgdomain.ParseRole(in.Role)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	if err := m.membership.ChangeRole(r.Context(), caller, org, account, role); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// removeMember is both removing and leaving — decisions/0026. They are the same
// row change with a different actor, so they are one route and two events; the
// command tells them apart by whether the target is the caller.
func (m *me) removeMember(w http.ResponseWriter, r *http.Request) {
	caller, org, account, ok := m.inOrg(w, r)
	if !ok {
		return
	}
	if err := m.membership.Remove(r.Context(), caller, org, account); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *me) renameOrg(w http.ResponseWriter, r *http.Request) {
	caller, err := m.sessions.Authenticate(r.Context(), identityhttp.Presented(r))
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	org, err := id.Parse(r.PathValue("org"))
	if err != nil {
		httpx.Fail(m.log, w, r, orgquery.ErrNoAccess)
		return
	}
	var in renameOrgRequest
	if !decodeBody(w, r, &in) {
		return
	}
	renamed, err := m.membership.Rename(r.Context(), caller.AccountID, org, in.Name)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]string{
		"org_id": renamed.ID.String(),
		"name":   renamed.Name,
	})
}

// inOrg authenticates and reads the two path ids. It deliberately does NOT
// authorise: the command owns that rule, and a second copy here is how the two
// drift apart.
func (m *me) inOrg(w http.ResponseWriter, r *http.Request) (caller, org, account id.ID, ok bool) {
	found, err := m.sessions.Authenticate(r.Context(), identityhttp.Presented(r))
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return id.ID{}, id.ID{}, id.ID{}, false
	}
	org, err = id.Parse(r.PathValue("org"))
	if err != nil {
		httpx.Fail(m.log, w, r, orgquery.ErrNoAccess)
		return id.ID{}, id.ID{}, id.ID{}, false
	}
	// `me` names the caller, so leaving needs no id the client has to look up.
	if raw := r.PathValue("account"); raw == "me" {
		account = found.AccountID
	} else if account, err = id.Parse(raw); err != nil {
		httpx.Fail(m.log, w, r, orgdomain.ErrMemberNotFound)
		return id.ID{}, id.ID{}, id.ID{}, false
	}
	return found.AccountID, org, account, true
}

type closeAccountRequest struct {
	CurrentPassword string `json:"current_password"`
}

// closeAccount is the one endpoint that spans identity and org, and the ONLY
// place the two are consulted in one request — decisions/0028.
//
// **The check is here because neither domain can do it.** identity may not ask
// whether this account is somebody's last owner; org may not archive an account.
// So the root asks org first, refuses if anybody would be stranded, and only
// then tells identity — after which org's subscriber ends every membership.
func (m *me) closeAccount(w http.ResponseWriter, r *http.Request) {
	caller, err := m.sessions.Authenticate(r.Context(), identityhttp.Presented(r))
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	var in closeAccountRequest
	if !decodeBody(w, r, &in) {
		return
	}

	blocked, err := m.access.Stranding(r.Context(), caller.AccountID)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	if len(blocked) > 0 {
		// NAME them. "You cannot close your account" with no reason is a dead
		// end; the fix exists and is theirs — transfer ownership, or remove the
		// other members.
		names := make([]string, 0, len(blocked))
		for _, b := range blocked {
			names = append(names, b.Name)
		}
		httpx.WriteError(w, r, errors.Newf(errors.Conflict,
			"you are the last owner of %s — transfer ownership or remove the other members first",
			strings.Join(names, ", ")))
		return
	}

	if err := m.settings.Close(r.Context(), caller.AccountID,
		secret.New(in.CurrentPassword)); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	// The caller's own session went with the rest. They are signed out by this
	// response.
	w.WriteHeader(http.StatusNoContent)
}

func decodeBody(w http.ResponseWriter, r *http.Request, into any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		httpx.WriteError(w, r, errors.Wrap(err, errors.Invalid, "the request body could not be read"))
		return false
	}
	return true
}
