package query

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/scope/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	Live(ctx context.Context, workspace, target id.ID) ([]domain.Rule, error)
	All(ctx context.Context, workspace, target id.ID) ([]domain.Rule, error)
	ByID(ctx context.Context, workspace, want id.ID) (domain.Rule, error)
}

type Rules struct{ reader Reader }

func NewRules(reader Reader) *Rules {
	if reader == nil {
		panic("scope: NewRules query with a nil reader")
	}
	return &Rules{reader: reader}
}

func (r *Rules) Live(ctx context.Context, workspace, target id.ID) ([]domain.Rule, error) {
	return r.list(ctx, workspace, target, false)
}

// All includes superseded rules, newest first. They are the history the
// citations point at — an invocation refusal and a finding's scope proof both
// name a rule id, and both captured it at the time.
func (r *Rules) All(ctx context.Context, workspace, target id.ID) ([]domain.Rule, error) {
	return r.list(ctx, workspace, target, true)
}

// Decide answers the question rather than returning rows, so no caller
// reimplements `exclude beats include` — and so the answer carries every rule
// that matched, which decisions/0010 requires and a caller filtering rows itself
// would have to reconstruct.
func (r *Rules) Decide(ctx context.Context, workspace, target id.ID,
	gate domain.Gate, c domain.Candidate) (domain.Decision, error) {
	live, err := r.Live(ctx, workspace, target)
	if err != nil {
		return domain.Decision{}, err
	}
	return domain.Decide(live, gate, c), nil
}

func (r *Rules) list(ctx context.Context, workspace, target id.ID, all bool) ([]domain.Rule, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	if target.IsZero() {
		return nil, domain.ErrTargetRequired
	}
	read := r.reader.Live
	if all {
		read = r.reader.All
	}
	found, err := read(ctx, workspace, target)
	if err != nil {
		return nil, fmt.Errorf("scope: rules: %w", err)
	}
	return found, nil
}

// ByID reads one rule, SUPERSEDED ONES INCLUDED — which is precisely why 0030
// keeps them. Three surfaces cite a rule id and each captured it at the time;
// this is the read that resolves the citation, and refusing a superseded row
// would make every historical citation dangle.
func (r *Rules) ByID(ctx context.Context, workspace, want id.ID) (domain.Rule, error) {
	if workspace.IsZero() || want.IsZero() {
		return domain.Rule{}, domain.ErrIDRequired
	}
	return r.reader.ByID(ctx, workspace, want)
}
