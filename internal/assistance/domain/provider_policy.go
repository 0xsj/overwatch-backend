package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const EventProviderPolicyUpdated = "assistance.provider_policy.updated"

// ProviderPolicy is workspace-scoped and defaults to denying external
// providers. Local providers do not require an opt-in because retained
// material never leaves the service boundary.
type ProviderPolicy struct {
	WorkspaceID   id.ID     `json:"workspace_id"`
	AllowExternal bool      `json:"allow_external"`
	UpdatedBy     id.ID     `json:"updated_by"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func DefaultProviderPolicy(workspace id.ID) ProviderPolicy {
	return ProviderPolicy{WorkspaceID: workspace}
}

func NewProviderPolicy(workspace, actor id.ID, allowExternal bool, at time.Time) (ProviderPolicy, error) {
	if workspace.IsZero() || actor.IsZero() {
		return ProviderPolicy{}, ErrIDRequired
	}
	if at.IsZero() {
		return ProviderPolicy{}, ErrTimeRequired
	}
	return ProviderPolicy{WorkspaceID: workspace, AllowExternal: allowExternal, UpdatedBy: actor, UpdatedAt: at}, nil
}

func (p ProviderPolicy) Validate() error {
	if p.WorkspaceID.IsZero() {
		return ErrWorkspaceRequired
	}
	if p.UpdatedBy.IsZero() || p.UpdatedAt.IsZero() {
		return ErrIDRequired
	}
	return nil
}

func (p ProviderPolicy) Description() string {
	if p.AllowExternal {
		return "External assistance providers are allowed for this workspace."
	}
	return strings.TrimSpace("External assistance providers are disabled; local assistance remains available.")
}
