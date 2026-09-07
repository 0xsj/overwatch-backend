package command

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/entity/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// Rulings is what a PERSON does to the graph: judging a fragment or an entity,
// and accepting or rejecting a claim.
//
// Everything here is a DECISION — decisions/0014 — because every one has an
// author and none has an outcome column. That is also why they are the only
// writes in this domain a subscriber does not make.
type Rulings struct {
	repo      Repository
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewRulings(repo Repository, publisher events.Publisher, ids Minter, clock Clock) *Rulings {
	if repo == nil || publisher == nil || ids == nil || clock == nil {
		panic("entity: NewRulings with a nil dependency")
	}
	return &Rulings{repo: repo, publisher: publisher, ids: ids, clock: clock}
}

// JudgeFragment records that a person looked. It is the write that makes
// "what have I never looked at" answerable — PRODUCT.md's third differentiator.
func (r *Rulings) JudgeFragment(ctx context.Context, workspace, want, by id.ID,
	state domain.JudgementState, reason string) (domain.Fragment, error) {
	held, err := r.repo.FragmentByID(ctx, workspace, want)
	if err != nil {
		return domain.Fragment{}, err
	}
	ruled, err := domain.Rule(state, by, reason, r.clock.Now())
	if err != nil {
		return domain.Fragment{}, err
	}
	next := held.Judge(ruled)
	if err := r.repo.JudgeFragment(ctx, next); err != nil {
		return domain.Fragment{}, err
	}
	return next, r.emit(ctx, domain.EventJudgementSet, workspace, domain.JudgementSet{
		WorkspaceID: workspace.String(), SubjectKind: "fragment", SubjectID: want.String(),
		From: held.Judgement.State.String(), To: next.Judgement.State.String(),
		Reason: next.Judgement.Reason,
	})
}

// MarkRead records that a person LOOKED — decisions/0037 §3. It writes the read
// pair and nothing else: reading is not ruling, and a caller wanting both makes
// two calls because they are two acts.
//
// It emits NOTHING. A read is not a decision — nobody is accountable for having
// looked — and it is not work with an outcome either. The record is the column,
// and the coverage grid is what reads it.
func (r *Rulings) MarkRead(ctx context.Context, workspace, want, by id.ID) (domain.Fragment, error) {
	held, err := r.repo.FragmentByID(ctx, workspace, want)
	if err != nil {
		return domain.Fragment{}, err
	}
	read, err := held.Read(by, r.clock.Now())
	if err != nil {
		return domain.Fragment{}, err
	}
	if err := r.repo.MarkRead(ctx, read); err != nil {
		return domain.Fragment{}, err
	}
	return read, nil
}

// JudgeEntity is the identical act on the other table. The duplication is
// 0009's, and it is why `root` holds a test asserting the two column groups
// have the same shape.
func (r *Rulings) JudgeEntity(ctx context.Context, workspace, want, by id.ID,
	state domain.JudgementState, reason string) (domain.Entity, error) {
	held, err := r.repo.EntityByID(ctx, workspace, want)
	if err != nil {
		return domain.Entity{}, err
	}
	ruled, err := domain.Rule(state, by, reason, r.clock.Now())
	if err != nil {
		return domain.Entity{}, err
	}
	next := held.Judge(ruled)
	if err := r.repo.JudgeEntity(ctx, next); err != nil {
		return domain.Entity{}, err
	}
	return next, r.emit(ctx, domain.EventJudgementSet, workspace, domain.JudgementSet{
		WorkspaceID: workspace.String(), SubjectKind: "entity", SubjectID: want.String(),
		From: held.Judgement.State.String(), To: next.Judgement.State.String(),
		Reason: next.Judgement.Reason,
	})
}

// Decide is a person ruling on a proposed claim. **It never rewrites the
// claimant** — decisions/0008's title — and the store's predicate refuses a
// second ruling, so two people accepting at once produce one winner.
//
// Today only a MODEL proposes, and there is no model caller — so this has no
// producer of work yet, and it is built because the review queue is what makes a
// model's claims safe to accept at all.
func (r *Rulings) Decide(ctx context.Context, workspace, want, by id.ID,
	state domain.ClaimState, note string) (domain.Attribution, error) {
	held, err := r.repo.AttributionByID(ctx, workspace, want)
	if err != nil {
		return domain.Attribution{}, err
	}
	next, err := held.Decide(state, by, note, r.clock.Now())
	if err != nil {
		return domain.Attribution{}, err
	}
	if err := r.repo.Decide(ctx, next); err != nil {
		return domain.Attribution{}, err
	}
	name := domain.EventAccepted
	if state == domain.Rejected {
		name = domain.EventRejected
	}
	return next, r.emit(ctx, name, workspace, domain.Claimed{
		AttributionID: next.ID.String(), WorkspaceID: workspace.String(),
		EntityID: next.EntityID.String(), FragmentID: next.FragmentID.String(),
		// The CLAIMANT, unchanged — the whole point of 0008.
		Claimant: next.Claimant.String(), State: next.State.String(),
		Basis: next.Basis, DecidedBy: next.DecidedBy.String(),
	})
}

func (r *Rulings) emit(ctx context.Context, name string, workspace id.ID, payload any) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, r.ids)
	}
	tenanted, err := prov.WithTenant(workspace.String())
	if err != nil {
		return fmt.Errorf("entity: %s: %w", name, err)
	}
	e, err := events.NewDecision(r.ids, r.clock, name,
		domain.SubjectKind+":"+workspace.String(), tenanted, payload)
	if err != nil {
		return fmt.Errorf("entity: %s: %w", name, err)
	}
	if err := r.publisher.Publish(ctx, e); err != nil {
		return fmt.Errorf("entity: %s: %w", name, err)
	}
	return nil
}
