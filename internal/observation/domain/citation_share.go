package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	EventCitationShareCreated  = "observation.citation.share.created"
	EventCitationShareRevoked  = "observation.citation.share.revoked"
	EventCitationShareAccessed = "observation.citation.share.accessed"
)

// CitationShare is a revocable handle for one immutable citation context. The
// raw token is returned only when created; storage keeps its digest instead.
type CitationShare struct {
	ID            id.ID      `json:"share_id"`
	WorkspaceID   id.ID      `json:"workspace_id"`
	SourceID      id.ID      `json:"source_id"`
	ObservationID id.ID      `json:"observation_id"`
	CreatedBy     id.ID      `json:"created_by"`
	TokenDigest   string     `json:"-"`
	CreatedAt     time.Time  `json:"created_at"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	RevokedBy     *id.ID     `json:"revoked_by,omitempty"`
}

func NewCitationShare(want, workspace, source, observation, createdBy id.ID, tokenDigest string, at time.Time) (CitationShare, error) {
	if want.IsZero() || workspace.IsZero() || source.IsZero() || observation.IsZero() || createdBy.IsZero() {
		return CitationShare{}, ErrIDRequired
	}
	if strings.TrimSpace(tokenDigest) == "" {
		return CitationShare{}, ErrShareTokenRequired
	}
	if at.IsZero() {
		return CitationShare{}, ErrTimeRequired
	}
	return CitationShare{ID: want, WorkspaceID: workspace, SourceID: source, ObservationID: observation, CreatedBy: createdBy, TokenDigest: tokenDigest, CreatedAt: at}, nil
}

func (s CitationShare) Revoked() bool { return s.RevokedAt != nil }

type CitationShareCreated struct {
	WorkspaceID   string `json:"workspace_id"`
	SourceID      string `json:"source_id"`
	ObservationID string `json:"observation_id"`
	ShareID       string `json:"share_id"`
	CreatedBy     string `json:"created_by"`
}

type CitationShareRevoked struct {
	WorkspaceID   string `json:"workspace_id"`
	SourceID      string `json:"source_id"`
	ObservationID string `json:"observation_id"`
	ShareID       string `json:"share_id"`
	RevokedBy     string `json:"revoked_by"`
}

type CitationShareAccessed struct {
	WorkspaceID   string `json:"workspace_id"`
	SourceID      string `json:"source_id"`
	ObservationID string `json:"observation_id"`
	ShareID       string `json:"share_id"`
	AccessMode    string `json:"access_mode"`
}
