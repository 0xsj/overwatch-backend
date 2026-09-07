package root

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	auditquery "github.com/0xsj/overwatch-backend/internal/audit/app/query"
	identityhttp "github.com/0xsj/overwatch-backend/internal/identity/transport/http"
	journalquery "github.com/0xsj/overwatch-backend/internal/journal/app/query"
	orgquery "github.com/0xsj/overwatch-backend/internal/org/app/query"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type entryResponse struct {
	ID          string          `json:"id"`
	Scope       string          `json:"scope"`
	Action      string          `json:"action"`
	Subject     string          `json:"subject"`
	Actor       string          `json:"actor"`
	OnBehalfOf  string          `json:"on_behalf_of,omitempty"`
	WorkspaceID string          `json:"workspace_id,omitempty"`
	Correlation string          `json:"correlation_id,omitempty"`
	Detail      json.RawMessage `json:"detail"`
	OccurredAt  string          `json:"occurred_at"`
}

// pageResponse carries the cursor and NEVER a total. Counting an append-only
// ledger is a full scan whose answer is stale before it renders, and CLAUDE.md
// says an unmeasured total renders as `–` and never as a number nothing
// computed. `next` is absent when there is no more.
type facetResponse struct {
	Facet string `json:"facet"`
	Total int    `json:"total"`
}

type pageResponse struct {
	Entries []entryResponse `json:"entries"`
	Next    string          `json:"next,omitempty"`

	// Facets accompany the FIRST page only — that is when a screen opens and
	// needs them, and recomputing per page would charge a scan for a row nobody
	// looks at twice. Absent on later pages, and the client keeps the ones it
	// already has.
	//
	// The counts ignore any `?facet=` in force, deliberately: counting the
	// filtered set makes every other facet read zero, which is a state a reader
	// cannot navigate out of.
	Facets []facetResponse `json:"facets,omitempty"`
}

// myActivity is the caller's own history: the entries whose SUBJECT is their
// account. Registration, sign-in, verification, a password change.
//
// **Subject and not actor**, and the difference matters the day an admin acts on
// somebody else's account: those entries name the admin as actor and the other
// person as subject, and they belong on the subject's activity page — it is
// their account that was changed.
func (m *me) myActivity(w http.ResponseWriter, r *http.Request) {
	caller, err := m.sessions.Authenticate(r.Context(), identityhttp.Presented(r))
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	after, size, facet := pageParams(r)
	page, err := m.ledger.ForSubject(r.Context(),
		"account:"+caller.AccountID.String(), facet, after, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, renderPage(page))
}

// workspaceAudit is one engagement's history, and the gate runs FIRST. A caller
// with no grant gets the same NotFound as a workspace that does not exist —
// decisions/0019.
func (m *me) workspaceAudit(w http.ResponseWriter, r *http.Request) {
	caller, err := m.sessions.Authenticate(r.Context(), identityhttp.Presented(r))
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	workspace, err := id.Parse(r.PathValue("workspace"))
	if err != nil {
		httpx.Fail(m.log, w, r, orgquery.ErrNoAccess)
		return
	}
	if !m.canSeeWorkspace(r, caller.AccountID, workspace) {
		httpx.Fail(m.log, w, r, orgquery.ErrNoAccess)
		return
	}

	after, size, facet := pageParams(r)
	page, err := m.ledger.ForWorkspace(r.Context(), workspace, facet, after, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, renderPage(page))
}

type stepResponse struct {
	Action      string          `json:"action"`
	Subject     string          `json:"subject"`
	Origin      string          `json:"origin"`
	Actor       string          `json:"actor"`
	WorkspaceID string          `json:"workspace_id,omitempty"`
	Depth       int             `json:"depth"`
	Attempt     int             `json:"attempt"`
	Decision    bool            `json:"decision"`
	Detail      json.RawMessage `json:"detail"`
	OccurredAt  string          `json:"occurred_at"`
}

// chain is the "what else was part of this" link, and it is the reason the
// journal has a read side at all.
//
// **The visibility rule lives here because a chain crosses domains.** One
// registration produces a line subjected to an account, one to an org and one to
// a workspace, and no single domain can rule on all three. This is the licence
// U2 grants the root, spent on a read.
//
// A caller may see PART of a chain and is never told how much is missing.
func (m *me) chain(w http.ResponseWriter, r *http.Request) {
	caller, err := m.sessions.Authenticate(r.Context(), identityhttp.Presented(r))
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	correlation, err := id.Parse(r.PathValue("correlation"))
	if err != nil {
		httpx.Fail(m.log, w, r, journalquery.ErrNoChain)
		return
	}

	mine := "account:" + caller.AccountID.String()
	steps, err := m.trail.ForCorrelation(r.Context(), correlation, func(s journalquery.Step) bool {
		// A tenanted line is decided by the grant, whatever its subject says.
		if s.WorkspaceID != "" {
			ws, err := id.Parse(s.WorkspaceID)
			return err == nil && m.canSeeWorkspace(r, caller.AccountID, ws)
		}
		switch kind, value, _ := strings.Cut(s.Subject, ":"); kind {
		case "account":
			// Only your own. An account line names a person, and one person's
			// history is not another's to read.
			return s.Subject == mine
		case "org":
			// Membership is the rule here, not a grant: an org line is about
			// the firm rather than about any one engagement.
			org, err := id.Parse(value)
			if err != nil {
				return false
			}
			_, err = m.access.In(r.Context(), caller.AccountID, org)
			return err == nil
		default:
			// A subject kind nothing here understands is hidden. Failing closed
			// is the only safe default for a rule that must be extended every
			// time a domain is added — and the day it is forgotten, the new
			// domain's lines are invisible rather than public.
			return false
		}
	})
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}

	out := make([]stepResponse, 0, len(steps))
	for _, s := range steps {
		out = append(out, stepResponse{
			Action: s.Action, Subject: s.Subject, Origin: s.Origin,
			Actor: s.Actor, WorkspaceID: s.WorkspaceID,
			Depth: s.Depth, Attempt: s.Attempt, Decision: s.Decision,
			Detail: detail(s.Detail), OccurredAt: s.OccurredAt.UTC().Format(time.RFC3339Nano),
		})
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

// canSeeWorkspace asks the OWNING org, and asking the owning org is the whole
// point — see [orgquery.Reach.On]'s precondition.
//
// The first version of this walked every org the caller belonged to and asked
// each one's Reach about the workspace. That is wrong and it is a disclosure:
// the owner exemption returns `admin` for any id, so owning any org made every
// engagement in the system readable. Resolving the org from the workspace first
// is what makes the exemption mean what decisions/0019 says it means.
func (m *me) canSeeWorkspace(r *http.Request, account, workspace id.ID) bool {
	// The closed flag is deliberately IGNORED here — decisions/0027. This
	// answers "may they read this engagement's record", and a closed one still
	// has a record. Acting is gated by onWorkspace instead.
	org, _, err := m.workspaces.OrgOf(r.Context(), workspace)
	if err != nil {
		return false
	}
	reach, err := m.access.In(r.Context(), account, org)
	if err != nil {
		return false
	}
	return reach.Allows(workspace, orgdomain.LevelRead)
}

// pageParams reads the cursor a previous page handed back. A malformed cursor is
// treated as ABSENT rather than refused: the worst it can do is start the reader
// at the head, and a 400 on a value the client did not compose itself is a dead
// end nobody can act on.
func pageParams(r *http.Request) (auditquery.Cursor, int, string) {
	q := r.URL.Query()
	size, _ := strconv.Atoi(q.Get("limit"))

	// An unknown facet is passed through rather than validated. The facet set is
	// whatever actions exist, which this handler cannot enumerate without the
	// query it is about to run — and the answer for an unknown one is an empty
	// page, which is correct and needs no special case.
	facet := q.Get("facet")

	stamp, rest, ok := strings.Cut(q.Get("after"), ",")
	if !ok {
		return auditquery.Cursor{}, size, facet
	}
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return auditquery.Cursor{}, size, facet
	}
	last, err := id.Parse(rest)
	if err != nil {
		return auditquery.Cursor{}, size, facet
	}
	return auditquery.Cursor{OccurredAt: at, ID: last}, size, facet
}

func renderPage(p auditquery.Page) pageResponse {
	out := pageResponse{Entries: make([]entryResponse, 0, len(p.Records))}
	for _, e := range p.Records {
		row := entryResponse{
			ID:          e.ID.String(),
			Scope:       e.Scope.String(),
			Action:      e.Action,
			Subject:     e.Subject,
			Actor:       e.Actor,
			OnBehalfOf:  e.OnBehalfOf,
			WorkspaceID: e.WorkspaceID,
			Detail:      detail(e.Detail),
			OccurredAt:  e.OccurredAt.UTC().Format(time.RFC3339Nano),
		}
		if !e.Correlation.IsZero() {
			row.Correlation = e.Correlation.String()
		}
		out.Entries = append(out.Entries, row)
	}
	// The cursor is only meaningful when there is another page behind it.
	// Returning one unconditionally invites a client to fetch an empty page and
	// conclude something went wrong.
	if p.More {
		out.Next = p.Next.OccurredAt.UTC().Format(time.RFC3339Nano) + "," + p.Next.ID.String()
	}
	for _, f := range p.Facets {
		out.Facets = append(out.Facets, facetResponse{Facet: f.Name, Total: f.Total})
	}
	return out
}

// detail keeps a null detail out of the wire as `{}` rather than `null`, so a
// client can index it without a guard on every row.
func detail(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(raw)
}
