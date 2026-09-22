package command

import (
	"context"
	stderrors "errors"
	"time"

	recorddomain "github.com/0xsj/overwatch-backend/internal/researchentity/domain"
	"github.com/0xsj/overwatch-backend/internal/researchresolution/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type Repository interface {
	Create(context.Context, domain.Resolution) error
	ByID(context.Context, id.ID, id.ID) (domain.Resolution, error)
	Save(context.Context, domain.Resolution) error
	ActiveByAlias(context.Context, id.ID, id.ID) (domain.Resolution, error)
}

type SetRepository interface {
	CreateSet(context.Context, domain.ResolutionSet) error
	BySetID(context.Context, id.ID, id.ID) (domain.ResolutionSet, error)
	SaveSet(context.Context, domain.ResolutionSet) error
	ActiveSetByRecord(context.Context, id.ID, id.ID) (domain.ResolutionSet, error)
}

type Records interface {
	ByID(context.Context, id.ID, id.ID) (recorddomain.Record, error)
	Save(context.Context, recorddomain.Record) error
	ReplaceObservations(context.Context, id.ID, id.ID, []id.ID) error
	CreateRevision(context.Context, recorddomain.Revision) error
}

type Transactor interface {
	InTx(context.Context, func(context.Context) error) error
}
type Minter interface{ NewID() id.ID }
type Clock interface{ Now() time.Time }

type Resolutions struct {
	repo      Repository
	sets      SetRepository
	records   Records
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewResolutions(repo Repository, records Records, tx Transactor, publisher events.Publisher, ids Minter, clock Clock, setRepo ...SetRepository) *Resolutions {
	if repo == nil || records == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("researchresolution: NewResolutions with a nil dependency")
	}
	var sets SetRepository
	if len(setRepo) > 0 {
		sets = setRepo[0]
	}
	return &Resolutions{repo: repo, sets: sets, records: records, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (r *Resolutions) Propose(ctx context.Context, workspace, alias, canonical, proposer id.ID, rationale string) (domain.Resolution, error) {
	if current, err := r.repo.ActiveByAlias(ctx, workspace, alias); err == nil && !current.ID.IsZero() {
		return domain.Resolution{}, domain.ErrAlreadyResolved
	} else if err != nil && !stderrors.Is(err, domain.ErrNotFound) {
		return domain.Resolution{}, err
	}
	aliasRecord, err := r.records.ByID(ctx, workspace, alias)
	if err != nil {
		return domain.Resolution{}, err
	}
	canonicalRecord, err := r.records.ByID(ctx, workspace, canonical)
	if err != nil {
		return domain.Resolution{}, err
	}
	if current, err := r.repo.ActiveByAlias(ctx, workspace, canonical); err == nil && !current.ID.IsZero() {
		return domain.Resolution{}, domain.ErrAlreadyResolved
	} else if err != nil && !stderrors.Is(err, domain.ErrNotFound) {
		return domain.Resolution{}, err
	}
	fresh, err := domain.New(r.ids.NewID(), workspace, alias, canonical, proposer, rationale, r.clock.Now())
	if err != nil {
		return domain.Resolution{}, err
	}
	// Keep the proposed impact in the review row. It is a preview only; Review
	// recalculates it from the records inside the confirmation path so a stale
	// proposal cannot silently overwrite newer authored citations.
	fresh.CanonicalObservationIDsBefore = append([]id.ID(nil), canonicalRecord.ObservationIDs...)
	fresh.AddedObservationIDs = difference(aliasRecord.ObservationIDs, canonicalRecord.ObservationIDs)
	if err := r.tx.InTx(ctx, func(ctx context.Context) error {
		if err := r.repo.Create(ctx, fresh); err != nil {
			return err
		}
		return r.publish(ctx, fresh)
	}); err != nil {
		return domain.Resolution{}, err
	}
	return fresh, nil
}

func (r *Resolutions) Review(ctx context.Context, workspace, resolutionID, reviewer id.ID, decision string) (domain.Resolution, error) {
	held, err := r.repo.ByID(ctx, workspace, resolutionID)
	if err != nil {
		return domain.Resolution{}, err
	}
	var next domain.Resolution
	switch decision {
	case "accept":
		canonical, err := r.records.ByID(ctx, workspace, held.CanonicalRecordID)
		if err != nil {
			return domain.Resolution{}, err
		}
		alias, err := r.records.ByID(ctx, workspace, held.AliasRecordID)
		if err != nil {
			return domain.Resolution{}, err
		}
		added := difference(alias.ObservationIDs, canonical.ObservationIDs)
		merged := append(append([]id.ID(nil), canonical.ObservationIDs...), added...)
		next, err = held.Accept(reviewer, canonical.ObservationIDs, added, r.clock.Now())
		if err != nil {
			return domain.Resolution{}, err
		}
		updated, err := canonical.Edit(reviewer, canonical.Kind.String(), canonical.Name, canonical.Description, merged, next.ReviewedAtValue())
		if err != nil {
			return domain.Resolution{}, err
		}
		if err := r.tx.InTx(ctx, func(ctx context.Context) error {
			if err := r.records.Save(ctx, updated); err != nil {
				return err
			}
			if err := r.records.ReplaceObservations(ctx, workspace, updated.ID, updated.ObservationIDs); err != nil {
				return err
			}
			if err := r.records.CreateRevision(ctx, recorddomain.NewRevision(r.ids.NewID(), updated)); err != nil {
				return err
			}
			if err := r.repo.Save(ctx, next); err != nil {
				return err
			}
			return r.publish(ctx, next)
		}); err != nil {
			return domain.Resolution{}, err
		}
		return next, nil
	case "reject":
		next, err = held.Reject(reviewer, r.clock.Now())
		if err != nil {
			return domain.Resolution{}, err
		}
	default:
		return domain.Resolution{}, domain.ErrDecisionUnknown
	}
	if err := r.tx.InTx(ctx, func(ctx context.Context) error {
		if err := r.repo.Save(ctx, next); err != nil {
			return err
		}
		return r.publish(ctx, next)
	}); err != nil {
		return domain.Resolution{}, err
	}
	return next, nil
}

func (r *Resolutions) Reverse(ctx context.Context, workspace, resolutionID, reviewer id.ID) (domain.Resolution, error) {
	held, err := r.repo.ByID(ctx, workspace, resolutionID)
	if err != nil {
		return domain.Resolution{}, err
	}
	canonical, err := r.records.ByID(ctx, workspace, held.CanonicalRecordID)
	if err != nil {
		return domain.Resolution{}, err
	}
	remaining := remove(canonical.ObservationIDs, held.AddedObservationIDs)
	next, err := held.Reverse(reviewer, r.clock.Now())
	if err != nil {
		return domain.Resolution{}, err
	}
	updated, err := canonical.Edit(reviewer, canonical.Kind.String(), canonical.Name, canonical.Description, remaining, next.ReversedAtValue())
	if err != nil {
		return domain.Resolution{}, err
	}
	if err := r.tx.InTx(ctx, func(ctx context.Context) error {
		if err := r.records.Save(ctx, updated); err != nil {
			return err
		}
		if err := r.records.ReplaceObservations(ctx, workspace, updated.ID, updated.ObservationIDs); err != nil {
			return err
		}
		if err := r.records.CreateRevision(ctx, recorddomain.NewRevision(r.ids.NewID(), updated)); err != nil {
			return err
		}
		if err := r.repo.Save(ctx, next); err != nil {
			return err
		}
		return r.publish(ctx, next)
	}); err != nil {
		return domain.Resolution{}, err
	}
	return next, nil
}

func (r *Resolutions) ProposeSet(ctx context.Context, workspace, canonical id.ID, aliases []id.ID, proposer id.ID, rationale string) (domain.ResolutionSet, error) {
	if r.sets == nil {
		return domain.ResolutionSet{}, domain.ErrNotFound
	}
	fresh, err := domain.NewSet(r.ids.NewID(), workspace, canonical, proposer, aliases, rationale, r.clock.Now())
	if err != nil {
		return domain.ResolutionSet{}, err
	}
	canonicalRecord, err := r.records.ByID(ctx, workspace, canonical)
	if err != nil {
		return domain.ResolutionSet{}, err
	}
	added := make([]id.ID, 0)
	for _, alias := range fresh.AliasRecordIDs {
		if _, err := r.activeRecordResolution(ctx, workspace, alias); err != nil {
			return domain.ResolutionSet{}, err
		}
		aliasRecord, err := r.records.ByID(ctx, workspace, alias)
		if err != nil {
			return domain.ResolutionSet{}, err
		}
		added = append(added, difference(aliasRecord.ObservationIDs, append(canonicalRecord.ObservationIDs, added...))...)
	}
	if _, err := r.activeRecordResolution(ctx, workspace, canonical); err != nil {
		return domain.ResolutionSet{}, err
	}
	fresh.CanonicalObservationIDsBefore = append([]id.ID(nil), canonicalRecord.ObservationIDs...)
	fresh.AddedObservationIDs = difference(added, nil)
	if len(domainObservationUnion(fresh.CanonicalObservationIDsBefore, fresh.AddedObservationIDs)) > 12 {
		return domain.ResolutionSet{}, domain.ErrObservationTooMany
	}
	if err := r.tx.InTx(ctx, func(ctx context.Context) error {
		if err := r.sets.CreateSet(ctx, fresh); err != nil {
			return err
		}
		return r.publishSet(ctx, fresh)
	}); err != nil {
		return domain.ResolutionSet{}, err
	}
	return fresh, nil
}

func (r *Resolutions) ReviewSet(ctx context.Context, workspace, resolutionID, reviewer id.ID, decision string) (domain.ResolutionSet, error) {
	if r.sets == nil {
		return domain.ResolutionSet{}, domain.ErrNotFound
	}
	held, err := r.sets.BySetID(ctx, workspace, resolutionID)
	if err != nil {
		return domain.ResolutionSet{}, err
	}
	var next domain.ResolutionSet
	switch decision {
	case "accept":
		canonical, err := r.records.ByID(ctx, workspace, held.CanonicalRecordID)
		if err != nil {
			return domain.ResolutionSet{}, err
		}
		added := make([]id.ID, 0)
		for _, aliasID := range held.AliasRecordIDs {
			alias, err := r.records.ByID(ctx, workspace, aliasID)
			if err != nil {
				return domain.ResolutionSet{}, err
			}
			added = append(added, difference(alias.ObservationIDs, append(canonical.ObservationIDs, added...))...)
		}
		merged := append(append([]id.ID(nil), canonical.ObservationIDs...), added...)
		next, err = held.Accept(reviewer, canonical.ObservationIDs, added, r.clock.Now())
		if err != nil {
			return domain.ResolutionSet{}, err
		}
		updated, err := canonical.Edit(reviewer, canonical.Kind.String(), canonical.Name, canonical.Description, merged, next.ReviewedAtValue())
		if err != nil {
			return domain.ResolutionSet{}, err
		}
		if err := r.tx.InTx(ctx, func(ctx context.Context) error {
			if err := r.records.Save(ctx, updated); err != nil {
				return err
			}
			if err := r.records.ReplaceObservations(ctx, workspace, updated.ID, updated.ObservationIDs); err != nil {
				return err
			}
			if err := r.records.CreateRevision(ctx, recorddomain.NewRevision(r.ids.NewID(), updated)); err != nil {
				return err
			}
			if err := r.sets.SaveSet(ctx, next); err != nil {
				return err
			}
			return r.publishSet(ctx, next)
		}); err != nil {
			return domain.ResolutionSet{}, err
		}
		return next, nil
	case "reject":
		next, err = held.Reject(reviewer, r.clock.Now())
		if err != nil {
			return domain.ResolutionSet{}, err
		}
	default:
		return domain.ResolutionSet{}, domain.ErrDecisionUnknown
	}
	if err := r.tx.InTx(ctx, func(ctx context.Context) error {
		if err := r.sets.SaveSet(ctx, next); err != nil {
			return err
		}
		return r.publishSet(ctx, next)
	}); err != nil {
		return domain.ResolutionSet{}, err
	}
	return next, nil
}

func (r *Resolutions) ReverseSet(ctx context.Context, workspace, resolutionID, reviewer id.ID) (domain.ResolutionSet, error) {
	if r.sets == nil {
		return domain.ResolutionSet{}, domain.ErrNotFound
	}
	held, err := r.sets.BySetID(ctx, workspace, resolutionID)
	if err != nil {
		return domain.ResolutionSet{}, err
	}
	canonical, err := r.records.ByID(ctx, workspace, held.CanonicalRecordID)
	if err != nil {
		return domain.ResolutionSet{}, err
	}
	remaining := remove(canonical.ObservationIDs, held.AddedObservationIDs)
	next, err := held.Reverse(reviewer, r.clock.Now())
	if err != nil {
		return domain.ResolutionSet{}, err
	}
	updated, err := canonical.Edit(reviewer, canonical.Kind.String(), canonical.Name, canonical.Description, remaining, next.ReversedAtValue())
	if err != nil {
		return domain.ResolutionSet{}, err
	}
	if err := r.tx.InTx(ctx, func(ctx context.Context) error {
		if err := r.records.Save(ctx, updated); err != nil {
			return err
		}
		if err := r.records.ReplaceObservations(ctx, workspace, updated.ID, updated.ObservationIDs); err != nil {
			return err
		}
		if err := r.records.CreateRevision(ctx, recorddomain.NewRevision(r.ids.NewID(), updated)); err != nil {
			return err
		}
		if err := r.sets.SaveSet(ctx, next); err != nil {
			return err
		}
		return r.publishSet(ctx, next)
	}); err != nil {
		return domain.ResolutionSet{}, err
	}
	return next, nil
}

func (r *Resolutions) activeRecordResolution(ctx context.Context, workspace, record id.ID) (domain.ResolutionSet, error) {
	if current, err := r.repo.ActiveByAlias(ctx, workspace, record); err == nil && !current.ID.IsZero() {
		return domain.ResolutionSet{}, domain.ErrAlreadyResolved
	} else if err != nil && !stderrors.Is(err, domain.ErrNotFound) {
		return domain.ResolutionSet{}, err
	}
	if current, err := r.sets.ActiveSetByRecord(ctx, workspace, record); err == nil && !current.ID.IsZero() {
		return domain.ResolutionSet{}, domain.ErrAlreadyResolved
	} else if err != nil && !stderrors.Is(err, domain.ErrNotFound) {
		return domain.ResolutionSet{}, err
	}
	return domain.ResolutionSet{}, nil
}

func domainObservationUnion(left, right []id.ID) []id.ID {
	return append(append([]id.ID(nil), left...), right...)
}

func (r *Resolutions) publish(ctx context.Context, resolution domain.Resolution) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, r.ids)
	}
	prov, err := prov.WithTenant(resolution.WorkspaceID.String())
	if err != nil {
		return err
	}
	event, err := events.NewDecision(r.ids, r.clock, domain.EventChanged, "workspace:"+resolution.WorkspaceID.String(), prov, domain.Changed{
		WorkspaceID: resolution.WorkspaceID.String(), ResolutionID: resolution.ID.String(), AliasRecordID: resolution.AliasRecordID.String(), CanonicalRecordID: resolution.CanonicalRecordID.String(), State: resolution.State.String(), ChangedBy: actor(resolution).String(),
	})
	if err != nil {
		return err
	}
	return r.publisher.Publish(ctx, event)
}

func (r *Resolutions) publishSet(ctx context.Context, resolution domain.ResolutionSet) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, r.ids)
	}
	prov, err := prov.WithTenant(resolution.WorkspaceID.String())
	if err != nil {
		return err
	}
	event, err := events.NewDecision(r.ids, r.clock, domain.EventChanged, "workspace:"+resolution.WorkspaceID.String(), prov, domain.Changed{
		WorkspaceID: resolution.WorkspaceID.String(), ResolutionID: resolution.ID.String(), AliasRecordID: "set:" + resolution.ID.String(), CanonicalRecordID: resolution.CanonicalRecordID.String(), State: resolution.State.String(), ChangedBy: actorSet(resolution).String(),
	})
	if err != nil {
		return err
	}
	return r.publisher.Publish(ctx, event)
}

func actorSet(resolution domain.ResolutionSet) id.ID {
	if resolution.State == domain.Reversed {
		return resolution.ReversedBy
	}
	if resolution.ReviewedBy.IsZero() {
		return resolution.ProposedBy
	}
	return resolution.ReviewedBy
}

func actor(resolution domain.Resolution) id.ID {
	if resolution.State == domain.Reversed {
		return resolution.ReversedBy
	}
	if resolution.ReviewedBy.IsZero() {
		return resolution.ProposedBy
	}
	return resolution.ReviewedBy
}

func difference(source, existing []id.ID) []id.ID {
	known := make(map[id.ID]struct{}, len(existing))
	for _, one := range existing {
		known[one] = struct{}{}
	}
	out := make([]id.ID, 0, len(source))
	for _, one := range source {
		if _, ok := known[one]; !ok {
			out = append(out, one)
		}
	}
	return out
}

func remove(source, values []id.ID) []id.ID {
	removed := make(map[id.ID]struct{}, len(values))
	for _, one := range values {
		removed[one] = struct{}{}
	}
	out := make([]id.ID, 0, len(source))
	for _, one := range source {
		if _, ok := removed[one]; !ok {
			out = append(out, one)
		}
	}
	return out
}
