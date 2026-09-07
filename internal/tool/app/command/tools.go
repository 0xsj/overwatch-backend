package command

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/tool/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// Tools adds and changes what the firm can run.
type Tools struct {
	repo      Repository
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewTools(repo Repository, publisher events.Publisher, ids Minter, clock Clock) *Tools {
	if repo == nil || publisher == nil || ids == nil || clock == nil {
		panic("tool: NewTools with a nil dependency")
	}
	return &Tools{repo: repo, publisher: publisher, ids: ids, clock: clock}
}

// Definition is what a caller asks for, already parsed — the transport owns the
// vocabulary and this owns the rules about it. It is a struct rather than six
// arguments because four of them are strings and two are enums, and a call site
// that transposes two strings compiles.
type Definition struct {
	Name      string
	Argv      string
	Intensity domain.Intensity
	Consumes  domain.Feed
	Produces  domain.Feed
	// Success is which exit codes mean the tool answered — decisions/0033.
	// Empty means {0}, normalised in the domain.
	Success []int
}

func (t *Tools) Add(ctx context.Context, org, by id.ID, in Definition) (domain.Tool, error) {
	fresh, err := domain.New(t.ids.NewID(), org, by, in.Name, in.Argv, in.Intensity,
		in.Consumes, in.Produces, in.Success, t.clock.Now())
	if err != nil {
		return domain.Tool{}, err
	}
	if err := t.repo.Create(ctx, fresh); err != nil {
		return domain.Tool{}, err
	}
	return fresh, t.emit(ctx, domain.EventToolAdded, org, domain.ToolAdded{
		ToolID: fresh.ID.String(), OrgID: org.String(),
		Name: fresh.Name, Intensity: fresh.Intensity.String(),
	})
}

// Update changes the argv template or the intensity.
//
// **Raising an intensity is a scope change everywhere at once.** A range in
// scope for passive collection is not thereby in scope for a loud scan (0010),
// so a tool promoted from passive to loud stops being permitted by every rule
// that qualified it — and no rule was edited. The event carries both
// intensities so that shift is visible in the firm's own log.
func (t *Tools) Update(ctx context.Context, org, want id.ID, in Definition) (domain.Tool, error) {
	held, err := t.repo.ByID(ctx, org, want)
	if err != nil {
		return domain.Tool{}, err
	}
	next, err := held.Update(in.Argv, in.Intensity, in.Consumes, in.Produces,
		in.Success, t.clock.Now())
	if err != nil {
		return domain.Tool{}, err
	}
	if next.Argv == held.Argv && next.Intensity == held.Intensity &&
		next.Consumes == held.Consumes && next.Produces == held.Produces &&
		sameCodes(next.SuccessExitCodes, held.SuccessExitCodes) {
		return held, nil
	}
	if err := t.repo.Save(ctx, next); err != nil {
		return domain.Tool{}, err
	}
	return next, t.emit(ctx, domain.EventToolUpdated, org, domain.ToolUpdated{
		ToolID: next.ID.String(), OrgID: org.String(),
		From: held.Intensity.String(), To: next.Intensity.String(),
	})
}

// Archive takes a tool out of use and keeps its record, because every invocation
// that ever ran names it. It releases the NAME, the same way archiving a target
// or closing an engagement does.
func (t *Tools) Archive(ctx context.Context, org, want id.ID) (domain.Tool, error) {
	held, err := t.repo.ByID(ctx, org, want)
	if err != nil {
		return domain.Tool{}, err
	}
	next, err := held.Archive(t.clock.Now())
	if err != nil {
		return domain.Tool{}, err
	}
	if err := t.repo.Save(ctx, next); err != nil {
		return domain.Tool{}, err
	}
	return next, t.emit(ctx, domain.EventToolArchived, org, domain.ToolArchived{
		ToolID: next.ID.String(), OrgID: org.String(), Name: next.Name,
	})
}

// emit leaves the envelope UNTENANTED and names the org as the subject —
// decisions/0024 derives the org scope from exactly that, and 0031 says why a
// tool has no workspace to be tenanted to. A tenant here would file the firm's
// own history under one arbitrary engagement.
func (t *Tools) emit(ctx context.Context, name string, org id.ID, payload any) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, t.ids)
	}
	e, err := events.NewDecision(t.ids, t.clock, name,
		domain.SubjectKind+":"+org.String(), prov, payload)
	if err != nil {
		return fmt.Errorf("tool: %s: %w", name, err)
	}
	if err := t.publisher.Publish(ctx, e); err != nil {
		return fmt.Errorf("tool: %s: %w", name, err)
	}
	return nil
}

func sameCodes(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
