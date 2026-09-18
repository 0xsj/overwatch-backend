package command

import (
	"context"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/entity/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// Assembler is the two subscribers that make the graph exist — decisions/0036.
//
//	target.added                 -> the ROOT ENTITY
//	extract.observation.created  -> fragments, attributions for what is covered,
//	                                and DERIVATIONS for what a record was read
//	                                out of — 0003's second edge kind, 0040
type Assembler struct {
	repo        Repository
	subjects    Subjects
	provenances Provenances
	runs        Runs
	claims      Claims
	publisher   events.Publisher
	ids         Minter
	clock       Clock
}

func NewAssembler(repo Repository, subjects Subjects, provenances Provenances,
	runs Runs, claims Claims, publisher events.Publisher, ids Minter, clock Clock) *Assembler {
	if repo == nil || subjects == nil || provenances == nil || runs == nil ||
		claims == nil || publisher == nil || ids == nil || clock == nil {
		panic("entity: NewAssembler with a nil dependency")
	}
	return &Assembler{repo: repo, subjects: subjects, provenances: provenances,
		runs: runs, claims: claims, publisher: publisher, ids: ids, clock: clock}
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

	// Derivations and Unresolved are `0003`'s second edge kind and the
	// provenances nothing could be found for — 0040. They are counted apart
	// because "we drew 35 edges" and "and 2 we could not" are two facts.
	Derivations int
	Unresolved  int
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

	// THE EDGES, after the fragments — a derivation names two of them, and both
	// have to exist before one can point at them. Same delivery, same
	// transaction as the caller's, so a crash leaves neither rather than a
	// graph half-drawn.
	drawn, unresolved, err := a.derive(ctx, workspace, invocation, now)
	if err != nil {
		return Assembled{}, err
	}
	out.Derivations = drawn
	out.Unresolved = unresolved

	if err := a.emit(ctx, domain.EventFragmentSeen, workspace, domain.FragmentSeen{
		WorkspaceID: workspace.String(), Fragments: out.Fragments,
		New: out.New, Attributed: out.Attributed,
	}); err != nil {
		return Assembled{}, err
	}
	if drawn == 0 && unresolved == 0 {
		// Most tools declare no provenance mapping. A silent event saying
		// nothing happened would be noise on every extraction in the system.
		return out, nil
	}
	// A SEPARATE event, for the same reason `field.unmapped` is separate from
	// `observations.created`: "37 cited, 2 did not resolve" is the sentence
	// somebody wants, and burying it inside a fragment count is how it goes
	// unnoticed.
	return out, a.emit(ctx, domain.EventDerivationDrawn, workspace, domain.Drawn{
		WorkspaceID: workspace.String(), InvocationID: invocation.String(),
		Drawn: drawn, Unresolved: unresolved,
	})
}

// derive turns this delivery's provenance readings into `0003`'s second edge
// kind — decisions/0040.
//
// **Both fragments must already exist.** The `to` side does by construction:
// it is a subject of this same delivery, which the loop above just upserted.
// The `from` side is looked up, and a miss is RECORDED rather than dropped or
// invented — 0040 §5. The two ordinary reasons are a candidate the scope gate
// refused (0039) and a fold mismatch, and both are worth a countable row.
func (a *Assembler) derive(ctx context.Context, workspace, invocation id.ID,
	now time.Time) (drawn, unresolved int, err error) {
	found, err := a.provenances.ForInvocation(ctx, workspace, invocation)
	if err != nil {
		return 0, 0, err
	}
	for _, p := range found {
		to, ok, err := a.repo.FragmentFor(ctx, workspace, p.SubjectKind, p.SubjectValue)
		if err != nil {
			return 0, 0, err
		}
		if !ok {
			// The thing the edge would point AT is missing. That is not the
			// case 0040 §5 is about — it means the subject itself was skipped
			// above, which only happens for a value this package cannot
			// represent. There is nothing to hang an unresolved row off.
			continue
		}
		from, ok, err := a.repo.FragmentFor(ctx, workspace, p.FromKind, p.FromValue)
		if err != nil {
			return 0, 0, err
		}
		if !ok {
			missed, err := domain.NewUnresolved(a.ids.NewID(), workspace, invocation,
				p.MappingID, to.ID, p.FromKind, p.FromValue, p.Label, now)
			if err != nil {
				continue
			}
			if err := a.repo.RecordUnresolved(ctx, missed); err != nil {
				return 0, 0, err
			}
			unresolved++
			continue
		}
		edge, err := domain.NewDerivation(a.ids.NewID(), workspace, from.ID, to.ID,
			p.Label, invocation, p.ArtifactID, p.MappingID, now)
		if err != nil {
			// A self-edge, or a label this package cannot hold. Skipped rather
			// than fatal: the rest of the delivery is still true, and a mapping
			// pointed at its own subject would otherwise fail every extraction.
			continue
		}
		if err := a.repo.Draw(ctx, edge); err != nil {
			return 0, 0, err
		}
		drawn++
	}
	return drawn, unresolved, nil
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
