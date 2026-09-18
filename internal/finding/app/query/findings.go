package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/finding/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	ByID(ctx context.Context, workspace, want id.ID) (domain.Finding, error)
	Page(ctx context.Context, workspace id.ID, state string, fragment id.ID, limit int) ([]domain.Finding, error)
	ForFragments(ctx context.Context, workspace id.ID, fragments []id.ID) ([]domain.Finding, error)
	Details(ctx context.Context, finding id.ID) ([]domain.Detail, error)
	Live(ctx context.Context, workspace id.ID) (map[id.ID]domain.Badge, error)
}

const (
	DefaultPage = 100
	MaxPage     = 500
)

type Findings struct{ reader Reader }

func NewFindings(reader Reader) *Findings {
	if reader == nil {
		panic("finding: NewFindings with a nil reader")
	}
	return &Findings{reader: reader}
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

func (f *Findings) ByID(ctx context.Context, workspace, want id.ID) (domain.Finding, error) {
	if workspace.IsZero() || want.IsZero() {
		return domain.Finding{}, domain.ErrIDRequired
	}
	return f.reader.ByID(ctx, workspace, want)
}

// Board is worst-first, then newest. `state` empty means every state — including
// `resolved` and `dismissed`, because a report cites what was closed as well as
// what is open.
func (f *Findings) Board(ctx context.Context, workspace id.ID, state string,
	fragment id.ID, limit int) ([]domain.Finding, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	if state != "" {
		if _, err := domain.ParseState(state); err != nil {
			return nil, err
		}
	}
	return f.reader.Page(ctx, workspace, state, fragment, page(limit))
}

// ForFragments is a complete read for a report's target-attributed subjects.
// The workspace remains mandatory even though the fragment IDs are known.
func (f *Findings) ForFragments(ctx context.Context, workspace id.ID, fragments []id.ID) ([]domain.Finding, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	if len(fragments) == 0 {
		return []domain.Finding{}, nil
	}
	return f.reader.ForFragments(ctx, workspace, fragments)
}

// Detail is one finding and everything its tool read beside the identity.
type Detail struct {
	Finding domain.Finding
	Details []domain.Detail
}

func (f *Findings) Detail(ctx context.Context, workspace, want id.ID) (Detail, error) {
	found, err := f.ByID(ctx, workspace, want)
	if err != nil {
		return Detail{}, err
	}
	details, err := f.reader.Details(ctx, found.ID)
	if err != nil {
		return Detail{}, err
	}
	return Detail{Finding: found, Details: details}, nil
}

// Badges is 0009's "this fragment has an open finding", per fragment. It is
// COMPUTED on every read and never materialised on the fragment row: it changes
// on every scan and on every triage, which is 0037's argument against a
// materialised coverage grid one table over.
func (f *Findings) Badges(ctx context.Context, workspace id.ID) (map[id.ID]domain.Badge, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	return f.reader.Live(ctx, workspace)
}
