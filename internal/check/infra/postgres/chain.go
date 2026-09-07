package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/check/domain"
	"github.com/0xsj/overwatch-backend/internal/check/infra/postgres/checkdb"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

func (s *Store) Chain(ctx context.Context, want id.ID) (domain.Chain, error) {
	steps, err := s.q(ctx).StepsForCheck(ctx, uuid(want))
	if err != nil {
		return domain.Chain{}, postgres.Translate(ctx, err, "check: steps")
	}
	flows, err := s.q(ctx).FlowsForCheck(ctx, uuid(want))
	if err != nil {
		return domain.Chain{}, postgres.Translate(ctx, err, "check: flows")
	}
	out := domain.Chain{
		Steps: make([]domain.Step, 0, len(steps)),
		Flows: make([]domain.Flow, 0, len(flows)),
	}
	for _, row := range steps {
		out.Steps = append(out.Steps, domain.Step{
			ID: ident(row.ID), CheckID: ident(row.CheckID), ToolID: ident(row.ToolID),
			X: int(row.X), Y: int(row.Y), Pinned: row.Pinned,
		})
	}
	for _, row := range flows {
		out.Flows = append(out.Flows, domain.Flow{
			CheckID: ident(row.CheckID), From: ident(row.FromStep), To: ident(row.ToStep),
		})
	}
	return out, nil
}

// SaveChain is a DIFF for steps and a REPLACE for flows — decisions/0032, and
// the asymmetry is the whole point:
//
//	a step   an invocation will name it, so its id must survive an edit
//	a flow   nothing cites an edge, so there is no identity to preserve
//
// The order matters. Deleting removed steps FIRST, before the flows are
// rewritten, means an edge pointing at a deleted step never exists — not even
// inside the transaction, where a later statement could otherwise read it.
//
// The caller wraps this in a transaction. Without one a crash between the delete
// and the inserts leaves a check with no chain, which is silence rather than a
// wrong chain, but it is still not what anybody asked for.
func (s *Store) SaveChain(ctx context.Context, check id.ID, chain domain.Chain) error {
	q := s.q(ctx)

	keep := make([]pgtype.UUID, 0, len(chain.Steps))
	for _, step := range chain.Steps {
		keep = append(keep, uuid(step.ID))
	}
	if err := q.DeleteStepsExcept(ctx, checkdb.DeleteStepsExceptParams{
		CheckID: uuid(check), Keep: keep,
	}); err != nil {
		return postgres.Translate(ctx, err, "check: prune steps")
	}
	if err := q.DeleteFlowsForCheck(ctx, uuid(check)); err != nil {
		return postgres.Translate(ctx, err, "check: clear flows")
	}

	for _, step := range chain.Steps {
		// Update first, insert when nothing moved. The alternative — read the
		// existing ids and branch — is one more round trip to learn what the
		// affected-row count already says.
		n, err := q.UpdateStep(ctx, checkdb.UpdateStepParams{
			ID: uuid(step.ID), CheckID: uuid(check), ToolID: uuid(step.ToolID),
			X: int32(step.X), Y: int32(step.Y), Pinned: step.Pinned,
		})
		if err != nil {
			return postgres.Translate(ctx, err, "check: update step")
		}
		if n > 0 {
			continue
		}
		if err := q.InsertStep(ctx, checkdb.InsertStepParams{
			ID: uuid(step.ID), CheckID: uuid(check), ToolID: uuid(step.ToolID),
			X: int32(step.X), Y: int32(step.Y), Pinned: step.Pinned,
		}); err != nil {
			return postgres.Translate(ctx, err, "check: insert step")
		}
	}

	for _, flow := range chain.Flows {
		if err := q.InsertFlow(ctx, checkdb.InsertFlowParams{
			CheckID: uuid(check), FromStep: uuid(flow.From), ToStep: uuid(flow.To),
		}); err != nil {
			return postgres.Translate(ctx, err, "check: insert flow")
		}
	}
	return nil
}
