package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	AlertKindCaptureChanged = "capture_changed"
	AlertKindWatchFailed    = "watch_failed"
	AlertKindQuestionGap    = "question_gap"
	AlertKindRecordGap      = "record_gap"
	AlertKindClusterGap     = "cluster_gap"
)

var ErrAlertInvalid = errors.New(errors.Invalid, "source alert is invalid")

type Alert struct {
	ID          id.ID     `json:"alert_id"`
	WorkspaceID id.ID     `json:"workspace_id"`
	SourceID    *id.ID    `json:"source_id,omitempty"`
	CaptureID   *id.ID    `json:"capture_id,omitempty"`
	QuestionID  *id.ID    `json:"question_id,omitempty"`
	RecordID    *id.ID    `json:"record_id,omitempty"`
	ClusterID   *id.ID    `json:"cluster_id,omitempty"`
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	Detail      string    `json:"detail"`
	DedupeKey   string    `json:"dedupe_key"`
	Active      bool      `json:"active"`
	CreatedBy   id.ID     `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

type AlertRow struct {
	Alert
	SourceTitle string     `json:"source_title,omitempty"`
	SeenAt      *time.Time `json:"seen_at,omitempty"`
}

func NewAlert(want, workspace, source, capture, actor id.ID, kind, title, detail string, at time.Time) (Alert, error) {
	title = strings.TrimSpace(title)
	detail = strings.TrimSpace(detail)
	if want.IsZero() || workspace.IsZero() || source.IsZero() || actor.IsZero() || at.IsZero() || (kind != AlertKindCaptureChanged && kind != AlertKindWatchFailed) || !metadata(title, 400) || title == "" || !metadata(detail, 2000) {
		return Alert{}, ErrAlertInvalid
	}
	if kind == AlertKindCaptureChanged && capture.IsZero() {
		return Alert{}, ErrAlertInvalid
	}
	if kind == AlertKindWatchFailed && !capture.IsZero() {
		return Alert{}, ErrAlertInvalid
	}
	return Alert{ID: want, WorkspaceID: workspace, SourceID: pointer(source), CaptureID: pointerIfPresent(capture), Kind: kind, Title: title, Detail: detail, DedupeKey: want.String(), Active: true, CreatedBy: actor, CreatedAt: at}, nil
}

func NewQuestionGapAlert(want, workspace, question, actor id.ID, dedupeKey, title, detail string, at time.Time) (Alert, error) {
	return NewDerivedGapAlert(want, workspace, AlertKindQuestionGap, question, actor, dedupeKey, title, detail, at)
}

func NewDerivedGapAlert(want, workspace id.ID, kind string, target, actor id.ID, dedupeKey, title, detail string, at time.Time) (Alert, error) {
	title = strings.TrimSpace(title)
	detail = strings.TrimSpace(detail)
	dedupeKey = strings.TrimSpace(dedupeKey)
	if want.IsZero() || workspace.IsZero() || target.IsZero() || actor.IsZero() || at.IsZero() || !metadata(dedupeKey, 200) || dedupeKey == "" || !metadata(title, 400) || title == "" || !metadata(detail, 2000) {
		return Alert{}, ErrAlertInvalid
	}
	out := Alert{ID: want, WorkspaceID: workspace, Kind: kind, Title: title, Detail: detail, DedupeKey: dedupeKey, Active: true, CreatedBy: actor, CreatedAt: at}
	switch kind {
	case AlertKindQuestionGap:
		out.QuestionID = pointer(target)
	case AlertKindRecordGap:
		out.RecordID = pointer(target)
	case AlertKindClusterGap:
		out.ClusterID = pointer(target)
	default:
		return Alert{}, ErrAlertInvalid
	}
	return out, nil
}

func pointer(value id.ID) *id.ID {
	return &value
}

func pointerIfPresent(value id.ID) *id.ID {
	if value.IsZero() {
		return nil
	}
	return pointer(value)
}
