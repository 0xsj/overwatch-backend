package command

import (
	"context"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/finding/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// Findings records what a scanner found, and what a person then decided about
// it. Those are the two halves of decisions/0041 and they are deliberately not
// symmetrical: the first is work with an outcome, the second is a DECISION with
// an author — `0014`.
type Findings struct {
	repo      Repository
	fragments Fragments
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewFindings(repo Repository, fragments Fragments, tx Transactor,
	publisher events.Publisher, ids Minter, clock Clock) *Findings {
	if repo == nil || fragments == nil || tx == nil || publisher == nil ||
		ids == nil || clock == nil {
		panic("finding: NewFindings with a nil dependency")
	}
	return &Findings{repo: repo, fragments: fragments, tx: tx,
		publisher: publisher, ids: ids, clock: clock}
}

// Sighting is one record a finding-producing tool emitted, in this package's own
// vocabulary. `observation` builds it and cannot see this package, so the
// composition root carries it across.
type Sighting struct {
	WorkspaceID  id.ID
	ToolID       id.ID
	InvocationID id.ID
	ArtifactID   id.ID

	Signature        string
	SignatureMapping id.ID

	// Severity is the tool's own word, unparsed. It is parsed HERE so an
	// unrecognised one names this package's rule rather than failing somewhere
	// with no vocabulary to explain it.
	Severity string

	// SubjectKind and SubjectValue are the fragment the finding is ON.
	SubjectKind  string
	SubjectValue string

	// Details are every other field the tool's mappings read.
	Details []Detail

	// SeenAt is when the TOOL RAN, not when extraction happened — the same rule
	// `observation` follows, so a re-extraction cannot make an old scan look
	// like tonight's.
	SeenAt time.Time
}

type Detail struct {
	Field   string
	Value   string
	Mapping id.ID
}

// Recorded is what one artifact's findings did.
type Recorded struct {
	Findings int
	Opened   int
	Skipped  int
}

// Record turns a finding-producing tool's records into findings.
//
// **A sighting whose fragment does not exist is SKIPPED and counted.** The
// `matched-at` value is normally a fragment the same extraction pass just made;
// when it is not — a scope rule kept the run off it, a fold mismatch — hanging
// the finding off a fragment invented here would put a problem on an asset
// nobody observed.
func (f *Findings) Record(ctx context.Context, in []Sighting) (Recorded, error) {
	out := Recorded{}
	if len(in) == 0 {
		return out, nil
	}
	now := f.clock.Now()

	var opened []domain.Finding
	err := f.tx.InTx(ctx, func(ctx context.Context) error {
		for _, one := range in {
			fragment, ok, err := f.fragments.ForValue(ctx, one.WorkspaceID,
				one.SubjectKind, one.SubjectValue)
			if err != nil {
				return err
			}
			if !ok {
				out.Skipped++
				continue
			}

			severity, err := domain.ParseSeverity(one.Severity)
			if err != nil {
				// An unrecognised severity is NOT filed as `info`. Quietly
				// downgrading an unknown word would hide the loudest thing a
				// scanner said, and `unknown` is not a level on 0004's scale.
				out.Skipped++
				continue
			}

			fresh, err := domain.New(f.ids.NewID(), one.WorkspaceID, one.ToolID,
				fragment, one.Signature, one.SubjectKind, one.SubjectValue,
				severity, "reported by "+one.Signature,
				one.InvocationID, one.ArtifactID, one.SignatureMapping,
				one.SeenAt, now)
			if err != nil {
				// A malformed sighting — no signature, an over-long one — is
				// skipped rather than failing the artifact. The others are
				// still what the scanner said.
				out.Skipped++
				continue
			}

			stored, isNew, err := f.repo.Record(ctx, fresh)
			if err != nil {
				return err
			}
			out.Findings++
			if isNew {
				out.Opened++
				opened = append(opened, stored)
			}

			for _, d := range one.Details {
				detail, err := domain.NewDetail(f.ids.NewID(), stored.ID,
					d.Field, d.Value, d.Mapping, one.ArtifactID, one.SeenAt)
				if err != nil {
					continue
				}
				if err := f.repo.SaveDetail(ctx, detail); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return Recorded{}, err
	}

	// ONE EVENT PER NEW PROBLEM, and none for a sighting. `finding.opened` is
	// news and a rescan is a heartbeat; a subscriber that wants to page
	// somebody wants only the first, and burying the distinction inside a count
	// is how an alert becomes noise.
	for _, one := range opened {
		if err := f.work(ctx, domain.EventFindingOpened, one.WorkspaceID, domain.Opened{
			WorkspaceID: one.WorkspaceID.String(), FindingID: one.ID.String(),
			Signature: one.Signature, Severity: one.Severity.String(),
			Fragment: one.FragmentValue,
		}); err != nil {
			return Recorded{}, err
		}
	}
	return out, nil
}

// Decide moves a finding's state. Every path here needs a person —
// decisions/0041: nothing closes a finding automatically, because absence of a
// match is not evidence of a fix.
func (f *Findings) Decide(ctx context.Context, workspace, want, by id.ID,
	to domain.State, reason string) (domain.Finding, error) {
	held, err := f.repo.ByID(ctx, workspace, want)
	if err != nil {
		return domain.Finding{}, err
	}
	now := f.clock.Now()

	var next domain.Finding
	switch to {
	case domain.StateTriaged:
		next, err = held.Triage(by, now)
	case domain.StateResolved:
		next, err = held.Resolve(by, reason, now)
	case domain.StateDismissed:
		next, err = held.Dismiss(by, reason, now)
	default:
		// There is no path back to `open`. A finding reopens only by being SEEN
		// again, which is a fact about the estate rather than somebody's
		// opinion of it.
		return domain.Finding{}, domain.ErrStateUnknown
	}
	if err != nil {
		return domain.Finding{}, err
	}
	if err := f.repo.Save(ctx, next); err != nil {
		return domain.Finding{}, err
	}
	return next, f.decide(ctx, domain.EventFindingDecided, workspace, domain.Decided{
		WorkspaceID: workspace.String(), FindingID: next.ID.String(),
		State: next.State.String(), ByPerson: true,
	})
}

// Reassess replaces a severity and keeps what it replaced — `0004`.
func (f *Findings) Reassess(ctx context.Context, workspace, want, by id.ID,
	to domain.Severity, basis string) (domain.Finding, error) {
	held, err := f.repo.ByID(ctx, workspace, want)
	if err != nil {
		return domain.Finding{}, err
	}
	next, err := held.Reassess(to, domain.Assessment{
		Claimant: domain.ByHuman, Actor: by, Basis: basis, At: f.clock.Now(),
	})
	if err != nil {
		return domain.Finding{}, err
	}
	if err := f.repo.Save(ctx, next); err != nil {
		return domain.Finding{}, err
	}
	return next, f.decide(ctx, domain.EventFindingReassess, workspace, domain.Decided{
		WorkspaceID: workspace.String(), FindingID: next.ID.String(),
		State: next.State.String(), ByPerson: true,
	})
}

// decide publishes a DECISION — 0014. A person ruled on a client's problem:
// that has an author, no outcome column, and a reason to outlive the journal's
// retention.
func (f *Findings) decide(ctx context.Context, name string, workspace id.ID, payload any) error {
	return f.publish(ctx, name, workspace, payload, true)
}

// work publishes a unit of work: a scanner found something, and it has an
// outcome.
func (f *Findings) work(ctx context.Context, name string, workspace id.ID, payload any) error {
	return f.publish(ctx, name, workspace, payload, false)
}

func (f *Findings) publish(ctx context.Context, name string, workspace id.ID,
	payload any, decision bool) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginReplay, f.ids)
	}
	tenanted, err := prov.WithTenant(workspace.String())
	if err != nil {
		return fmt.Errorf("finding: %s: %w", name, err)
	}
	subject := domain.SubjectKind + ":" + workspace.String()
	var e events.Event
	if decision {
		e, err = events.NewDecision(f.ids, f.clock, name, subject, tenanted, payload)
	} else {
		e, err = events.New(f.ids, f.clock, name, subject, tenanted, payload)
	}
	if err != nil {
		return fmt.Errorf("finding: %s: %w", name, err)
	}
	if err := f.publisher.Publish(ctx, e); err != nil {
		return fmt.Errorf("finding: %s: %w", name, err)
	}
	return nil
}
