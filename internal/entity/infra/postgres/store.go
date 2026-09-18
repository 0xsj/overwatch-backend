package postgres

import (
	"github.com/jackc/pgx/v5/pgtype"

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

// FragmentFor is the lookup a derivation's `from` resolves through —
// decisions/0040 §5. NOT FOUND is an ordinary answer and not an error: the two
// reasons it happens are a candidate the scope gate refused and a fold
// mismatch, and both are things the caller records rather than fails on.
func (s *Store) FragmentFor(ctx context.Context, workspace id.ID, kind, value string) (domain.Fragment, bool, error) {
	row, err := s.q(ctx).FragmentForValue(ctx, entitydb.FragmentForValueParams{
		WorkspaceID: uuid(workspace), Kind: kind, Value: domain.Fold(value),
	})
	if err != nil {
		translated := postgres.Translate(ctx, err, "entity: fragment for value")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Fragment{}, false, nil
		}
		return domain.Fragment{}, false, translated
	}
	got, err := fragmentOf(row)
	return got, err == nil, err
}

// Draw writes one of `0003`'s second edge kind. Idempotent on redelivery.
func (s *Store) Draw(ctx context.Context, d domain.Derivation) error {
	err := s.q(ctx).InsertDerivation(ctx, entitydb.InsertDerivationParams{
		ID: uuid(d.ID), WorkspaceID: uuid(d.WorkspaceID),
		FromFragmentID: uuid(d.From), ToFragmentID: uuid(d.To), Label: d.Label,
		InvocationID: uuid(d.Invocation), ArtifactID: uuid(d.Artifact),
		MappingID: uuid(d.Mapping), CreatedAt: stamp(d.CreatedAt),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "entity: draw derivation")
	}
	return nil
}

// RecordUnresolved keeps a provenance nothing could be found for — 0040 §5.
func (s *Store) RecordUnresolved(ctx context.Context, u domain.Unresolved) error {
	err := s.q(ctx).InsertUnresolved(ctx, entitydb.InsertUnresolvedParams{
		ID: uuid(u.ID), WorkspaceID: uuid(u.WorkspaceID),
		InvocationID: uuid(u.Invocation), MappingID: uuid(u.Mapping),
		ToFragmentID: uuid(u.To), FromKind: u.FromKind, FromValue: u.FromValue,
		FromRaw: u.FromRaw, Label: u.Label, CreatedAt: stamp(u.CreatedAt),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "entity: record unresolved provenance")
	}
	return nil
}

// RootsPerFragment is `0044` §2's `seen elsewhere`, batched: one query for the
// whole canvas rather than one per node.
func (s *Store) RootsPerFragment(ctx context.Context, workspace id.ID, fragments []id.ID) (map[id.ID]int, error) {
	if len(fragments) == 0 {
		return map[id.ID]int{}, nil
	}
	ids := make([]pgtype.UUID, 0, len(fragments))
	for _, one := range fragments {
		ids = append(ids, uuid(one))
	}
	rows, err := s.q(ctx).RootsPerFragment(ctx, entitydb.RootsPerFragmentParams{
		WorkspaceID: uuid(workspace), Fragments: ids,
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "entity: roots per fragment")
	}
	out := make(map[id.ID]int, len(rows))
	for _, row := range rows {
		out[ident(row.FragmentID)] = int(row.Roots)
	}
	return out, nil
}

// DerivationsAmong is every edge with BOTH ends on the canvas — 0044 §1.
func (s *Store) DerivationsAmong(ctx context.Context, workspace id.ID, fragments []id.ID) ([]domain.Derivation, error) {
	if len(fragments) == 0 {
		return nil, nil
	}
	ids := make([]pgtype.UUID, 0, len(fragments))
	for _, one := range fragments {
		ids = append(ids, uuid(one))
	}
	rows, err := s.q(ctx).DerivationsAmong(ctx, entitydb.DerivationsAmongParams{
		WorkspaceID: uuid(workspace), Fragments: ids,
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "entity: derivations among")
	}
	out := make([]domain.Derivation, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Derivation{
			ID: ident(row.ID), WorkspaceID: ident(row.WorkspaceID),
			From: ident(row.FromFragmentID), To: ident(row.ToFragmentID),
			Label: row.Label, Invocation: ident(row.InvocationID),
			Artifact: ident(row.ArtifactID), Mapping: ident(row.MappingID),
			CreatedAt: instant(row.CreatedAt),
		})
	}
	return out, nil
}

// Derivations answers BOTH DIRECTIONS — the canvas draws outward from a
// fragment and does not care which end it is on.
func (s *Store) Derivations(ctx context.Context, workspace, fragment id.ID, limit int) ([]domain.Derivation, error) {
	rows, err := s.q(ctx).DerivationsForFragment(ctx, entitydb.DerivationsForFragmentParams{
		WorkspaceID: uuid(workspace), FromFragmentID: uuid(fragment), Page: int32(limit),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "entity: derivations")
	}
	out := make([]domain.Derivation, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Derivation{
			ID: ident(row.ID), WorkspaceID: ident(row.WorkspaceID),
			From: ident(row.FromFragmentID), To: ident(row.ToFragmentID),
			Label: row.Label, Invocation: ident(row.InvocationID),
			Artifact: ident(row.ArtifactID), Mapping: ident(row.MappingID),
			CreatedAt: instant(row.CreatedAt),
		})
	}
	return out, nil
}

// Unresolved is the other half of the same read — what a rule refused, or what
// a fold missed, seen from the invocation that cited it.
func (s *Store) Unresolved(ctx context.Context, workspace, invocation id.ID) ([]domain.Unresolved, error) {
	rows, err := s.q(ctx).UnresolvedForInvocation(ctx, entitydb.UnresolvedForInvocationParams{
		WorkspaceID: uuid(workspace), InvocationID: uuid(invocation),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "entity: unresolved provenance")
	}
	out := make([]domain.Unresolved, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Unresolved{
			ID: ident(row.ID), WorkspaceID: ident(row.WorkspaceID),
			Invocation: ident(row.InvocationID), Mapping: ident(row.MappingID),
			To: ident(row.ToFragmentID), FromKind: row.FromKind,
			FromValue: row.FromValue, FromRaw: row.FromRaw, Label: row.Label,
			CreatedAt: instant(row.CreatedAt),
		})
	}
	return out, nil
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
	return assets(rows)
}

func (s *Store) AllAssets(ctx context.Context, workspace, target id.ID) ([]domain.Asset, error) {
	rows, err := s.q(ctx).AllAssets(ctx, entitydb.AllAssetsParams{
		WorkspaceID: uuid(workspace), Target: maybe(target),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "entity: all assets")
	}
	return assets(rows)
}

func (s *Store) AcceptedFragmentsForTarget(ctx context.Context, workspace, target id.ID) ([]id.ID, error) {
	rows, err := s.q(ctx).AcceptedFragmentsForTarget(ctx, entitydb.AcceptedFragmentsForTargetParams{
		WorkspaceID: uuid(workspace), TargetID: uuid(target),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "entity: target fragments")
	}
	out := make([]id.ID, 0, len(rows))
	for _, row := range rows {
		out = append(out, ident(row))
	}
	return out, nil
}

func assets(rows []entitydb.EntityAsset) ([]domain.Asset, error) {
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
