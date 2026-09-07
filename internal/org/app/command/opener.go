package command

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// WorkspaceOpened is the event this package listens for. The name is a string
// here rather than an import of workspace's constant, because org and workspace
// are peers and the checks refuse the import — the coupling is a contract on a
// name, which is what it would be over a broker too.
const WorkspaceOpened = "workspace.opened"

type workspaceOpened struct {
	WorkspaceID string `json:"workspace_id"`
	OrgID       string `json:"org_id"`
	OpenedBy    string `json:"opened_by"`
}

// Granter gives whoever opened a workspace `admin` on it — decisions/0020.
//
// **Without it an admin opens an engagement they cannot see.** An admin is not
// exempt (decisions/0019 made sure of that), so a workspace they created with no
// grant is invisible to them; the org owner never notices because the exemption
// covers them before this runs.
//
// It is a subscriber rather than a step inside workspace's Open, because
// workspace may not write org's tables. That is the same reason registration is
// a chain.
type Granter struct {
	repo  GrantRepository
	ids   Minter
	clock Clock
}

// GrantRepository is the narrowest write port for this: one insert, and a read
// that only exists so a redelivery is a no-op.
type GrantRepository interface {
	AddGrant(ctx context.Context, g domain.Grant) error
}

func NewGranter(repo GrantRepository, ids Minter, clock Clock) *Granter {
	if repo == nil || ids == nil || clock == nil {
		panic("org: NewGranter with a nil dependency")
	}
	return &Granter{repo: repo, ids: ids, clock: clock}
}

// Handle writes the opener's admin grant.
//
// **The grant is written even when the opener is the org owner**, who does not
// need it — decisions/0020. The row is redundant while they hold that role and
// becomes load-bearing the moment they do not: whoever started an engagement
// keeps admin on it independently of their org role, which is what a firm wants
// when somebody is demoted. Branching on the role here would silently make a
// demotion revoke access to work they own.
func (g *Granter) Handle(ctx context.Context, e events.Event) error {
	if e.Name != WorkspaceOpened {
		return nil
	}
	var payload workspaceOpened
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return fmt.Errorf("org: decode %s: %w", e.Name, err)
	}
	org, err := id.Parse(payload.OrgID)
	if err != nil {
		return fmt.Errorf("org: %s names no org: %w", e.Name, err)
	}
	workspace, err := id.Parse(payload.WorkspaceID)
	if err != nil {
		return fmt.Errorf("org: %s names no workspace: %w", e.Name, err)
	}
	opener, err := id.Parse(payload.OpenedBy)
	if err != nil {
		return fmt.Errorf("org: %s names no opener: %w", e.Name, err)
	}

	grant, err := domain.NewGrant(g.ids.NewID(), org, opener, workspace,
		domain.LevelAdmin, g.clock.Now())
	if err != nil {
		return fmt.Errorf("org: grant opener: %w", err)
	}
	if err := g.repo.AddGrant(ctx, grant); err != nil {
		// Delivery is at-least-once — decisions/0007. The unique index on
		// (org, account, workspace) is what makes the second delivery a no-op,
		// which is cheaper and more honest than a preflight read that races.
		if errors.Is(err, domain.ErrGrantExists) {
			return nil
		}
		return fmt.Errorf("org: grant opener: %w", err)
	}
	return nil
}
