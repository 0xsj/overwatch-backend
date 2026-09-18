package command

import (
	"context"
	"fmt"
	"time"

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

	// Candidates is what each invocation was AIMED AT, and at plan time only a
	// source step has any — decisions/0039 Section 3. A downstream step's are
	// resolved when it runs, from what its feeders observed.
	//
	// They travel beside the invocations rather than inside them because a
	// candidate is a row: an invocation carrying a slice would make the refused
	// ones a field of the thing that did not touch them.
	Candidates []domain.Candidate
}

// CandidatesOf groups a plan's candidates by the invocation they belong to, for
// a caller rendering one step at a time.
func (p Planned) CandidatesOf(invocation id.ID) []domain.Candidate {
	out := make([]domain.Candidate, 0, 1)
	for _, c := range p.Candidates {
		if c.InvocationID == invocation {
			out = append(out, c)
		}
	}
	return out
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
		// AFTER the invocations, because a candidate names one.
		for _, c := range planned.Candidates {
			if err := r.repo.AddCandidate(ctx, c); err != nil {
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
			slot.Step.StepID, slot.Step.ToolID, n, slot.Argv, slot.Step.Upstream)
		if err != nil {
			return Planned{}, err
		}

		// A DOWNSTREAM step is left PENDING with its template unresolved. The
		// gate is not asked because there is nothing to ask about yet: its
		// candidates are the subjects its feeders observe, and nothing has run.
		// decisions/0039 Section 3 — and this is the line that used to write
		// `skipped` unconditionally, which was true only while nothing could
		// ever feed it.
		if !slot.Step.Source {
			out.Invocations = append(out.Invocations, planned)
			continue
		}

		decided, candidates, err := r.ask(ctx, workspace, target, fresh.ID,
			planned, slot.Step.Kind, slot.Intensity, slot.Values, now)
		if err != nil {
			return Planned{}, err
		}
		out.Invocations = append(out.Invocations, decided)
		out.Candidates = append(out.Candidates, candidates...)
	}
	return out, nil
}

// ask puts every candidate to the SPAWN GATE and settles the invocation from
// their answers — decisions/0039 Section 2. It is one function because plan time
// and execution time ask the identical question of a source step and a
// downstream one, and two copies of a gate walk drift.
//
//	any permitted    PENDING, citing the rule that permitted the first
//	all refused      REFUSED, citing that refusal
//	none at all      SKIPPED, naming what did not arrive
//
// **The invocation cites ONE rule and the candidates carry the rest.** `0010`'s
// three surfaces cite a rule id and an invocation has one column for it; a
// summary id merging several would be a citation to something nobody wrote. That
// is lossy for attribution when two rules permit different halves of one step —
// 0036 Section 5 reads `permit_rule` to attribute a discovered fragment — and it
// is accepted: any permitting rule is a true answer to "did this engagement's
// scope allow this run", which is the question being asked.
func (r *Runs) ask(ctx context.Context, workspace, target, run id.ID,
	planned domain.Invocation, kind, intensity string, values []string,
	now time.Time) (domain.Invocation, []domain.Candidate, error) {
	if len(values) == 0 {
		skipped, err := planned.Skip(domain.SkipNothingUpstream, now)
		return skipped, nil, err
	}

	candidates := make([]domain.Candidate, 0, len(values))
	permit := id.ID{}
	for _, value := range values {
		candidate, err := domain.NewCandidate(r.ids.NewID(), workspace, run,
			planned.ID, kind, value, now)
		if err != nil {
			// A kind the vocabulary has no word for, or an empty value. Both
			// are the planner's bug rather than the gate's answer, and turning
			// one into a silent refusal would hide it.
			return planned, nil, err
		}
		gate, err := r.spawns.MaySpawn(ctx, workspace, target, kind, value, intensity)
		if err != nil {
			return planned, nil, err
		}
		switch {
		case gate.Permitted:
			candidate = candidate.Permit()
			if permit.IsZero() {
				permit = gate.Rule
			}
		case gate.Rule.IsZero():
			candidate, err = candidate.NotInScope(gate.Reason)
		default:
			candidate, err = candidate.Refuse(gate.Rule, gate.Reason)
		}
		if err != nil {
			return planned, nil, err
		}
		candidates = append(candidates, candidate)
	}

	if len(domain.PermittedValues(candidates)) > 0 {
		// Left PENDING — this is the only path to a process — but the RULE that
		// permitted it is recorded, because PRODUCT.md's lineage walks back to
		// "the scope rule that allowed the command to run" and nothing else
		// records it.
		return planned.Permit(permit), candidates, nil
	}

	// EVERY candidate refused, so nothing spawns. The invocation carries the
	// first refusal so the run reads without opening the candidate rows, and
	// the rows carry each one so the scope proof is complete.
	first, _ := domain.FirstRefusal(candidates)
	var (
		refused domain.Invocation
		err     error
	)
	if first.RefusalRule.IsZero() {
		refused, err = planned.NotInScope(first.RefusalReason, now)
	} else {
		refused, err = planned.Refuse(first.RefusalRule, first.RefusalReason, now)
	}
	return refused, candidates, err
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
