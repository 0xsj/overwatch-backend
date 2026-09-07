package postgres

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/scope/domain"
	"github.com/0xsj/overwatch-backend/internal/scope/infra/postgres/scopedb"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

func (s *Store) Create(ctx context.Context, r domain.Rule) error {
	err := s.q(ctx).InsertRule(ctx, scopedb.InsertRuleParams{
		ID:           uuid(r.ID),
		WorkspaceID:  uuid(r.WorkspaceID),
		TargetID:     uuid(r.TargetID),
		Pattern:      r.Pattern,
		Polarity:     r.Polarity.String(),
		Gate:         r.Gate.String(),
		Kinds:        kindNames(r.Kinds),
		Tools:        intensityNames(r.Tools),
		CreatedBy:    uuid(r.CreatedBy),
		CreatedAt:    stamp(r.CreatedAt),
		SupersededAt: stamp(r.SupersededAt),
		SupersededBy: maybe(r.SupersededBy),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "scope: insert rule")
	}
	return nil
}

func (s *Store) ByID(ctx context.Context, workspace, want id.ID) (domain.Rule, error) {
	row, err := s.q(ctx).RuleByID(ctx, scopedb.RuleByIDParams{
		ID: uuid(want), WorkspaceID: uuid(workspace),
	})
	if err != nil {
		translated := postgres.Translate(ctx, err, "scope: read rule")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Rule{}, fmt.Errorf("scope: read rule: %w", domain.ErrNotFound)
		}
		return domain.Rule{}, translated
	}
	return rule(row)
}

// Live is the evaluator's read — one target's rules that still apply.
func (s *Store) Live(ctx context.Context, workspace, target id.ID) ([]domain.Rule, error) {
	rows, err := s.q(ctx).LiveRulesForTarget(ctx, scopedb.LiveRulesForTargetParams{
		TargetID: uuid(target), WorkspaceID: uuid(workspace),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "scope: live rules")
	}
	return rules(rows)
}

// All is the editor's read — superseded rules included, newest first, because
// they are the history the citations point at.
func (s *Store) All(ctx context.Context, workspace, target id.ID) ([]domain.Rule, error) {
	rows, err := s.q(ctx).AllRulesForTarget(ctx, scopedb.AllRulesForTargetParams{
		TargetID: uuid(target), WorkspaceID: uuid(workspace),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "scope: all rules")
	}
	return rules(rows)
}

// Supersede is the only update in this schema — decisions/0030. It touches two
// lifecycle columns and nothing else, and its `superseded_at is null` predicate
// is the concurrency control: two callers retiring one rule produce one winner.
func (s *Store) Supersede(ctx context.Context, r domain.Rule) error {
	n, err := s.q(ctx).SupersedeRule(ctx, scopedb.SupersedeRuleParams{
		ID:           uuid(r.ID),
		WorkspaceID:  uuid(r.WorkspaceID),
		SupersededAt: stamp(r.SupersededAt),
		SupersededBy: maybe(r.SupersededBy),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "scope: supersede rule")
	}
	if n == 0 {
		return fmt.Errorf("scope: supersede rule: %w", domain.ErrSuperseded)
	}
	return nil
}
