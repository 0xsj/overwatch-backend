package root

import (
	"encoding/json"
	"net/http"

	identityhttp "github.com/0xsj/overwatch-backend/internal/identity/transport/http"
	orgquery "github.com/0xsj/overwatch-backend/internal/org/app/query"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type openWorkspaceRequest struct {
	Name string `json:"name"`
}

type workspaceResponse struct {
	WorkspaceID string `json:"workspace_id"`
	OrgID       string `json:"org_id"`
	Name        string `json:"name"`

	// Access is what the OPENER may do, and it is `admin` from the moment this
	// responds — for an owner by the exemption, for an admin once the grant
	// subscriber runs. See the note on the window in decisions/0020.
	Access string `json:"access"`
}

// openWorkspace is an ORG-level act: starting an engagement, not doing work
// inside one. So it is gated on the org ROLE and not on a workspace grant —
// there is no workspace to hold a grant on yet.
//
// **Owner and admin may open one; member, guest and client may not.** An admin
// who cannot start an engagement is an admin who has to ask an owner for every
// new piece of work, which defeats the reason decisions/0019 gave admin its own
// role.
func (m *me) openWorkspace(w http.ResponseWriter, r *http.Request) {
	caller, err := m.sessions.Authenticate(r.Context(), identityhttp.Presented(r))
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	// decisions/0018: authentication is the door, capability is the gate. An
	// unverified account may sign in and may not start an engagement, because
	// an unproven address must not end up on a client's record.
	if !caller.Verified() {
		httpx.WriteError(w, r, errors.New(errors.Forbidden,
			"confirm your email address before starting an engagement"))
		return
	}
	org, err := id.Parse(r.PathValue("org"))
	if err != nil {
		httpx.Fail(m.log, w, r, orgquery.ErrNoAccess)
		return
	}

	reach, err := m.access.In(r.Context(), caller.AccountID, org)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	switch reach.Role {
	case orgdomain.RoleOwner, orgdomain.RoleAdmin:
	default:
		// Forbidden and not NotFound, and the difference from every other
		// refusal here is deliberate: the caller is a member of this org and
		// already knows it exists, so there is nothing left to disclose. What
		// they need is the reason.
		httpx.WriteError(w, r, errors.New(errors.Forbidden,
			"only an owner or an admin can start an engagement"))
		return
	}

	var in openWorkspaceRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		httpx.WriteError(w, r, errors.Wrap(err, errors.Invalid, "the request body could not be read"))
		return
	}

	space, err := m.opener.Open(r.Context(), org, caller.AccountID, in.Name)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, workspaceResponse{
		WorkspaceID: space.ID.String(),
		OrgID:       space.OrgID.String(),
		Name:        space.Name,
		Access:      orgdomain.LevelAdmin.String(),
	})
}
