package query

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/workspace/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Reader is the read side. ForOrg already excludes archived workspaces, which is
// where that rule belongs: a screen that must not show them cannot be trusted to
// remember, and every caller wanting them back can ask a different question.
type Reader interface {
	ForOrg(ctx context.Context, orgID id.ID) ([]domain.Workspace, error)
	AllForOrg(ctx context.Context, orgID id.ID) ([]domain.Workspace, error)
	ByID(ctx context.Context, want id.ID) (domain.Workspace, error)
}

// Summary is a view. It deliberately omits the version and the source event —
// the first is a write-side concern and the second is how the row got here,
// which is provenance's question and answered from the journal.
type Summary struct {
	ID    id.ID
	OrgID id.ID
	Name  string

	// Closed is false for everything InOrg returns, and is the point of
	// AllForOrg — decisions/0027.
	Closed bool
}

type Workspaces struct{ reader Reader }

func NewWorkspaces(reader Reader) *Workspaces {
	if reader == nil {
		panic("workspace: NewWorkspaces with a nil reader")
	}
	return &Workspaces{reader: reader}
}

// InOrg answers with the live workspaces of one org, oldest first. It does NOT
// check that the caller belongs to that org — this package cannot see a
// membership, and a query that silently authorises is the shape of an access
// bug. The caller resolves the org first; see root's /v1/me.
func (w *Workspaces) InOrg(ctx context.Context, org id.ID) ([]Summary, error) {
	if org.IsZero() {
		return nil, domain.ErrIDRequired
	}
	found, err := w.reader.ForOrg(ctx, org)
	if err != nil {
		return nil, fmt.Errorf("workspace: in org: %w", err)
	}
	out := make([]Summary, 0, len(found))
	for _, ws := range found {
		out = append(out, Summary{ID: ws.ID, OrgID: ws.OrgID, Name: ws.Name})
	}
	return out, nil
}

// OrgOf answers which org owns a workspace, and it exists to close an
// authorisation trap rather than to serve a screen.
//
// **org's owner exemption is admin on every workspace IN THAT ORG**, and a
// [org/app/query.Reach] cannot check the second half — it is scoped to an org
// and org may not read this table. So a caller that hands an arbitrary
// workspace id to a Reach gets `admin` from any org they own, which is how a
// stranger read somebody else's engagement audit on 2026-09-07.
//
// The fix is to resolve the org from the WORKSPACE and ask that org's Reach.
// This is the read that makes that possible, and it is why an access check
// costs two queries rather than one.
//
// **It answers for a CLOSED engagement too, and returns whether it is one** —
// decisions/0027. It used to refuse an archived workspace, which made a closed
// engagement's own audit log unreadable: the record is kept precisely to be read
// afterwards. Refusing is now the caller's decision, because acting in a closed
// engagement and reading its record are different questions.
func (w *Workspaces) OrgOf(ctx context.Context, workspace id.ID) (id.ID, bool, error) {
	if workspace.IsZero() {
		return id.ID{}, false, domain.ErrNotFound
	}
	found, err := w.reader.ByID(ctx, workspace)
	if err != nil {
		return id.ID{}, false, err
	}
	return found.OrgID, found.Archived(), nil
}

// AllForOrg is every workspace in an org, CLOSED ONES INCLUDED, oldest first.
//
// It exists because [Workspaces.InOrg] excludes archived rows — correctly, since
// that read feeds the switcher — which leaves a closed engagement unreachable
// and therefore impossible to reopen. decisions/0027.
//
// It does not authorise. The caller filters by what its Reach permits.
func (w *Workspaces) AllForOrg(ctx context.Context, org id.ID) ([]Summary, error) {
	if org.IsZero() {
		return nil, domain.ErrIDRequired
	}
	found, err := w.reader.AllForOrg(ctx, org)
	if err != nil {
		return nil, fmt.Errorf("workspace: all in org: %w", err)
	}
	out := make([]Summary, 0, len(found))
	for _, ws := range found {
		out = append(out, Summary{
			ID: ws.ID, OrgID: ws.OrgID, Name: ws.Name, Closed: ws.Archived(),
		})
	}
	return out, nil
}
