package domain

import (
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var (
	ErrIDRequired        = errors.New(errors.Invalid, "an account and engagement are required")
	ErrTimeRequired      = errors.New(errors.Invalid, "a seen instant is required")
	ErrNotFound          = errors.New(errors.NotFound, "seen marker")
	ErrWorkspaceRequired = errors.New(errors.Invalid, "a seen marker belongs to an engagement")
)

// Marker is the durable per-account watermark for a workspace's change view.
// It is not a claim about the workspace and it does not alter observations; it
// records only the last point at which this reader chose to mark the projection
// seen.
type Marker struct {
	AccountID   id.ID
	WorkspaceID id.ID
	SeenAt      time.Time
}

func New(account, workspace id.ID, at time.Time) (Marker, error) {
	if account.IsZero() || workspace.IsZero() {
		return Marker{}, ErrIDRequired
	}
	if at.IsZero() {
		return Marker{}, ErrTimeRequired
	}
	return Marker{AccountID: account, WorkspaceID: workspace, SeenAt: at}, nil
}
