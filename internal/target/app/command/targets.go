package command

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/target/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// Targets adds and changes the things being looked at.
//
// **Nothing here authorises.** The caller's reach is org's, resolved at the
// composition root before any of this runs — `write` to add or rename, `admin`
// to archive, read off decisions/0019's ladder rather than invented here.
type Targets struct {
	repo      Repository
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewTargets(repo Repository, publisher events.Publisher, ids Minter, clock Clock) *Targets {
	if repo == nil || publisher == nil || ids == nil || clock == nil {
		panic("target: NewTargets with a nil dependency")
	}
	return &Targets{repo: repo, publisher: publisher, ids: ids, clock: clock}
}

func (t *Targets) Add(ctx context.Context, workspace, by id.ID, name string, kind domain.Kind) (domain.Target, error) {
	fresh, err := domain.New(t.ids.NewID(), workspace, by, name, kind, t.clock.Now())
	if err != nil {
		return domain.Target{}, err
	}
	if err := t.repo.Create(ctx, fresh); err != nil {
		return domain.Target{}, err
	}
	return fresh, t.emit(ctx, domain.EventTargetAdded, fresh, domain.Added{
		TargetID: fresh.ID.String(), WorkspaceID: workspace.String(),
		Name: fresh.Name, Kind: fresh.Kind.String(),
	})
}

func (t *Targets) Rename(ctx context.Context, workspace, want id.ID, name string) (domain.Target, error) {
	held, err := t.repo.ByID(ctx, workspace, want)
	if err != nil {
		return domain.Target{}, err
	}
	next, err := held.Rename(name, t.clock.Now())
	if err != nil {
		return domain.Target{}, err
	}
	if next.Name == held.Name {
		return held, nil
	}
	if err := t.repo.Save(ctx, next); err != nil {
		return domain.Target{}, err
	}
	return next, t.emit(ctx, domain.EventTargetRenamed, next, domain.Renamed{
		TargetID: next.ID.String(), WorkspaceID: workspace.String(),
		From: held.Name, To: next.Name,
	})
}

// Archive hides a target and keeps the record. It releases the name, so a
// reopen can collide — the same shape as closing an engagement (0027), and the
// same refusal, because two live targets sharing a name is invisible on a list.
func (t *Targets) Archive(ctx context.Context, workspace, want id.ID) (domain.Target, error) {
	held, err := t.repo.ByID(ctx, workspace, want)
	if err != nil {
		return domain.Target{}, err
	}
	next, err := held.Archive(t.clock.Now())
	if err != nil {
		return domain.Target{}, err
	}
	if err := t.repo.Save(ctx, next); err != nil {
		return domain.Target{}, err
	}
	return next, t.emit(ctx, domain.EventTargetArchived, next, domain.Archived{
		TargetID: next.ID.String(), WorkspaceID: workspace.String(), Name: next.Name,
	})
}

func (t *Targets) Reopen(ctx context.Context, workspace, want id.ID) (domain.Target, error) {
	held, err := t.repo.ByID(ctx, workspace, want)
	if err != nil {
		return domain.Target{}, err
	}
	next, err := held.Reopen(t.clock.Now())
	if err != nil {
		return domain.Target{}, err
	}
	if err := t.repo.Save(ctx, next); err != nil {
		return domain.Target{}, err
	}
	return next, t.emit(ctx, domain.EventTargetReopened, next, domain.Reopened{
		TargetID: next.ID.String(), WorkspaceID: workspace.String(), Name: next.Name,
	})
}

// emit tenants every event to the workspace, so it lands on that ENGAGEMENT's
// audit log — which is where somebody asks what was looked at and when.
//
// An untenanted one lands under `scope = system` and appears on no screen at
// all, while every test that counts rows still passes.
func (t *Targets) emit(ctx context.Context, name string, at domain.Target, payload any) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, t.ids)
	}
	tenanted, err := prov.WithTenant(at.WorkspaceID.String())
	if err != nil {
		return fmt.Errorf("target: %s: %w", name, err)
	}
	e, err := events.NewDecision(t.ids, t.clock, name,
		domain.SubjectKind+":"+at.ID.String(), tenanted, payload)
	if err != nil {
		return fmt.Errorf("target: %s: %w", name, err)
	}
	if err := t.publisher.Publish(ctx, e); err != nil {
		return fmt.Errorf("target: %s: %w", name, err)
	}
	return nil
}
