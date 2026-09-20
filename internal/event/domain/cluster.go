package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	MaxEventClusterTitle  = 200
	MaxEventClusterDetail = 4000
	MaxEventClusterEvents = 12
)

type ClusterState string

const (
	ClusterProposed ClusterState = "proposed"
	ClusterAccepted ClusterState = "accepted"
	ClusterRejected ClusterState = "rejected"
)

type Cluster struct {
	ID          id.ID        `json:"cluster_id"`
	WorkspaceID id.ID        `json:"workspace_id"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	EventIDs    []id.ID      `json:"event_ids"`
	State       ClusterState `json:"state"`
	ReviewNote  string       `json:"review_note,omitempty"`
	Author      id.ID        `json:"author"`
	UpdatedBy   id.ID        `json:"updated_by"`
	ReviewedBy  *id.ID       `json:"reviewed_by,omitempty"`
	ReviewedAt  *time.Time   `json:"reviewed_at,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

func NewCluster(want, workspace, author id.ID, title, description string, events []id.ID, at time.Time) (Cluster, error) {
	if want.IsZero() || workspace.IsZero() || author.IsZero() || at.IsZero() {
		return Cluster{}, ErrIDRequired
	}
	title, description, events, err := validateCluster(title, description, events)
	if err != nil {
		return Cluster{}, err
	}
	return Cluster{ID: want, WorkspaceID: workspace, Title: title, Description: description, EventIDs: events, State: ClusterProposed, Author: author, UpdatedBy: author, CreatedAt: at, UpdatedAt: at}, nil
}

func (c Cluster) Edit(by id.ID, title, description string, events []id.ID, at time.Time) (Cluster, error) {
	if c.ID.IsZero() || c.WorkspaceID.IsZero() || by.IsZero() || at.IsZero() {
		return c, ErrIDRequired
	}
	title, description, events, err := validateCluster(title, description, events)
	if err != nil {
		return c, err
	}
	next := c
	next.Title, next.Description, next.EventIDs = title, description, events
	next.UpdatedBy, next.UpdatedAt = by, at
	return next, nil
}

func (c Cluster) Review(by id.ID, state ClusterState, note string, at time.Time) (Cluster, error) {
	if c.ID.IsZero() || c.WorkspaceID.IsZero() || by.IsZero() || at.IsZero() {
		return c, ErrIDRequired
	}
	if state != ClusterProposed && state != ClusterAccepted && state != ClusterRejected {
		return c, ErrClusterStateUnknown
	}
	note = strings.TrimSpace(note)
	if state != ClusterProposed && (note == "" || !valid(note, MaxEventClusterDetail)) {
		return c, ErrClusterReviewRequired
	}
	next := c
	next.State, next.ReviewNote, next.UpdatedBy, next.UpdatedAt = state, note, by, at
	next.ReviewedBy, next.ReviewedAt = nil, nil
	if state != ClusterProposed {
		reviewer, reviewedAt := by, at
		next.ReviewedBy, next.ReviewedAt = &reviewer, &reviewedAt
	}
	return next, nil
}

func validateCluster(title, description string, events []id.ID) (string, string, []id.ID, error) {
	title, description = strings.TrimSpace(title), strings.TrimSpace(description)
	if title == "" || len(title) > MaxEventClusterTitle || !utf8.ValidString(title) || strings.ContainsRune(title, 0) {
		return "", "", nil, ErrClusterTitleRequired
	}
	if !valid(description, MaxEventClusterDetail) {
		return "", "", nil, ErrClusterDescriptionTooLong
	}
	if len(events) < 2 || len(events) > MaxEventClusterEvents {
		return "", "", nil, ErrClusterEventCount
	}
	seen := make(map[id.ID]struct{}, len(events))
	ordered := make([]id.ID, 0, len(events))
	for _, event := range events {
		if event.IsZero() {
			return "", "", nil, ErrIDRequired
		}
		if _, ok := seen[event]; ok {
			return "", "", nil, ErrDuplicateClusterEvent
		}
		seen[event] = struct{}{}
		ordered = append(ordered, event)
	}
	return title, description, ordered, nil
}
