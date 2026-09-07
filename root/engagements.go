package root

import (
	"net/http"

	identityhttp "github.com/0xsj/overwatch-backend/internal/identity/transport/http"
	orgquery "github.com/0xsj/overwatch-backend/internal/org/app/query"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// workspaceNotClosed keeps reopen idempotent-looking rather than silent: a
// client that reopens twice should be told the second one did nothing.
var workspaceNotClosed = errors.New(errors.Conflict, "that engagement is not closed")

type engagementResponse struct {
	WorkspaceID string `json:"workspace_id"`
	OrgID       string `json:"org_id"`
	Name        string `json:"name"`
	Access      string `json:"access"`
	Closed      bool   `json:"closed"`
}

// listEngagements is every engagement in an org the caller can reach, CLOSED
// ONES INCLUDED — decisions/0027.
//
// `/v1/me` deliberately excludes closed engagements: a boot call should not
// carry a firm's history. Without this list a closed engagement is unreachable
// and reopening it is impossible over HTTP.
func (m *me) listEngagements(w http.ResponseWriter, r *http.Request) {
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
	reach, err := m.access.In(r.Context(), caller.AccountID, org)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	found, err := m.workspaces.AllForOrg(r.Context(), org)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}

	out := make([]engagementResponse, 0, len(found))
	for _, ws := range found {
		// Same rule as /v1/me: one the caller has no grant on is ABSENT, not a
		// disabled row. Closing does not change that.
		level := reach.On(ws.ID)
		if !level.Visible() {
			continue
		}
		out = append(out, engagementResponse{
			WorkspaceID: ws.ID.String(),
			OrgID:       ws.OrgID.String(),
			Name:        ws.Name,
			Access:      level.String(),
			Closed:      ws.Closed,
		})
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

func (m *me) renameEngagement(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelAdmin)
	if !ok {
		return
	}
	var in renameOrgRequest
	if !decodeBody(w, r, &in) {
		return
	}
	renamed, err := m.opener.Rename(r.Context(), workspace, in.Name)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, engagementResponse{
		WorkspaceID: renamed.ID.String(),
		OrgID:       renamed.OrgID.String(),
		Name:        renamed.Name,
		Access:      orgdomain.LevelAdmin.String(),
		Closed:      renamed.Archived(),
	})
}

func (m *me) closeEngagement(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelAdmin)
	if !ok {
		return
	}
	if _, err := m.opener.Close(r.Context(), workspace); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// reopenEngagement reaches a CLOSED engagement, so it cannot use onWorkspace —
// that helper refuses one by design. It uses the record helper and then requires
// admin itself.
//
// **It can fail on the name.** Closing released it, so another engagement may
// hold it now; the answer is a conflict and the fix is to rename first.
func (m *me) reopenEngagement(w http.ResponseWriter, r *http.Request) {
	caller, workspace, org, closed, ok := m.reachWorkspace(w, r, orgdomain.LevelAdmin)
	if !ok {
		return
	}
	_, _ = caller, org
	if !closed {
		httpx.Fail(m.log, w, r, workspaceNotClosed)
		return
	}
	reopened, err := m.opener.Reopen(r.Context(), workspace)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, engagementResponse{
		WorkspaceID: reopened.ID.String(),
		OrgID:       reopened.OrgID.String(),
		Name:        reopened.Name,
		Access:      orgdomain.LevelAdmin.String(),
		Closed:      false,
	})
}
