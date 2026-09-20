package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

// HandoffShare is a revocable handle for a frozen recipient handoff. The raw
// token is intentionally not part of the domain object; only its digest is
// persisted and the raw value is returned once by the create command.
type HandoffShare struct {
	ID          id.ID      `json:"share_id"`
	WorkspaceID id.ID      `json:"workspace_id"`
	SnapshotID  id.ID      `json:"snapshot_id"`
	CreatedBy   id.ID      `json:"created_by"`
	TokenDigest string     `json:"-"`
	CreatedAt   time.Time  `json:"created_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	RevokedBy   *id.ID     `json:"revoked_by,omitempty"`
}

func NewHandoffShare(want, workspace, snapshot, createdBy id.ID, tokenDigest string, at time.Time) (HandoffShare, error) {
	if want.IsZero() || workspace.IsZero() || snapshot.IsZero() || createdBy.IsZero() {
		return HandoffShare{}, ErrIDRequired
	}
	if strings.TrimSpace(tokenDigest) == "" {
		return HandoffShare{}, ErrShareTokenRequired
	}
	if at.IsZero() {
		return HandoffShare{}, ErrTimeRequired
	}
	return HandoffShare{ID: want, WorkspaceID: workspace, SnapshotID: snapshot, CreatedBy: createdBy, TokenDigest: tokenDigest, CreatedAt: at}, nil
}

func (s HandoffShare) Revoked() bool { return s.RevokedAt != nil }
