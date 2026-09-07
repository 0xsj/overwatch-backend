package command

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/run/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// Runs starts a run by PLANNING it in full. Nothing spawns here — the executor
// picks the plan up, which is what keeps a scan off the request's thread.
type Runs struct {
	repo      Repository
	chains    Chains
	targets   Targets
	spawns    Spawns
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewRuns(repo Repository, chains Chains, targets Targets, spawns Spawns,
	tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Runs {
	if repo == nil || chains == nil || targets == nil || spawns == nil ||
		tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("run: NewRuns with a nil dependency")
	}
	return &Runs{repo: repo, chains: chains, targets: targets, spawns: spawns,
		tx: tx, publisher: publisher, ids: ids, clock: clock}
}

// Planned is what a caller gets back, and what a PREVIEW would get without the
// write — decisions/0033 §1. The two are one computation on purpose: two
// implementations of the spawn walk drift, and the one that drifts is the
// preview, which is what a person reads before authorising a scan.
type Planned struct {
	Run         domain.Run
	Invocations []domain.Invocation
}

// Counts is the summary the envelope carries. A subscriber cannot compute it —
// decisions/0013 — and "we planned six and the gate refused four" is the
// sentence somebody wants without opening the run.
func (p Planned) Counts() (planned, refused, skipped int) {
	for _, i := range p.Invocations {
		planned++
		switch i.Phase {
		case domain.PhaseRefused:
			refused++
		case domain.PhaseSkipped:
			skipped++
		}
	}
	return planned, refused, skipped
}

// Preview walks the plan WITHOUT writing anything. It is the client's
// SpawnPreview — "the thing n8n structurally cannot do, because n8n has no
// notion of scope" — and it is the same function [Runs.Start] uses.
func (r *Runs) Preview(ctx context.Context, workspace, target, check id.ID) (Planned, error) {
	return r.plan(ctx, workspace, target, check, id.ID{})
}

// Start plans and persists. The run is `running` with every invocation written,
// and the executor claims it — so a crash between here and the first spawn
// leaves a visible plan rather than nothing.
func (r *Runs) Start(ctx context.Context, workspace, target, check, by id.ID) (Planned, error) {
	planned, err := r.plan(ctx, workspace, target, check, by)
	if err != nil {
		return Planned{}, err
	}

	err = r.tx.InTx(ctx, func(ctx context.Context) error {
		if err := r.repo.Create(ctx, planned.Run); err != nil {
			return err
		}
		for _, i := range planned.Invocations {
			if err := r.repo.PlanInvocation(ctx, i); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Planned{}, err
	}

	total, refused, skipped := planned.Counts()
	return planned, r.decide(ctx, domain.EventRunStarted, planned.Run, domain.Started{
		RunID: planned.Run.ID.String(), WorkspaceID: workspace.String(),
		TargetID: target.String(), CheckID: check.String(),
		Planned: total, Refused: refused, Skipped: skipped,
	})
}

// plan is the whole of decisions/0033 §1: resolve the chain, resolve the target,
// ask the SPAWN GATE for every step, and write the answer into the invocation
// before anything runs.
func (r *Runs) plan(ctx context.Context, workspace, target, check, by id.ID) (Planned, error) {
	steps, _, err := r.chains.Steps(ctx, workspace, check)
	if err != nil {
		return Planned{}, err
	}
	name, err := r.targets.Name(ctx, workspace, target)
	if err != nil {
		return Planned{}, err
	}
	slots, err := domain.Outline(steps, name)
	if err != nil {
		return Planned{}, err
	}

	now := r.clock.Now()
	fresh, err := domain.New(r.ids.NewID(), workspace, target, check, by, now)
	if err != nil {
		return Planned{}, err
	}

	out := Planned{Run: fresh, Invocations: make([]domain.Invocation, 0, len(slots))}
	for n, slot := range slots {
		planned, err := domain.Plan(r.ids.NewID(), fresh.ID, workspace,
			slot.Step.StepID, slot.Step.ToolID, n, slot.Argv,
			slot.Step.Kind, slot.Value)
		if err != nil {
			return Planned{}, err
		}

		// A step that cannot run today is skipped WITHOUT asking the gate. The
		// gate answers "may this spawn"; there is no spawn to ask about, and a
		// refusal recorded for something that was never going to run would put a
		// rule citation on a row that proves nothing.
		if slot.Skip != "" {
			planned, err = planned.Skip(slot.Skip, now)
			if err != nil {
				return Planned{}, err
			}
			out.Invocations = append(out.Invocations, planned)
			continue
		}

		gate, err := r.spawns.MaySpawn(ctx, workspace, target,
			slot.Step.Kind, slot.Value, slot.Intensity)
		if err != nil {
			return Planned{}, err
		}
		switch {
		case gate.Permitted:
			// Left PENDING — this is the only path to a process — but the RULE
			// that permitted it is recorded, because PRODUCT.md's lineage walks
			// back to "the scope rule that allowed the command to run" and
			// nothing else records it.
			planned = planned.Permit(gate.Rule)
		case gate.Rule.IsZero():
			planned, err = planned.NotInScope(gate.Reason, now)
		default:
			planned, err = planned.Refuse(gate.Rule, gate.Reason, now)
		}
		if err != nil {
			return Planned{}, err
		}
		out.Invocations = append(out.Invocations, planned)
	}
	return out, nil
}

// Loud says whether starting this check needs `admin` rather than `write` —
// decisions/0033 §7. It is computed from the chain's tools so nobody keeps a
// second copy of "is this check loud", and it is asked at the composition root
// before Start is called.
func (r *Runs) Loud(ctx context.Context, workspace, check id.ID) (bool, error) {
	_, loud, err := r.chains.Steps(ctx, workspace, check)
	return loud, err
}

// decide publishes a DECISION — decisions/0014. A person chose to look at a
// client: that has an author, no outcome column, and a reason to outlive the
// journal's retention.
func (r *Runs) decide(ctx context.Context, name string, of domain.Run, payload any) error {
	return r.publish(ctx, name, of, payload, true)
}

// work publishes a unit of work: it has an outcome, and the outcome is the
// point.
func (r *Runs) work(ctx context.Context, name string, of domain.Run, payload any) error {
	return r.publish(ctx, name, of, payload, false)
}

func (r *Runs) publish(ctx context.Context, name string, of domain.Run, payload any, decision bool) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, r.ids)
	}
	// Tenanted to the WORKSPACE, so it lands on that engagement's audit log —
	// which is where somebody asks what was run against this client and when.
	tenanted, err := prov.WithTenant(of.WorkspaceID.String())
	if err != nil {
		return fmt.Errorf("run: %s: %w", name, err)
	}
	subject := "workspace:" + of.WorkspaceID.String()
	var e events.Event
	if decision {
		e, err = events.NewDecision(r.ids, r.clock, name, subject, tenanted, payload)
	} else {
		e, err = events.New(r.ids, r.clock, name, subject, tenanted, payload)
	}
	if err != nil {
		return fmt.Errorf("run: %s: %w", name, err)
	}
	if err := r.publisher.Publish(ctx, e); err != nil {
		return fmt.Errorf("run: %s: %w", name, err)
	}
	return nil
}
