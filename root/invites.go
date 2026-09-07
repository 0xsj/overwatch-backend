package root

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	identityquery "github.com/0xsj/overwatch-backend/internal/identity/app/query"
	identityhttp "github.com/0xsj/overwatch-backend/internal/identity/transport/http"
	orgcmd "github.com/0xsj/overwatch-backend/internal/org/app/command"
	orgquery "github.com/0xsj/overwatch-backend/internal/org/app/query"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// directory satisfies org's [orgcmd.Directory] port — the one thing org needs
// from identity, and it crosses here rather than through an import because org
// and identity are peers.
//
// It answers ADDRESSES and nothing else. A wider adapter would let org drift
// into asking identity questions it has no business asking.
type directory struct{ people *identityquery.Directory }

func (d directory) AddressesOf(ctx context.Context, accounts []id.ID) (map[id.ID]string, error) {
	found, err := d.people.Named(ctx, accounts)
	if err != nil {
		return nil, err
	}
	out := make(map[id.ID]string, len(found))
	for k, person := range found {
		out[k] = person.Email.String()
	}
	return out, nil
}

type inviteRequest struct {
	Email       string `json:"email"`
	Role        string `json:"role"`
	WorkspaceID string `json:"workspace_id"`
	Level       string `json:"level"`
}

type inviteResponse struct {
	InviteID    string `json:"invite_id"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Level       string `json:"level,omitempty"`
	ExpiresAt   string `json:"expires_at"`
}

// invite asks somebody to join. Inviting needs an ORG role — decisions/0025 —
// which is the mirror of granting needing a WORKSPACE level.
func (m *me) invite(w http.ResponseWriter, r *http.Request) {
	caller, err := m.sessions.Authenticate(r.Context(), identityhttp.Presented(r))
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	if !caller.Verified() {
		httpx.WriteError(w, r, errors.New(errors.Forbidden,
			"confirm your email address before inviting anybody"))
		return
	}
	org, err := id.Parse(r.PathValue("org"))
	if err != nil {
		httpx.Fail(m.log, w, r, orgquery.ErrNoAccess)
		return
	}
	var in inviteRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		httpx.WriteError(w, r, errors.Wrap(err, errors.Invalid, "the request body could not be read"))
		return
	}
	role, err := orgdomain.ParseRole(in.Role)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}

	invitation := orgcmd.Invitation{Email: in.Email, Role: role}
	// A workspace and a level arrive together or not at all — an invitation
	// with one and not the other cannot say what access it confers.
	if (in.WorkspaceID == "") != (in.Level == "") {
		httpx.WriteError(w, r, errors.New(errors.Invalid,
			"a first engagement needs both a workspace and a level"))
		return
	}
	if in.WorkspaceID != "" {
		if invitation.WorkspaceID, err = id.Parse(in.WorkspaceID); err != nil {
			httpx.Fail(m.log, w, r, orgquery.ErrNoAccess)
			return
		}
		if invitation.Level, err = orgdomain.ParseLevel(in.Level); err != nil {
			httpx.Fail(m.log, w, r, err)
			return
		}
	}

	sent, err := m.invites.Send(r.Context(), caller.AccountID, org, invitation)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := inviteResponse{
		InviteID:  sent.ID.String(),
		Email:     sent.Email,
		Role:      sent.Role.String(),
		ExpiresAt: sent.ExpiresAt.UTC().Format(time.RFC3339),
	}
	if sent.HasGrant() {
		out.WorkspaceID = sent.WorkspaceID.String()
		out.Level = sent.Level.String()
	}
	httpx.WriteJSON(w, r, http.StatusCreated, out)
}

// acceptInvite is authenticated and address-bound — decisions/0025. The caller's
// own email is read from the session rather than from the request, so there is
// nothing here to claim to be somebody else.
func (m *me) acceptInvite(w http.ResponseWriter, r *http.Request) {
	caller, err := m.sessions.Authenticate(r.Context(), identityhttp.Presented(r))
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		httpx.WriteError(w, r, errors.Wrap(err, errors.Invalid, "the request body could not be read"))
		return
	}
	taken, err := m.invites.Accept(r.Context(), caller.AccountID, caller.Email.String(), in.Token)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]string{
		"org_id": taken.OrgID.String(),
		"role":   taken.Role.String(),
	})
}

func (m *me) revokeInvite(w http.ResponseWriter, r *http.Request) {
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
	invite, err := id.Parse(r.PathValue("invite"))
	if err != nil {
		httpx.Fail(m.log, w, r, orgdomain.ErrInviteGone)
		return
	}
	if err := m.invites.Revoke(r.Context(), caller.AccountID, org, invite); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// orgAudit is the firm's own history — membership, roles, invitations.
//
// **Every member may read it, and it can never name an engagement**, because an
// org-scope entry carries no workspace id by constraint (decisions/0024). That
// constraint is the only reason this is safe to serve org-wide, having been
// refused three days earlier for a feed of workspace entries.
func (m *me) orgAudit(w http.ResponseWriter, r *http.Request) {
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
	if _, err := m.access.In(r.Context(), caller.AccountID, org); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	after, size, _ := pageParams(r)
	page, err := m.ledger.ForOrg(r.Context(), org, after, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, renderPage(page))
}
