package root

import (
	"log/slog"
	"net/http"
	"time"

	auditquery "github.com/0xsj/overwatch-backend/internal/audit/app/query"
	identitycmd "github.com/0xsj/overwatch-backend/internal/identity/app/command"
	identityquery "github.com/0xsj/overwatch-backend/internal/identity/app/query"
	identityhttp "github.com/0xsj/overwatch-backend/internal/identity/transport/http"
	journalquery "github.com/0xsj/overwatch-backend/internal/journal/app/query"
	orgcmd "github.com/0xsj/overwatch-backend/internal/org/app/command"
	orgquery "github.com/0xsj/overwatch-backend/internal/org/app/query"
	workspacecmd "github.com/0xsj/overwatch-backend/internal/workspace/app/command"
	workspacequery "github.com/0xsj/overwatch-backend/internal/workspace/app/query"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// me holds the three reads it composes, and is a type rather than a method on
// app so a test can build it without booting a process. The three are held
// separately rather than behind one façade: a façade over three domains is the
// coupling decisions/0017 exists to prevent, wearing a struct.
type me struct {
	sessions   *identityquery.Sessions
	settings   *identitycmd.Settings
	people     *identityquery.Directory
	orgs       *orgquery.Orgs
	access     *orgquery.Access
	workspaces *workspacequery.Workspaces
	opener     *workspacecmd.Service
	grants     *orgcmd.Grants
	invites    *orgcmd.Invites
	membership *orgcmd.Members
	ledger     *auditquery.Ledger
	trail      *journalquery.Trail
	log        *slog.Logger
}

func newMe(sessions *identityquery.Sessions, settings *identitycmd.Settings,
	people *identityquery.Directory,
	orgs *orgquery.Orgs, access *orgquery.Access,
	workspaces *workspacequery.Workspaces, opener *workspacecmd.Service,
	grants *orgcmd.Grants, invites *orgcmd.Invites, membership *orgcmd.Members,
	ledger *auditquery.Ledger, trail *journalquery.Trail,
	log *slog.Logger) *me {
	if sessions == nil || settings == nil || people == nil || orgs == nil || access == nil ||
		workspaces == nil || opener == nil || grants == nil || invites == nil ||
		membership == nil || ledger == nil || trail == nil {
		panic("root: newMe with a nil dependency")
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &me{sessions: sessions, settings: settings, people: people, orgs: orgs,
		access: access, workspaces: workspaces, opener: opener,
		grants: grants, invites: invites, membership: membership,
		ledger: ledger, trail: trail, log: log}
}

// meResponse is what the client needs to decide which screen to draw, in one
// round trip. It answers two questions a boot sequence otherwise asks three
// endpoints for: who am I, and where may I go.
type meResponse struct {
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
	Status    string `json:"status"`

	// Verified is Status == active restated as the thing the client acts on.
	// The client must not compute a capability from a status string: the set of
	// statuses will grow, and every place that spelt out the comparison becomes
	// wrong at once.
	Verified bool `json:"verified"`

	Orgs []meOrg `json:"orgs"`
}

type meOrg struct {
	OrgID      string        `json:"org_id"`
	Name       string        `json:"name"`
	Role       string        `json:"role"`
	Workspaces []meWorkspace `json:"workspaces"`
}

type meWorkspace struct {
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`

	// Access is what this caller may do here — decisions/0019. It is on the
	// workspace and not on the org, because the org role is a ceiling and the
	// grant is what actually applies.
	Access string `json:"access"`
}

// me is composed HERE and in no domain, because it is the one place allowed to
// know two vocabularies. `identity` must not import `org`, and `org` must not
// import `workspace` — the import checks refuse it and decisions/0017 is why:
// each of the three has to be able to leave as its own service. The composition
// root is where a join across them is legal, and it is a join of three ANSWERS
// rather than of three tables.
//
// It is deliberately tolerant of an empty result. Between registering and the
// chain finishing, a signed-in account has no org at all (decisions/0018), and
// this endpoint's job in that window is to say so rather than to fail.
// register puts the route beside the handler, so a test mounts the real path
// rather than a string it chose itself — a test asserting /v1/me while the
// server serves /v1/whoami passes and proves nothing.
func (m *me) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me", m.handle)
	mux.HandleFunc("GET /v1/orgs/{org}/members", m.members)
	mux.HandleFunc("POST /v1/orgs/{org}/workspaces", m.openWorkspace)

	// The ledgers — audit answers "who did what", the journal answers "what
	// caused this". Both are composed here because both cross domains.
	mux.HandleFunc("GET /v1/me/activity", m.myActivity)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/audit", m.workspaceAudit)
	mux.HandleFunc("GET /v1/chains/{correlation}", m.chain)

	// Who is on an engagement — decisions/0023. PUT sets a level and DELETE
	// removes access; there is deliberately no way to write `none`, because a
	// revocation deletes the row.
	mux.HandleFunc("GET /v1/workspaces/{workspace}/members", m.workspaceMembers)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/members/{account}", m.setGrant)
	mux.HandleFunc("DELETE /v1/workspaces/{workspace}/members/{account}", m.revokeGrant)

	// Invitations — decisions/0025. Inviting needs an ORG role, which is the
	// mirror of granting needing a WORKSPACE level.
	mux.HandleFunc("POST /v1/orgs/{org}/invites", m.invite)
	mux.HandleFunc("DELETE /v1/orgs/{org}/invites/{invite}", m.revokeInvite)
	mux.HandleFunc("POST /v1/invites/accept", m.acceptInvite)

	// The firm's own log — decisions/0024. It can never name an engagement.
	mux.HandleFunc("GET /v1/orgs/{org}/audit", m.orgAudit)

	// Membership — decisions/0026. `me` in the account position is leaving,
	// which is the same row change with a different actor and one extra refusal.
	mux.HandleFunc("PATCH /v1/orgs/{org}", m.renameOrg)
	mux.HandleFunc("PATCH /v1/orgs/{org}/members/{account}", m.changeRole)
	mux.HandleFunc("DELETE /v1/orgs/{org}/members/{account}", m.removeMember)

	// Engagements — decisions/0027. Closing is reversible, so the list that
	// includes closed ones is what makes reopening reachable at all.
	mux.HandleFunc("GET /v1/orgs/{org}/workspaces", m.listEngagements)
	mux.HandleFunc("PATCH /v1/workspaces/{workspace}", m.renameEngagement)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/close", m.closeEngagement)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/reopen", m.reopenEngagement)

	// Closing an account — decisions/0028. The one endpoint that consults both
	// identity and org in a single request, because neither can answer alone.
	mux.HandleFunc("POST /v1/me/close", m.closeAccount)
}

func (m *me) handle(w http.ResponseWriter, r *http.Request) {
	caller, err := m.sessions.Authenticate(r.Context(), identityhttp.Presented(r))
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}

	memberships, err := m.orgs.For(r.Context(), caller.AccountID)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}

	out := meResponse{
		AccountID: caller.AccountID.String(),
		Email:     caller.Email.String(),
		Status:    caller.Status.String(),
		Verified:  caller.Verified(),
		Orgs:      make([]meOrg, 0, len(memberships)),
	}
	for _, membership := range memberships {
		// The caller's standing in this org, resolved ONCE — role plus every
		// grant they hold in it. Asking per workspace would be a query per row.
		reach, err := m.access.In(r.Context(), caller.AccountID, membership.OrgID)
		if err != nil {
			httpx.Fail(m.log, w, r, err)
			return
		}

		// The membership is what authorises asking. workspace.Workspaces.InOrg
		// answers for any org id it is handed and says so in its doc; asking it
		// only for orgs that org itself just confirmed is what keeps that safe.
		found, err := m.workspaces.InOrg(r.Context(), membership.OrgID)
		if err != nil {
			httpx.Fail(m.log, w, r, err)
			return
		}
		org := meOrg{
			OrgID:      membership.OrgID.String(),
			Name:       membership.Name,
			Role:       membership.Role.String(),
			Workspaces: make([]meWorkspace, 0, len(found)),
		}
		for _, ws := range found {
			// A workspace the caller has no grant on is ABSENT — decisions/0005
			// and 0019. Not a disabled row, not a 403: rendering it at all tells
			// an analyst that a client they cannot see exists, which for the
			// client behind that wall is the leak itself.
			level := reach.On(ws.ID)
			if !level.Visible() {
				continue
			}
			org.Workspaces = append(org.Workspaces, meWorkspace{
				WorkspaceID: ws.ID.String(),
				Name:        ws.Name,
				Access:      level.String(),
			})
		}
		out.Orgs = append(out.Orgs, org)
	}

	httpx.WriteJSON(w, r, http.StatusOK, out)
}

type memberResponse struct {
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Status    string `json:"status"`
	JoinedAt  string `json:"joined_at"`
}

// members is the second composed read, and it joins the same two vocabularies:
// org owns who is a member and what their role is, identity owns what they are
// called. Neither can answer alone and neither may import the other.
//
// **The gate runs first and answers NotFound.** A caller who is not a live
// member of the named org gets the same answer as one naming an org that does
// not exist — decisions/0019. Anything else lets somebody enumerate the orgs
// this server hosts one id at a time.
func (m *me) members(w http.ResponseWriter, r *http.Request) {
	caller, err := m.sessions.Authenticate(r.Context(), identityhttp.Presented(r))
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	org, err := id.Parse(r.PathValue("org"))
	if err != nil {
		// A malformed id is answered as an absent one, for the same reason.
		httpx.Fail(m.log, w, r, orgquery.ErrNoAccess)
		return
	}
	if _, err := m.access.In(r.Context(), caller.AccountID, org); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}

	seats, err := m.orgs.Members(r.Context(), org)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	accounts := make([]id.ID, 0, len(seats))
	for _, seat := range seats {
		accounts = append(accounts, seat.AccountID)
	}
	people, err := m.people.Named(r.Context(), accounts)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}

	out := make([]memberResponse, 0, len(seats))
	for _, seat := range seats {
		person := people[seat.AccountID]
		out = append(out, memberResponse{
			AccountID: seat.AccountID.String(),
			Email:     person.Email.String(),
			Name:      person.Name,
			Role:      seat.Role.String(),
			Status:    seat.Status.String(),
			JoinedAt:  seat.JoinedAt.UTC().Format(time.RFC3339),
		})
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}
