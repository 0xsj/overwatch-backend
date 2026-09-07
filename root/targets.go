package root

import (
	"net/http"
	"time"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	targetdomain "github.com/0xsj/overwatch-backend/internal/target/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type addTargetRequest struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type renameTargetRequest struct {
	Name string `json:"name"`
}

type targetResponse struct {
	TargetID  string `json:"target_id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Archived  bool   `json:"archived"`
	CreatedAt string `json:"created_at"`
}

// listTargets is the first product READ, and it is scoped by the engagement in
// the path — never by a target id alone. `?archived=1` includes closed ones,
// which is what makes one reachable and therefore reopenable.
func (m *me) listTargets(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	read := m.targets.Live
	if r.URL.Query().Get("archived") != "" {
		read = m.targets.All
	}
	found, err := read(r.Context(), workspace)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := make([]targetResponse, 0, len(found))
	for _, t := range found {
		out = append(out, targetResponse{
			TargetID: t.ID.String(), Name: t.Name, Kind: t.Kind.String(),
			Archived: t.Archived, CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

// addTarget is the FIRST PRODUCT WRITE THROUGH THE CAPABILITY GATE.
// decisions/0018 said the first workspace operation built owns the check; 0019
// made it specific; every caller until now has been tenancy.
//
// `write`, off 0019's ladder: adding something to look at is the ordinary work
// of an engagement.
func (m *me) addTarget(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in addTargetRequest
	if !decodeBody(w, r, &in) {
		return
	}
	kind, err := targetdomain.ParseKind(in.Kind)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	added, err := m.targetsCmd.Add(r.Context(), workspace, caller, in.Name, kind)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, targetResponse{
		TargetID: added.ID.String(), Name: added.Name, Kind: added.Kind.String(),
		CreatedAt: added.CreatedAt.UTC().Format(time.RFC3339),
	})
}

func (m *me) renameTarget(w http.ResponseWriter, r *http.Request) {
	_, workspace, target, ok := m.onTarget(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in renameTargetRequest
	if !decodeBody(w, r, &in) {
		return
	}
	renamed, err := m.targetsCmd.Rename(r.Context(), workspace, target, in.Name)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, targetResponse{
		TargetID: renamed.ID.String(), Name: renamed.Name, Kind: renamed.Kind.String(),
		Archived: renamed.Archived(), CreatedAt: renamed.CreatedAt.UTC().Format(time.RFC3339),
	})
}

// archiveTarget needs ADMIN, not write — 0029. It hides a record, which is
// nearer "edit scope" than it is to the ordinary work of an engagement.
func (m *me) archiveTarget(w http.ResponseWriter, r *http.Request) {
	_, workspace, target, ok := m.onTarget(w, r, orgdomain.LevelAdmin)
	if !ok {
		return
	}
	if _, err := m.targetsCmd.Archive(r.Context(), workspace, target); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *me) reopenTarget(w http.ResponseWriter, r *http.Request) {
	_, workspace, target, ok := m.onTarget(w, r, orgdomain.LevelAdmin)
	if !ok {
		return
	}
	reopened, err := m.targetsCmd.Reopen(r.Context(), workspace, target)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, targetResponse{
		TargetID: reopened.ID.String(), Name: reopened.Name, Kind: reopened.Kind.String(),
		CreatedAt: reopened.CreatedAt.UTC().Format(time.RFC3339),
	})
}

// onTarget is the workspace gate plus a target id, and the target is NEVER
// resolved without the workspace — the store's signature refuses it, and this is
// what feeds that signature.
//
// A target in another engagement answers the same NotFound as one that does not
// exist, because the caller must not learn which.
func (m *me) onTarget(w http.ResponseWriter, r *http.Request, least orgdomain.Level) (
	caller, workspace, target id.ID, ok bool) {
	caller, workspace, _, ok = m.onWorkspace(w, r, least)
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
