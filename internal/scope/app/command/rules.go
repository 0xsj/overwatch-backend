package command

import (
	"context"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/scope/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// Repository takes a workspace on every method, for the reason target's does:
// a rule read without one is another engagement's, and a signature that cannot
// express the mistake is worth more than a rule about remembering.
type Repository interface {
	Create(ctx context.Context, r domain.Rule) error
	ByID(ctx context.Context, workspace, want id.ID) (domain.Rule, error)
	Supersede(ctx context.Context, r domain.Rule) error
}

type Minter interface{ NewID() id.ID }

type Clock interface{ Now() time.Time }

// Rules writes a target's scope. **There is no Edit** — decisions/0030.
type Rules struct {
	repo      Repository
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewRules(repo Repository, publisher events.Publisher, ids Minter, clock Clock) *Rules {
	if repo == nil || publisher == nil || ids == nil || clock == nil {
		panic("scope: NewRules with a nil dependency")
	}
	return &Rules{repo: repo, publisher: publisher, ids: ids, clock: clock}
}

// Draft is what a caller asks for. Kinds and Tools are already parsed, so the
// transport owns the vocabulary and this owns the rules about it.
type Draft struct {
	Pattern  string
	Polarity domain.Polarity
	Gate     domain.Gate
	Kinds    []domain.Kind
	Tools    []domain.Intensity
}

func (r *Rules) Add(ctx context.Context, workspace, target, by id.ID, in Draft) (domain.Rule, error) {
	fresh, err := domain.NewRule(r.ids.NewID(), workspace, target, by,
		in.Pattern, in.Polarity, in.Gate, in.Kinds, in.Tools, r.clock.Now())
	if err != nil {
		return domain.Rule{}, err
	}
	if err := r.repo.Create(ctx, fresh); err != nil {
		return domain.Rule{}, err
	}
	return fresh, r.emit(ctx, domain.EventRuleAdded, fresh, domain.RuleAdded{
		RuleID: fresh.ID.String(), TargetID: target.String(),
		WorkspaceID: workspace.String(), Pattern: fresh.Pattern,
		Polarity: fresh.Polarity.String(), Gate: fresh.Gate.String(),
	})
}

// Supersede retires a rule and keeps its id. Changing a rule is this plus an
// Add — two rows, one of which explains the other, which is what an audit trail
// of scope edits wants anyway.
func (r *Rules) Supersede(ctx context.Context, workspace, want, by id.ID) (domain.Rule, error) {
	held, err := r.repo.ByID(ctx, workspace, want)
	if err != nil {
		return domain.Rule{}, err
	}
	gone, err := held.Supersede(by, r.clock.Now())
	if err != nil {
		return domain.Rule{}, err
	}
	if err := r.repo.Supersede(ctx, gone); err != nil {
		return domain.Rule{}, err
	}
	return gone, r.emit(ctx, domain.EventRuleSuperseded, gone, domain.RuleSuperseded{
		RuleID: gone.ID.String(), TargetID: gone.TargetID.String(),
		WorkspaceID: workspace.String(), Pattern: gone.Pattern,
	})
}

// emit tenants to the workspace, so a scope edit lands on that ENGAGEMENT's
// audit log — which is where "who widened the scope" is asked, and it is asked
// after an engagement rather than during it.
func (r *Rules) emit(ctx context.Context, name string, rule domain.Rule, payload any) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, r.ids)
	}
	tenanted, err := prov.WithTenant(rule.WorkspaceID.String())
	if err != nil {
		return fmt.Errorf("scope: %s: %w", name, err)
	}
	e, err := events.NewDecision(r.ids, r.clock, name,
		domain.SubjectKind+":"+rule.TargetID.String(), tenanted, payload)
	if err != nil {
		return fmt.Errorf("scope: %s: %w", name, err)
	}
	if err := r.publisher.Publish(ctx, e); err != nil {
		return fmt.Errorf("scope: %s: %w", name, err)
	}
	return nil
}
