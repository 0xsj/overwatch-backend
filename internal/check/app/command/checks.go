package command

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/check/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// Checks writes the questions a firm asks.
type Checks struct {
	repo      Repository
	tools     Tools
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewChecks(repo Repository, tools Tools, tx Transactor,
	publisher events.Publisher, ids Minter, clock Clock) *Checks {
	if repo == nil || tools == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("check: NewChecks with a nil dependency")
	}
	return &Checks{repo: repo, tools: tools, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (c *Checks) Add(ctx context.Context, org, by id.ID, in domain.Draft) (domain.Check, error) {
	fresh, err := domain.New(c.ids.NewID(), org, by, in, c.clock.Now())
	if err != nil {
		return domain.Check{}, err
	}
	if err := c.repo.Create(ctx, fresh); err != nil {
		return domain.Check{}, err
	}
	return fresh, c.emit(ctx, domain.EventCheckAdded, org, domain.Added{
		CheckID: fresh.ID.String(), OrgID: org.String(),
		Name: fresh.Name, AppliesTo: domain.Names(fresh.AppliesTo),
	})
}

// Update changes the question, the interval, the schedule flag — and AppliesTo,
// which is the one that matters.
//
// **Narrowing AppliesTo silently changes every coverage denominator this check
// contributes to**, in every engagement, retroactively. Nothing else records
// that, so the event carries both sets rather than only the new one.
func (c *Checks) Update(ctx context.Context, org, want id.ID, in domain.Draft) (domain.Check, error) {
	held, err := c.repo.ByID(ctx, org, want)
	if err != nil {
		return domain.Check{}, err
	}
	next, err := held.Update(in, c.clock.Now())
	if err != nil {
		return domain.Check{}, err
	}
	if err := c.repo.Save(ctx, next); err != nil {
		return domain.Check{}, err
	}
	return next, c.emit(ctx, domain.EventCheckUpdated, org, domain.Updated{
		CheckID: next.ID.String(), OrgID: org.String(), Name: next.Name,
		From: domain.Names(held.AppliesTo), To: domain.Names(next.AppliesTo),
	})
}

// Archive hides a check and keeps its record, because every run that ever
// happened names it. It releases the NAME, the same partial-index shape the
// other archiving domains use.
func (c *Checks) Archive(ctx context.Context, org, want id.ID) (domain.Check, error) {
	held, err := c.repo.ByID(ctx, org, want)
	if err != nil {
		return domain.Check{}, err
	}
	next, err := held.Archive(c.clock.Now())
	if err != nil {
		return domain.Check{}, err
	}
	if err := c.repo.Save(ctx, next); err != nil {
		return domain.Check{}, err
	}
	return next, c.emit(ctx, domain.EventCheckArchived, org, domain.Archived{
		CheckID: next.ID.String(), OrgID: org.String(), Name: next.Name,
	})
}

// StepDraft is one node as the editor sends it. A ZERO ID means a new step —
// the client cannot mint one, and the alternative (making it send a uuid) puts
// id generation in a browser for no gain.
type StepDraft struct {
	ID     id.ID
	ToolID id.ID
	X, Y   int
	Pinned bool
}

// FlowDraft names two steps by the ids the caller knows. A NEW step has none
// yet, so an edge onto one is expressed by index — see [Checks.SaveChain].
type FlowDraft struct {
	From, To id.ID
	// FromNew and ToNew are indexes into the step list, used when that endpoint
	// is a step being created in this same save. -1 means "use the id".
	FromNew, ToNew int
}

// SaveChain replaces a check's graph.
//
// **New steps get their ids here, before validation**, so a flow can name one.
// That is why FlowDraft carries indexes as well as ids: the editor draws an edge
// between two nodes it has just added, and neither exists yet.
//
// The order is: mint, resolve, VALIDATE, then write. Validating after the write
// would mean a cyclic chain reaching the database and being rolled back, which
// works and reports the wrong error.
func (c *Checks) SaveChain(ctx context.Context, org, want id.ID,
	steps []StepDraft, flows []FlowDraft) (domain.Chain, error) {
	held, err := c.repo.ByID(ctx, org, want)
	if err != nil {
		return domain.Chain{}, err
	}
	if held.Archived() {
		return domain.Chain{}, domain.ErrArchived
	}

	chain := domain.Chain{
		Steps: make([]domain.Step, 0, len(steps)),
		Flows: make([]domain.Flow, 0, len(flows)),
	}
	for _, in := range steps {
		if in.ToolID.IsZero() {
			return domain.Chain{}, domain.ErrToolRequired
		}
		// The tool must be THIS org's. Without this a check could name a tool it
		// cannot see, and the refusal would surface at run time as a missing
		// binary rather than here as a bad reference.
		ok, err := c.tools.Exists(ctx, org, in.ToolID)
		if err != nil {
			return domain.Chain{}, err
		}
		if !ok {
			return domain.Chain{}, domain.ErrToolRequired
		}
		stepID := in.ID
		if stepID.IsZero() {
			stepID = c.ids.NewID()
		}
		chain.Steps = append(chain.Steps, domain.Step{
			ID: stepID, CheckID: want, ToolID: in.ToolID,
			X: in.X, Y: in.Y, Pinned: in.Pinned,
		})
	}

	for _, in := range flows {
		from, err := resolve(chain.Steps, in.From, in.FromNew)
		if err != nil {
			return domain.Chain{}, err
		}
		to, err := resolve(chain.Steps, in.To, in.ToNew)
		if err != nil {
			return domain.Chain{}, err
		}
		chain.Flows = append(chain.Flows, domain.Flow{CheckID: want, From: from, To: to})
	}

	if err := chain.Validate(); err != nil {
		return domain.Chain{}, err
	}

	err = c.tx.InTx(ctx, func(ctx context.Context) error {
		return c.repo.SaveChain(ctx, want, chain)
	})
	if err != nil {
		return domain.Chain{}, err
	}
	return chain, c.emit(ctx, domain.EventChainSaved, org, domain.ChainSaved{
		CheckID: want.String(), OrgID: org.String(),
		Steps: len(chain.Steps), Flows: len(chain.Flows),
	})
}

func resolve(steps []domain.Step, known id.ID, index int) (id.ID, error) {
	if index >= 0 {
		if index >= len(steps) {
			return id.ID{}, domain.ErrStepUnknown
		}
		return steps[index].ID, nil
	}
	if known.IsZero() {
		return id.ID{}, domain.ErrStepUnknown
	}
	return known, nil
}

// emit leaves the envelope UNTENANTED and names the org as the subject —
// decisions/0024 derives the org scope from exactly that, and 0031 says why a
// check has no workspace to be tenanted to.
func (c *Checks) emit(ctx context.Context, name string, org id.ID, payload any) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, c.ids)
	}
	e, err := events.NewDecision(c.ids, c.clock, name,
		domain.SubjectKind+":"+org.String(), prov, payload)
	if err != nil {
		return fmt.Errorf("check: %s: %w", name, err)
	}
	if err := c.publisher.Publish(ctx, e); err != nil {
		return fmt.Errorf("check: %s: %w", name, err)
	}
	return nil
}
