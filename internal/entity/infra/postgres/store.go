package postgres

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/entity/domain"
	"github.com/0xsj/overwatch-backend/internal/entity/infra/postgres/entitydb"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

// CreateEntity is IDEMPOTENT. The outbox delivers at least once, so a
// redelivered `target.added` must be a no-op — decisions/0007.
func (s *Store) CreateEntity(ctx context.Context, e domain.Entity) error {
	if err := e.Judgement.Valid(); err != nil {
		return err
	}
	err := s.q(ctx).InsertEntity(ctx, entitydb.InsertEntityParams{
		ID: uuid(e.ID), WorkspaceID: uuid(e.WorkspaceID), Kind: e.Kind,
		Label: e.Label, TargetID: maybe(e.TargetID),
		JudgementState: e.Judgement.State.String(), JudgementBy: maybe(e.Judgement.By),
		JudgementAt: stamp(e.Judgement.At), JudgementReason: text(e.Judgement.Reason),
		CreatedAt: stamp(e.CreatedAt),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "entity: insert entity")
	}
	return nil
}

func (s *Store) EntityByID(ctx context.Context, workspace, want id.ID) (domain.Entity, error) {
	row, err := s.q(ctx).EntityByID(ctx, entitydb.EntityByIDParams{ID: uuid(want), WorkspaceID: uuid(workspace)})
	if err != nil {
		return domain.Entity{}, notFound(ctx, err, "entity: read entity", domain.ErrNotFound)
	}
	return entityOf(row)
}

// RootFor is looked up by TARGET rather than by id, because that is the only way
// a subscriber knows which entity it made — and `entity_one_root_per_target`
// guarantees at most one.
func (s *Store) RootFor(ctx context.Context, target id.ID) (domain.Entity, error) {
	row, err := s.q(ctx).RootEntityForTarget(ctx, maybe(target))
	if err != nil {
		return domain.Entity{}, notFound(ctx, err, "entity: root for target", domain.ErrNotFound)
	}
	return entityOf(row)
}

func (s *Store) Entities(ctx context.Context, workspace id.ID, limit int) ([]domain.Entity, error) {
	rows, err := s.q(ctx).EntitiesForWorkspace(ctx, entitydb.EntitiesForWorkspaceParams{
		WorkspaceID: uuid(workspace), Page: int32(limit),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "entity: entities")
	}
	out := make([]domain.Entity, 0, len(rows))
	for _, row := range rows {
		e, err := entityOf(row)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func (s *Store) JudgeEntity(ctx context.Context, e domain.Entity) error {
	if err := e.Judgement.Valid(); err != nil {
		return err
	}
	n, err := s.q(ctx).JudgeEntity(ctx, entitydb.JudgeEntityParams{
		ID: uuid(e.ID), WorkspaceID: uuid(e.WorkspaceID),
		JudgementState: e.Judgement.State.String(), JudgementBy: maybe(e.Judgement.By),
		JudgementAt: stamp(e.Judgement.At), JudgementReason: text(e.Judgement.Reason),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "entity: judge entity")
	}
	if n == 0 {
		return fmt.Errorf("entity: judge entity: %w", domain.ErrNotFound)
	}
	return nil
}

// Upsert is THE DEDUP — decisions/0036. It answers the stored fragment and
// whether this call created it, because the subscriber's event carries "how many
// are new" and a second read to find out would be a read per fragment.
func (s *Store) Upsert(ctx context.Context, f domain.Fragment) (domain.Fragment, bool, error) {
	row, err := s.q(ctx).UpsertFragment(ctx, entitydb.UpsertFragmentParams{
		ID: uuid(f.ID), WorkspaceID: uuid(f.WorkspaceID), Kind: f.Kind, Value: f.Value,
		Origin: f.Origin.String(), FirstSeen: stamp(f.FirstSeen), LastSeen: stamp(f.LastSeen),
		Observations: int32(f.Observations), CreatedAt: stamp(f.CreatedAt),
	})
	if err != nil {
		return domain.Fragment{}, false, postgres.Translate(ctx, err, "entity: upsert fragment")
	}
	got, err := fragmentOf(entitydb.EntityFragment{
		ID: row.ID, WorkspaceID: row.WorkspaceID, Kind: row.Kind, Value: row.Value,
		Origin: row.Origin, FirstSeen: row.FirstSeen, LastSeen: row.LastSeen,
		Observations: row.Observations, JudgementState: row.JudgementState,
		JudgementBy: row.JudgementBy, JudgementAt: row.JudgementAt,
		JudgementReason: row.JudgementReason, CreatedAt: row.CreatedAt,
		ReadAt: row.ReadAt, ReadBy: row.ReadBy,
	})
	return got, row.Inserted, err
}

func (s *Store) FragmentByID(ctx context.Context, workspace, want id.ID) (domain.Fragment, error) {
	row, err := s.q(ctx).FragmentByID(ctx, entitydb.FragmentByIDParams{ID: uuid(want), WorkspaceID: uuid(workspace)})
	if err != nil {
		return domain.Fragment{}, notFound(ctx, err, "entity: read fragment", domain.ErrFragmentNotFound)
	}
	return fragmentOf(row)
}

func (s *Store) Fragments(ctx context.Context, workspace id.ID, kind string, limit int) ([]domain.Fragment, error) {
	rows, err := s.q(ctx).FragmentsForWorkspace(ctx, entitydb.FragmentsForWorkspaceParams{
		WorkspaceID: uuid(workspace), Kind: text(kind), Page: int32(limit),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "entity: fragments")
	}
	return fragments(rows)
}

func (s *Store) JudgeFragment(ctx context.Context, f domain.Fragment) error {
	if err := f.Judgement.Valid(); err != nil {
		return err
	}
	n, err := s.q(ctx).JudgeFragment(ctx, entitydb.JudgeFragmentParams{
		ID: uuid(f.ID), WorkspaceID: uuid(f.WorkspaceID),
		JudgementState: f.Judgement.State.String(), JudgementBy: maybe(f.Judgement.By),
		JudgementAt: stamp(f.Judgement.At), JudgementReason: text(f.Judgement.Reason),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "entity: judge fragment")
	}
	if n == 0 {
		return fmt.Errorf("entity: judge fragment: %w", domain.ErrFragmentNotFound)
	}
	return nil
}

func (s *Store) Attribute(ctx context.Context, a domain.Attribution) error {
	if err := a.Valid(); err != nil {
		return err
	}
	err := s.q(ctx).InsertAttribution(ctx, entitydb.InsertAttributionParams{
		ID: uuid(a.ID), WorkspaceID: uuid(a.WorkspaceID),
		EntityID: uuid(a.EntityID), FragmentID: uuid(a.FragmentID),
		Claimant: a.Claimant.String(), ClaimantRef: maybe(a.ClaimantRef),
		Confidence: confidence(a.Confidence, a.HasConfidence),
		Basis:      a.Basis, State: a.State.String(),
		DecidedAt: stamp(a.DecidedAt), DecidedBy: maybe(a.DecidedBy),
		DecidedNote: text(a.DecidedNote), CreatedAt: stamp(a.CreatedAt),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "entity: attribute")
	}
	return nil
}

func (s *Store) AttributionByID(ctx context.Context, workspace, want id.ID) (domain.Attribution, error) {
	row, err := s.q(ctx).AttributionByID(ctx, entitydb.AttributionByIDParams{
		ID: uuid(want), WorkspaceID: uuid(workspace),
	})
	if err != nil {
		return domain.Attribution{}, notFound(ctx, err, "entity: read attribution", domain.ErrAttributionNotFound)
	}
	return attributionOf(row)
}

func (s *Store) AttributionsFor(ctx context.Context, fragment id.ID) ([]domain.Attribution, error) {
	rows, err := s.q(ctx).AttributionsForFragment(ctx, uuid(fragment))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "entity: attributions")
	}
	return attributions(rows)
}

func (s *Store) AttributionsOn(ctx context.Context, entity id.ID, state string, limit int) ([]domain.Attribution, error) {
	rows, err := s.q(ctx).AttributionsForEntity(ctx, entitydb.AttributionsForEntityParams{
		EntityID: uuid(entity), State: text(state), Page: int32(limit),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "entity: attributions on")
	}
	return attributions(rows)
}

// Decide's predicate refuses a second ruling, so two people accepting at once
// produce one winner and one ErrAlreadyDecided rather than a lost update.
func (s *Store) Decide(ctx context.Context, a domain.Attribution) error {
	if err := a.Valid(); err != nil {
		return err
	}
	n, err := s.q(ctx).DecideAttribution(ctx, entitydb.DecideAttributionParams{
		ID: uuid(a.ID), WorkspaceID: uuid(a.WorkspaceID), State: a.State.String(),
		DecidedAt: stamp(a.DecidedAt), DecidedBy: maybe(a.DecidedBy),
		DecidedNote: text(a.DecidedNote),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "entity: decide")
	}
	if n == 0 {
		return fmt.Errorf("entity: decide: %w", domain.ErrAlreadyDecided)
	}
	return nil
}

// Assets reads the VIEW. The predicate lives in one place rather than in every
// caller that means "asset" — decisions/0009 asked for exactly this.
func (s *Store) Assets(ctx context.Context, workspace, target id.ID, limit int) ([]domain.Asset, error) {
	rows, err := s.q(ctx).Assets(ctx, entitydb.AssetsParams{
		WorkspaceID: uuid(workspace), Target: maybe(target), Page: int32(limit),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "entity: assets")
	}
	out := make([]domain.Asset, 0, len(rows))
	for _, row := range rows {
		f, err := fragmentOf(entitydb.EntityFragment{
			ID: row.ID, WorkspaceID: row.WorkspaceID, Kind: row.Kind, Value: row.Value,
			Origin: row.Origin, FirstSeen: row.FirstSeen, LastSeen: row.LastSeen,
			Observations: row.Observations, JudgementState: row.JudgementState,
			JudgementBy: row.JudgementBy, JudgementAt: row.JudgementAt,
			JudgementReason: row.JudgementReason, CreatedAt: row.CreatedAt,
			ReadAt: row.ReadAt, ReadBy: row.ReadBy,
		})
		if err != nil {
			return nil, err
		}
		claimant, err := domain.ParseClaimant(row.Claimant)
		if err != nil {
			return nil, err
		}
		out = append(out, domain.Asset{
			Fragment: f, AttributionID: ident(row.AttributionID), Claimant: claimant,
			Basis: row.Basis, RootEntityID: ident(row.RootEntityID),
			TargetID: ident(row.TargetID),
		})
	}
	return out, nil
}

// MarkRead writes the read pair and NOTHING ELSE — decisions/0037. A single
// statement that also touched the judgement would be one call doing two acts,
// which is exactly the collapse the columns exist to prevent.
func (s *Store) MarkRead(ctx context.Context, f domain.Fragment) error {
	n, err := s.q(ctx).MarkRead(ctx, entitydb.MarkReadParams{
		ID: uuid(f.ID), WorkspaceID: uuid(f.WorkspaceID),
		ReadAt: stamp(f.ReadAt), ReadBy: maybe(f.ReadBy),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "entity: mark read")
	}
	if n == 0 {
		return fmt.Errorf("entity: mark read: %w", domain.ErrFragmentNotFound)
	}
	return nil
}

func fragments(rows []entitydb.EntityFragment) ([]domain.Fragment, error) {
	out := make([]domain.Fragment, 0, len(rows))
	for _, row := range rows {
		f, err := fragmentOf(row)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

func attributions(rows []entitydb.EntityAttribution) ([]domain.Attribution, error) {
	out := make([]domain.Attribution, 0, len(rows))
	for _, row := range rows {
		a, err := attributionOf(row)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func notFound(ctx context.Context, err error, where string, missing error) error {
	translated := postgres.Translate(ctx, err, where)
	if errors.IsKind(translated, errors.NotFound) {
		return fmt.Errorf("%s: %w", where, missing)
	}
	return translated
}
