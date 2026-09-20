package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const MaxCommentBodyLength = 4000

// SnapshotComment is collaboration material attached to a frozen handoff.
// It deliberately lives outside Snapshot and SnapshotReview: comments may be
// added after delivery without changing the authored handoff or its decision
// history.
type SnapshotComment struct {
	ID          id.ID     `json:"comment_id"`
	WorkspaceID id.ID     `json:"workspace_id"`
	SnapshotID  id.ID     `json:"snapshot_id"`
	AuthorID    id.ID     `json:"author"`
	Body        string    `json:"body"`
	CreatedAt   time.Time `json:"created_at"`
}

func NewSnapshotComment(want, workspace, snapshot, author id.ID, body string, at time.Time) (SnapshotComment, error) {
	if want.IsZero() || workspace.IsZero() || snapshot.IsZero() || author.IsZero() {
		return SnapshotComment{}, ErrIDRequired
	}
	if at.IsZero() {
		return SnapshotComment{}, ErrTimeRequired
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return SnapshotComment{}, ErrCommentRequired
	}
	if !utf8.ValidString(body) || strings.ContainsRune(body, 0) || len(body) > MaxCommentBodyLength {
		return SnapshotComment{}, ErrCommentTooLong
	}
	return SnapshotComment{ID: want, WorkspaceID: workspace, SnapshotID: snapshot, AuthorID: author, Body: body, CreatedAt: at}, nil
}
