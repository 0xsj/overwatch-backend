package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	SourceLinkRecorded     = "review.source_link.recorded"
	MaxSourceLinkRationale = 4000
)

// SourceLink records a qualified analyst suspicion that one observation may
// derive from another. Downstream is the possibly derivative report;
// upstream is the possible earlier source. It is not a proven provenance edge.
type SourceLink struct {
	ID                      id.ID     `json:"source_link_id"`
	WorkspaceID             id.ID     `json:"workspace_id"`
	DownstreamObservationID id.ID     `json:"downstream_observation_id"`
	UpstreamObservationID   id.ID     `json:"upstream_observation_id"`
	Rationale               string    `json:"rationale"`
	Author                  id.ID     `json:"author"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type SourceLinkItem struct {
	SourceLink
	DownstreamSourceID    id.ID  `json:"downstream_source_id"`
	DownstreamSourceTitle string `json:"downstream_source_title"`
	DownstreamCaptureID   id.ID  `json:"downstream_capture_id"`
	DownstreamStatement   string `json:"downstream_statement"`
	UpstreamSourceID      id.ID  `json:"upstream_source_id"`
	UpstreamSourceTitle   string `json:"upstream_source_title"`
	UpstreamCaptureID     id.ID  `json:"upstream_capture_id"`
	UpstreamStatement     string `json:"upstream_statement"`
	CycleDetected         bool   `json:"cycle_detected"`
}

func NewSourceLink(want, workspace, author, downstream, upstream id.ID, rationale string, at time.Time) (SourceLink, error) {
	if want.IsZero() || workspace.IsZero() || author.IsZero() || downstream.IsZero() || upstream.IsZero() || downstream == upstream || at.IsZero() {
		return SourceLink{}, ErrInvalid
	}
	rationale = strings.TrimSpace(rationale)
	if rationale == "" || len(rationale) > MaxSourceLinkRationale || !utf8.ValidString(rationale) || strings.ContainsRune(rationale, 0) {
		return SourceLink{}, errors.New(errors.Invalid, "source-link rationale requires UTF-8 text up to 4000 bytes")
	}
	return SourceLink{ID: want, WorkspaceID: workspace, DownstreamObservationID: downstream, UpstreamObservationID: upstream, Rationale: rationale, Author: author, CreatedAt: at, UpdatedAt: at}, nil
}

func (s SourceLink) Edit(by id.ID, rationale string, at time.Time) (SourceLink, error) {
	if s.ID.IsZero() || s.WorkspaceID.IsZero() || by.IsZero() || at.IsZero() {
		return s, ErrInvalid
	}
	rationale = strings.TrimSpace(rationale)
	if rationale == "" || len(rationale) > MaxSourceLinkRationale || !utf8.ValidString(rationale) || strings.ContainsRune(rationale, 0) {
		return s, errors.New(errors.Invalid, "source-link rationale requires UTF-8 text up to 4000 bytes")
	}
	next := s
	next.Rationale, next.Author, next.UpdatedAt = rationale, by, at
	return next, nil
}
