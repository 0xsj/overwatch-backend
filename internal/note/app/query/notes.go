package query

import (
	"context"
	"strings"

	"github.com/0xsj/overwatch-backend/internal/note/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	AllSummary(ctx context.Context, workspace id.ID) ([]domain.Note, error)
	ByID(ctx context.Context, workspace, want id.ID) (domain.Note, error)
	Page(ctx context.Context, workspace id.ID, kind, value, contextKind string, summaryOnly bool, limit int) ([]domain.Note, error)
	Window(ctx context.Context, workspace, before id.ID, kind, value, contextKind, search string, summaryOnly bool, limit int) ([]domain.Note, error)
}

const (
	DefaultPage = 100
	MaxPage     = 500
)

type Notes struct{ reader Reader }

// Page is the bounded, resumable read used by the investigation notebook.
// The legacy list methods remain array-shaped for older clients; new screens
// use this cursor contract so a large notebook is never silently truncated.
type Page struct {
	Items      []domain.Note `json:"items"`
	NextCursor *id.ID        `json:"next_cursor"`
}

func NewNotes(reader Reader) *Notes {
	if reader == nil {
		panic("note: NewNotes with a nil reader")
	}
	return &Notes{reader: reader}
}

func page(limit int) int {
	if limit <= 0 {
		return DefaultPage
	}
	if limit > MaxPage {
		return MaxPage
	}
	return limit
}

func (n *Notes) ByID(ctx context.Context, workspace, want id.ID) (domain.Note, error) {
	if workspace.IsZero() || want.IsZero() {
		return domain.Note{}, domain.ErrIDRequired
	}
	return n.reader.ByID(ctx, workspace, want)
}

// List answers every note in an engagement, newest first.
//
// **This is NOT search.** `CLAUDE.md` says a note is *"searched beside assets and
// observations"* and there is no search surface in this system for anything —
// `0043` §4 names that as a noun shipped short of its own definition rather than
// pretending a list is the same thing.
func (n *Notes) List(ctx context.Context, workspace id.ID, contextKind string, limit int) ([]domain.Note, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	if contextKind != "" {
		parsed, err := domain.ParseContextKind(contextKind)
		if err != nil {
			return nil, err
		}
		contextKind = string(parsed)
	}
	return n.reader.Page(ctx, workspace, "", "", contextKind, false, page(limit))
}

// About answers the notes attached to one thing. The value is FOLDED here as
// well as at write time, so a caller passing what a person typed finds what a
// person saved.
func (n *Notes) About(ctx context.Context, workspace id.ID, kind, value, contextKind string, limit int) ([]domain.Note, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	if kind == "" || value == "" {
		return nil, domain.ErrSubjectHalfSet
	}
	return n.reader.Page(ctx, workspace, kind, domain.Fold(value), contextKind, false, page(limit))
}

// Summary is the SUBJECTLESS notes — the engagement summary, and what `0042`'s
// eighth report section carries.
func (n *Notes) Summary(ctx context.Context, workspace id.ID, contextKind string, limit int) ([]domain.Note, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	if contextKind != "" {
		parsed, err := domain.ParseContextKind(contextKind)
		if err != nil {
			return nil, err
		}
		contextKind = string(parsed)
	}
	return n.reader.Page(ctx, workspace, "", "", contextKind, true, page(limit))
}

// Window reads a cursor-addressed note window. Search is deliberately over the
// authored note text and subject tuple, not a guessed cross-domain index; a
// caller can act on every returned row without losing which note matched.
func (n *Notes) Window(ctx context.Context, workspace, before id.ID, kind, value, contextKind, search string, summaryOnly bool, limit int) (Page, error) {
	if workspace.IsZero() {
		return Page{}, domain.ErrWorkspaceRequired
	}
	if kind != "" || value != "" {
		if kind == "" || value == "" {
			return Page{}, domain.ErrSubjectHalfSet
		}
		value = domain.Fold(value)
	}
	if contextKind != "" {
		parsed, err := domain.ParseContextKind(contextKind)
		if err != nil {
			return Page{}, err
		}
		contextKind = string(parsed)
	}
	search = strings.TrimSpace(search)
	if len(search) > 200 {
		return Page{}, domain.ErrSearchTooLong
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := n.reader.Window(ctx, workspace, before, kind, value, contextKind, search, summaryOnly, limit+1)
	if err != nil {
		return Page{}, err
	}
	out := Page{Items: rows}
	if out.Items == nil {
		out.Items = []domain.Note{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

// AllSummary reads the entire engagement summary for a deliverable.
func (n *Notes) AllSummary(ctx context.Context, workspace id.ID) ([]domain.Note, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	return n.reader.AllSummary(ctx, workspace)
}
