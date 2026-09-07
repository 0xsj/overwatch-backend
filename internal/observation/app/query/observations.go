package query

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/observation/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	ByID(ctx context.Context, workspace, want id.ID) (domain.Observation, error)
	ForInvocation(ctx context.Context, workspace, invocation id.ID, limit int) ([]domain.Observation, error)
	ForSubject(ctx context.Context, workspace id.ID, kind, value string, limit int) ([]domain.Observation, error)
	Subjects(ctx context.Context, workspace id.ID, limit int) ([]domain.Subject, error)
	UnmappedFor(ctx context.Context, workspace, invocation id.ID) ([]domain.Unmapped, error)
	Quality(ctx context.Context, workspace, invocation id.ID) (domain.Quality, error)
	SubjectsPerInvocation(ctx context.Context, workspace id.ID) ([]domain.SubjectSeen, error)
}

const (
	DefaultPage = 200
	MaxPage     = 1000
)

type Observations struct {
	reader   Reader
	mappings Mappings
	runs     Runs
	rules    Rules
	orgs     Orgs
}

func NewObservations(reader Reader, mappings Mappings, runs Runs, rules Rules, orgs Orgs) *Observations {
	if reader == nil || mappings == nil || runs == nil || rules == nil || orgs == nil {
		panic("observation: NewObservations with a nil dependency")
	}
	return &Observations{reader: reader, mappings: mappings, runs: runs, rules: rules, orgs: orgs}
}

func page(limit int) int {
	if limit <= 0 {
		return DefaultPage
	}
	if limit > MaxPage {
		return MaxPage
	}
	return limit
}

func (o *Observations) ForInvocation(ctx context.Context, workspace, invocation id.ID, limit int) ([]domain.Observation, error) {
	if workspace.IsZero() || invocation.IsZero() {
		return nil, domain.ErrIDRequired
	}
	return o.reader.ForInvocation(ctx, workspace, invocation, page(limit))
}

// ForSubject is the asset drawer's read: everything known about one subject,
// NOT deduplicated. Two runs a day apart are two statements and collapsing them
// loses the second date — "the state of each field" is the first row per field,
// which the ordering already gives the caller.
func (o *Observations) ForSubject(ctx context.Context, workspace id.ID, kind, value string, limit int) ([]domain.Observation, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	if kind == "" || value == "" {
		return nil, domain.ErrSubjectRequired
	}
	return o.reader.ForSubject(ctx, workspace, kind, value, page(limit))
}

func (o *Observations) Subjects(ctx context.Context, workspace id.ID, limit int) ([]domain.Subject, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	return o.reader.Subjects(ctx, workspace, page(limit))
}

func (o *Observations) Unmapped(ctx context.Context, workspace, invocation id.ID) ([]domain.Unmapped, error) {
	if workspace.IsZero() || invocation.IsZero() {
		return nil, domain.ErrIDRequired
	}
	return o.reader.UnmappedFor(ctx, workspace, invocation)
}

func (o *Observations) Quality(ctx context.Context, workspace, invocation id.ID) (domain.Quality, error) {
	if workspace.IsZero() || invocation.IsZero() {
		return domain.Quality{}, domain.ErrIDRequired
	}
	return o.reader.Quality(ctx, workspace, invocation)
}

// Lineage walks one value backwards to the bytes it was read out of.
//
// **It reads the observation FIRST, and that is the tenancy check.** Every
// subsequent step is reached through ids the observation itself carries, so a
// caller cannot assemble a lineage for a row they cannot see.
//
// A step that is genuinely absent comes back nil rather than as an error: an
// invocation that nothing refused has no rule to cite, and rendering that as a
// failure would make the commonest case look broken.
func (o *Observations) Lineage(ctx context.Context, workspace, want id.ID) (Lineage, error) {
	found, err := o.reader.ByID(ctx, workspace, want)
	if err != nil {
		return Lineage{}, err
	}
	out := Lineage{Observation: found}

	org, err := o.orgs.OrgOf(ctx, workspace)
	if err != nil {
		return Lineage{}, err
	}
	if out.Mapping, err = o.mappings.Step(ctx, org, found.MappingID); err != nil {
		return Lineage{}, fmt.Errorf("observation: lineage mapping: %w", err)
	}

	invocation, artifact, rule, err := o.runs.Steps(ctx, workspace, found.InvocationID, found.ArtifactID)
	if err != nil {
		return Lineage{}, fmt.Errorf("observation: lineage invocation: %w", err)
	}
	out.Invocation, out.Artifact = invocation, artifact

	if rule.IsZero() {
		return out, nil
	}
	if out.Rule, err = o.rules.Step(ctx, workspace, rule); err != nil {
		return Lineage{}, fmt.Errorf("observation: lineage rule: %w", err)
	}
	return out, nil
}

// SubjectsPerInvocation is coverage's second source. See domain.SubjectSeen.
func (o *Observations) SubjectsPerInvocation(ctx context.Context, workspace id.ID) ([]domain.SubjectSeen, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	return o.reader.SubjectsPerInvocation(ctx, workspace)
}
