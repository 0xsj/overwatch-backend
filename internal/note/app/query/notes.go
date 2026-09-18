package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/note/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	AllSummary(ctx context.Context, workspace id.ID) ([]domain.Note, error)
	ByID(ctx context.Context, workspace, want id.ID) (domain.Note, error)
	Page(ctx context.Context, workspace id.ID, kind, value string, summaryOnly bool, limit int) ([]domain.Note, error)
}

const (
	DefaultPage = 100
	MaxPage     = 500
)

type Notes struct{ reader Reader }

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
func (n *Notes) List(ctx context.Context, workspace id.ID, limit int) ([]domain.Note, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	return n.reader.Page(ctx, workspace, "", "", false, page(limit))
}

// About answers the notes attached to one thing. The value is FOLDED here as
// well as at write time, so a caller passing what a person typed finds what a
// person saved.
func (n *Notes) About(ctx context.Context, workspace id.ID, kind, value string, limit int) ([]domain.Note, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	if kind == "" || value == "" {
		return nil, domain.ErrSubjectHalfSet
	}
	return n.reader.Page(ctx, workspace, kind, domain.Fold(value), false, page(limit))
}

// Summary is the SUBJECTLESS notes — the engagement summary, and what `0042`'s
// eighth report section carries.
func (n *Notes) Summary(ctx context.Context, workspace id.ID, limit int) ([]domain.Note, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	return n.reader.Page(ctx, workspace, "", "", true, page(limit))
}

// AllSummary reads the entire engagement summary for a deliverable.
func (n *Notes) AllSummary(ctx context.Context, workspace id.ID) ([]domain.Note, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	return n.reader.AllSummary(ctx, workspace)
}
