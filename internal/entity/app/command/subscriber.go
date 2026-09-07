package command

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/entity/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// Assembler is the two subscribers that make the graph exist — decisions/0036.
//
//	target.added                 -> the ROOT ENTITY
//	extract.observation.created  -> fragments, and attributions for what is covered
type Assembler struct {
	repo      Repository
	subjects  Subjects
	runs      Runs
	claims    Claims
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewAssembler(repo Repository, subjects Subjects, runs Runs, claims Claims,
	publisher events.Publisher, ids Minter, clock Clock) *Assembler {
	if repo == nil || subjects == nil || runs == nil || claims == nil ||
		publisher == nil || ids == nil || clock == nil {
		panic("entity: NewAssembler with a nil dependency")
	}
	return &Assembler{repo: repo, subjects: subjects, runs: runs, claims: claims,
		publisher: publisher, ids: ids, clock: clock}
}

// RootFor creates a target's root entity — decisions/0029's column finally has
// something to point at.
//
// **Idempotent on redelivery.** The outbox delivers at least once and
// `entity_one_root_per_target` refuses a second, so this reads back what exists
// rather than treating the conflict as a failure — and it still emits, because
// the SECOND subscriber (target, filling in its column) may be the one that
// failed the first time round.
func (a *Assembler) RootFor(ctx context.Context, workspace, target id.ID, kind, label string) error {
	entityKind := rootKindOf(kind)
	fresh, err := domain.NewEntity(a.ids.NewID(), workspace, entityKind, label, a.clock.Now())
	if err != nil {
		return err
	}
	fresh = fresh.Root(target)
	if err := a.repo.CreateEntity(ctx, fresh); err != nil {
		return err
	}
	held, err := a.repo.RootFor(ctx, target)
	if err != nil {
		return err
	}
	return a.emit(ctx, domain.EventRootCreated, workspace, domain.RootCreated{
		EntityID: held.ID.String(), WorkspaceID: workspace.String(),
		TargetID: target.String(), Kind: held.Kind, Label: held.Label,
	})
}

// rootKindOf maps a target's kind onto an entity's. `target.Kind` has exactly
// two values and both are entity kinds in 0034's vocabulary, so this is a
// translation with nowhere to go wrong — and it is written out rather than
// passed through, because the two vocabularies are not the same one and a future
// target kind must land here as a compile-time question.
func rootKindOf(target string) string {
	if target == "person" {
		return "person"
	}
	return "org"
}

// Assembled is what one delivery did, and it is what the event carries.
type Assembled struct {
	Fragments  int
	New        int
	Attributed int
}

// Observed is the second subscriber: fragments from what the tools said, and an
// attribution for everything the engagement covers.
//
// **A fragment with no permitting rule is still created.** It was observed; that
// is true whether or not the engagement covers it. It simply is not an asset,
// which is 0009's predicate doing its job rather than a missing row.
func (a *Assembler) Observed(ctx context.Context, workspace, invocation id.ID) (Assembled, error) {
	subjects, err := a.subjects.ForInvocation(ctx, workspace, invocation)
	if err != nil {
		return Assembled{}, err
	}
	if len(subjects) == 0 {
		return Assembled{}, nil
	}

	// The join `0036` warns about: get this wrong and every row is well-formed,
	// the asset appears under the wrong client, and nothing in the schema can
	// tell.
	target, permittedBy, err := a.runs.TargetOf(ctx, workspace, invocation)
	if err != nil {
		return Assembled{}, err
	}
	root, err := a.repo.RootFor(ctx, target)
	if err != nil {
		return Assembled{}, err
	}

	now := a.clock.Now()
	out := Assembled{}
	for _, subject := range subjects {
		fresh, err := domain.NewFragment(a.ids.NewID(), workspace,
			subject.Kind, subject.Value, domain.Observed, subject.LastSeen)
		if err != nil {
			// A subject this package cannot represent — an over-long value —
			// is skipped rather than failing the delivery. The observation
			// still exists and still says what it said.
			continue
		}
		fresh.Observations = subject.Count
		stored, inserted, err := a.repo.Upsert(ctx, fresh)
		if err != nil {
			return Assembled{}, err
		}
		out.Fragments++
		if inserted {
			out.New++
		}

		claim, err := a.claims.MayClaim(ctx, workspace, target,
			stored.Kind, stored.Value, permittedBy)
		if err != nil {
			return Assembled{}, err
		}
		if !claim.Covered {
			continue
		}
		// CLAIMANT `rule`, born ACCEPTED, no decider — 0036 §3. The rule that
		// matched is the claimant ref, which is where "on what basis" points.
		attributed, err := domain.Propose(a.ids.NewID(), workspace, root.ID, stored.ID,
			domain.ByRule, claim.Rule, 0, false, claim.Basis, now)
		if err != nil {
			return Assembled{}, err
		}
		if err := a.repo.Attribute(ctx, attributed); err != nil {
			return Assembled{}, err
		}
		out.Attributed++
	}

	return out, a.emit(ctx, domain.EventFragmentSeen, workspace, domain.FragmentSeen{
		WorkspaceID: workspace.String(), Fragments: out.Fragments,
		New: out.New, Attributed: out.Attributed,
	})
}

// emit publishes WORK — decisions/0014. Assembling the graph has an outcome and
// nobody chose anything; a rule matched.
func (a *Assembler) emit(ctx context.Context, name string, workspace id.ID, payload any) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		// A subscriber running with no inherited provenance is a REPLAY — the
		// delivery arrived without the chain that caused it, which is exactly
		// what redelivery after a restart looks like. There is no
		// `OriginSubscriber`, deliberately: the origin says what STARTED the
		// chain, and a subscriber never starts one.
		prov = provenance.New(provenance.OriginReplay, a.ids)
	}
	tenanted, err := prov.WithTenant(workspace.String())
	if err != nil {
		return fmt.Errorf("entity: %s: %w", name, err)
	}
	e, err := events.New(a.ids, a.clock, name,
		domain.SubjectKind+":"+workspace.String(), tenanted, payload)
	if err != nil {
		return fmt.Errorf("entity: %s: %w", name, err)
	}
	if err := a.publisher.Publish(ctx, e); err != nil {
		return fmt.Errorf("entity: %s: %w", name, err)
	}
	return nil
}
