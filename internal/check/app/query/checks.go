package query

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/check/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	ByID(ctx context.Context, org, want id.ID) (domain.Check, error)
	ForOrg(ctx context.Context, org id.ID, includeArchived bool) ([]domain.Check, error)
	Chain(ctx context.Context, check id.ID) (domain.Chain, error)
	UsingTool(ctx context.Context, org, tool id.ID) ([]domain.Usage, error)
	Schedulable(ctx context.Context) ([]domain.Check, error)
}

type Checks struct{ reader Reader }

func NewChecks(reader Reader) *Checks {
	if reader == nil {
		panic("check: NewChecks query with a nil reader")
	}
	return &Checks{reader: reader}
}

func (c *Checks) ByID(ctx context.Context, org, want id.ID) (domain.Check, error) {
	if org.IsZero() || want.IsZero() {
		return domain.Check{}, domain.ErrIDRequired
	}
	return c.reader.ByID(ctx, org, want)
}

func (c *Checks) ForOrg(ctx context.Context, org id.ID, includeArchived bool) ([]domain.Check, error) {
	if org.IsZero() {
		return nil, domain.ErrOrgRequired
	}
	found, err := c.reader.ForOrg(ctx, org, includeArchived)
	if err != nil {
		return nil, fmt.Errorf("check: checks: %w", err)
	}
	return found, nil
}

// Chain reads THROUGH the check, so the org is checked once and the chain read
// cannot be reached with an id alone.
func (c *Checks) Chain(ctx context.Context, org, want id.ID) (domain.Chain, error) {
	if _, err := c.ByID(ctx, org, want); err != nil {
		return domain.Chain{}, err
	}
	return c.reader.Chain(ctx, want)
}

// UsingTool answers what archiving a tool has to ask. It is the reason the chain
// is rows rather than a jsonb column — decisions/0032.
//
// An EMPTY result is "nothing runs this", which is what permits the archive. It
// is not the same as an error, and a caller that treats a failed read as an
// empty list has turned a refusal into an approval.
func (c *Checks) UsingTool(ctx context.Context, org, tool id.ID) ([]domain.Usage, error) {
	if org.IsZero() || tool.IsZero() {
		return nil, domain.ErrIDRequired
	}
	return c.reader.UsingTool(ctx, org, tool)
}

// Schedulable is every check a schedule could start, across ALL orgs —
// decisions/0038. It takes no org and checks none, which every other read here
// does: the caller is a background lifecycle rather than a request, and there is
// no caller to be tenanted to.
func (c *Checks) Schedulable(ctx context.Context) ([]domain.Check, error) {
	return c.reader.Schedulable(ctx)
}
