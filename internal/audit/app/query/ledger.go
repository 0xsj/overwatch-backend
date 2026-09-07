package query

import (
	"context"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/audit/domain"
	auditpg "github.com/0xsj/overwatch-backend/internal/audit/infra/postgres"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// DefaultPage is what a caller gets for asking for nothing, and MaxPage is the
// most it can ask for. The cap is not politeness: a page size read from a query
// string is a caller choosing how much work this server does.
const (
	DefaultPage = 50
	MaxPage     = 200
)

// Reader is the read side and shares nothing with the subscriber's write port.
type Reader interface {
	PageForSubject(ctx context.Context, subject string, after auditpg.Cursor, limit int32) ([]domain.Entry, error)
	PageForWorkspace(ctx context.Context, workspace string, after auditpg.Cursor, limit int32) ([]domain.Entry, error)

	PageForSubjectFacet(ctx context.Context, subject, facet string, after auditpg.Cursor, limit int32) ([]domain.Entry, error)
	PageForWorkspaceFacet(ctx context.Context, workspace, facet string, after auditpg.Cursor, limit int32) ([]domain.Entry, error)

	PageForOrg(ctx context.Context, org string, after auditpg.Cursor, limit int32) ([]domain.Entry, error)
	FacetsForOrg(ctx context.Context, org string) ([]auditpg.Facet, error)

	FacetsForSubject(ctx context.Context, subject string) ([]auditpg.Facet, error)
	FacetsForWorkspace(ctx context.Context, workspace string) ([]auditpg.Facet, error)
}

// Facet is one bucket of the action's first segment. It is COMPUTED from the
// action and never stored: the action is the fact and the facet is a reading of
// it, so a column would be a second spelling that drifts the first time an
// action is renamed.
type Facet = auditpg.Facet

// Cursor names the last entry a reader saw. Zero means "from the head".
type Cursor = auditpg.Cursor

// Record is one row as a screen needs it. It is a view and not the aggregate:
// EventID is dropped because it is the ledger's idempotency key and means
// nothing to a reader, and Correlation is kept because it is the link to "what
// else was part of this".
type Record struct {
	ID          id.ID
	Scope       domain.Scope
	Action      string
	Subject     string
	Actor       string
	OnBehalfOf  string
	WorkspaceID string
	OrgID       string
	Correlation id.ID
	Detail      []byte
	OccurredAt  time.Time
}

// Page is a slice of the ledger and the cursor that continues it.
//
// **More is a fact, not a count.** It is true when the read came back full,
// which is one row of over-fetch rather than a scan — and it is what a client
// needs to decide whether to draw a "load more". A total would be a full scan
// whose answer is stale before it renders.
type Page struct {
	Records []Record
	Next    Cursor
	More    bool

	// Facets are the counts for the WHOLE subject, deliberately ignoring any
	// facet filter in force. Counting only the filtered set makes every other
	// facet read zero, which is the state a reader cannot navigate out of —
	// they can enter a facet and never see that another exists.
	//
	// They are computed for the FIRST page only, because that is when a screen
	// opens and needs them, and recomputing on every page would charge a scan
	// for a row nobody looks at twice.
	Facets []Facet
}

type Ledger struct{ reader Reader }

func NewLedger(reader Reader) *Ledger {
	if reader == nil {
		panic("audit: NewLedger with a nil reader")
	}
	return &Ledger{reader: reader}
}

// ForSubject is one subject's history — "what happened to this account". The
// subject is the `kind:id` string decisions/0013 puts in the envelope.
func (l *Ledger) ForSubject(ctx context.Context, subject, facet string, after Cursor, size int) (Page, error) {
	if subject == "" {
		return Page{}, domain.ErrSubjectRequired
	}
	limit := clamp(size)

	var found []domain.Entry
	var err error
	if facet == "" {
		found, err = l.reader.PageForSubject(ctx, subject, after, limit+1)
	} else {
		found, err = l.reader.PageForSubjectFacet(ctx, subject, facet, after, limit+1)
	}
	if err != nil {
		return Page{}, fmt.Errorf("audit: for subject: %w", err)
	}
	out := page(found, int(limit))

	if after.IsZero() {
		if out.Facets, err = l.reader.FacetsForSubject(ctx, subject); err != nil {
			return Page{}, fmt.Errorf("audit: facets: %w", err)
		}
	}
	return out, nil
}

// ForWorkspace is one engagement's history. **It does not check that the caller
// may see that workspace** — this package cannot see a grant. The caller
// resolves access first; see root.
func (l *Ledger) ForWorkspace(ctx context.Context, workspace id.ID, facet string, after Cursor, size int) (Page, error) {
	if workspace.IsZero() {
		return Page{}, domain.ErrSubjectRequired
	}
	limit := clamp(size)
	key := workspace.String()

	var found []domain.Entry
	var err error
	if facet == "" {
		found, err = l.reader.PageForWorkspace(ctx, key, after, limit+1)
	} else {
		found, err = l.reader.PageForWorkspaceFacet(ctx, key, facet, after, limit+1)
	}
	if err != nil {
		return Page{}, fmt.Errorf("audit: for workspace: %w", err)
	}
	out := page(found, int(limit))

	if after.IsZero() {
		if out.Facets, err = l.reader.FacetsForWorkspace(ctx, key); err != nil {
			return Page{}, fmt.Errorf("audit: facets: %w", err)
		}
	}
	return out, nil
}

// page turns an over-fetched slice into a page. The extra row is read and
// DISCARDED: it is how "there is more" is answered without counting, and
// dropping it is what keeps the page size the caller asked for.
func page(found []domain.Entry, limit int) Page {
	more := len(found) > limit
	if more {
		found = found[:limit]
	}
	out := Page{Records: make([]Record, 0, len(found)), More: more}
	for _, e := range found {
		out.Records = append(out.Records, Record{
			ID:          e.ID,
			Scope:       e.Scope,
			Action:      e.Action,
			Subject:     e.Subject,
			Actor:       e.Actor,
			OnBehalfOf:  e.OnBehalfOf,
			WorkspaceID: e.WorkspaceID,
			OrgID:       e.OrgID,
			Correlation: e.Correlation,
			Detail:      e.Detail,
			OccurredAt:  e.OccurredAt,
		})
	}
	if n := len(out.Records); n > 0 {
		last := out.Records[n-1]
		out.Next = Cursor{OccurredAt: last.OccurredAt, ID: last.ID}
	}
	return out
}

func clamp(size int) int32 {
	switch {
	case size <= 0:
		return DefaultPage
	case size > MaxPage:
		return MaxPage
	default:
		return int32(size)
	}
}

// ForOrg is the firm's own history: membership, roles, invitations —
// decisions/0024. An org-scope entry can never carry a workspace id, which is
// what makes this safe to serve to every member rather than only to those on a
// particular engagement.
//
// There is deliberately NO facet filter here yet; the actions are few and the
// screen has no facet row. It costs one query to add when it does.
func (l *Ledger) ForOrg(ctx context.Context, org id.ID, after Cursor, size int) (Page, error) {
	if org.IsZero() {
		return Page{}, domain.ErrSubjectRequired
	}
	limit := clamp(size)
	key := org.String()
	found, err := l.reader.PageForOrg(ctx, key, after, limit+1)
	if err != nil {
		return Page{}, fmt.Errorf("audit: for org: %w", err)
	}
	out := page(found, int(limit))
	if after.IsZero() {
		if out.Facets, err = l.reader.FacetsForOrg(ctx, key); err != nil {
			return Page{}, fmt.Errorf("audit: facets: %w", err)
		}
	}
	return out, nil
}
