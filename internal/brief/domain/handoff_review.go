package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const MaxReviewNoteLength = 4000

// ReviewState describes the current disposition of a frozen handoff. It is
// deliberately separate from Snapshot: a review can change without changing
// the immutable material that was handed off.
type ReviewState string

const (
	ReviewPending          ReviewState = "pending"
	ReviewApproved         ReviewState = "approved"
	ReviewChangesRequested ReviewState = "changes_requested"
)

type ReviewDecision struct {
	ID          id.ID       `json:"decision_id"`
	WorkspaceID id.ID       `json:"workspace_id"`
	SnapshotID  id.ID       `json:"snapshot_id"`
	ReviewerID  id.ID       `json:"reviewer_id"`
	State       ReviewState `json:"state"`
	Note        string      `json:"note,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
}

type SnapshotReview struct {
	WorkspaceID id.ID            `json:"workspace_id"`
	SnapshotID  id.ID            `json:"snapshot_id"`
	State       ReviewState      `json:"state"`
	AssigneeID  *id.ID           `json:"assignee_id,omitempty"`
	AssignedBy  *id.ID           `json:"assigned_by,omitempty"`
	AssignedAt  *time.Time       `json:"assigned_at,omitempty"`
	UpdatedAt   time.Time        `json:"updated_at"`
	Decisions   []ReviewDecision `json:"decisions"`
}

func NewReviewDecision(want, workspace, snapshot, reviewer id.ID, state, note string, at time.Time) (ReviewDecision, error) {
	if want.IsZero() || workspace.IsZero() || snapshot.IsZero() || reviewer.IsZero() {
		return ReviewDecision{}, ErrIDRequired
	}
	if at.IsZero() {
		return ReviewDecision{}, ErrTimeRequired
	}
	parsed, err := ParseReviewState(state)
	if err != nil || parsed == ReviewPending {
		return ReviewDecision{}, ErrReviewStateInvalid
	}
	note = strings.TrimSpace(note)
	if parsed == ReviewChangesRequested && note == "" {
		return ReviewDecision{}, ErrReviewNoteRequired
	}
	if !utf8.ValidString(note) || strings.ContainsRune(note, 0) || len(note) > MaxReviewNoteLength {
		return ReviewDecision{}, ErrReviewNoteTooLong
	}
	return ReviewDecision{ID: want, WorkspaceID: workspace, SnapshotID: snapshot, ReviewerID: reviewer, State: parsed, Note: note, CreatedAt: at}, nil
}

func ParseReviewState(value string) (ReviewState, error) {
	switch ReviewState(strings.TrimSpace(value)) {
	case ReviewPending:
		return ReviewPending, nil
	case ReviewApproved:
		return ReviewApproved, nil
	case ReviewChangesRequested:
		return ReviewChangesRequested, nil
	default:
		return "", ErrReviewStateInvalid
	}
}
