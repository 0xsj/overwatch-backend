package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	EventWatchConfigured = "source.watch.configured"
	EventWatchRun        = "source.watch.run"

	WatchStatusNever     = "never"
	WatchStatusChanged   = "changed"
	WatchStatusUnchanged = "unchanged"
	WatchStatusFailed    = "failed"

	DefaultWatchIntervalSeconds = 3600
	MinWatchIntervalSeconds     = 900
	MaxWatchIntervalSeconds     = 604800
)

var (
	ErrWatchInvalid       = errors.New(errors.Invalid, "source watch configuration is invalid")
	ErrWatchSourceInvalid = errors.New(errors.Invalid, "only an unpurged URL reference can be monitored")
	ErrWatchNotDue        = errors.New(errors.NotFound, "no due source watch is available")
	ErrWatchLeased        = errors.New(errors.Conflict, "source watch is already leased")
)

type Watch struct {
	WorkspaceID     id.ID      `json:"workspace_id"`
	SourceID        id.ID      `json:"source_id"`
	Enabled         bool       `json:"enabled"`
	IntervalSeconds int        `json:"interval_seconds"`
	NextRunAt       *time.Time `json:"next_run_at,omitempty"`
	LastRunAt       *time.Time `json:"last_run_at,omitempty"`
	LastStatus      string     `json:"last_status"`
	LastCaptureID   id.ID      `json:"last_capture_id,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	LeaseOwner      string     `json:"lease_owner,omitempty"`
	LeaseUntil      *time.Time `json:"lease_until,omitempty"`
	UpdatedBy       id.ID      `json:"updated_by,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func DefaultWatch(workspace, source id.ID) Watch {
	return Watch{WorkspaceID: workspace, SourceID: source, IntervalSeconds: DefaultWatchIntervalSeconds, LastStatus: WatchStatusNever}
}

func NewWatch(workspace, source, actor id.ID, enabled bool, intervalSeconds int, at time.Time) (Watch, error) {
	if workspace.IsZero() || source.IsZero() || actor.IsZero() || at.IsZero() || !validWatchInterval(intervalSeconds) {
		return Watch{}, ErrWatchInvalid
	}
	watch := Watch{WorkspaceID: workspace, SourceID: source, Enabled: enabled, IntervalSeconds: intervalSeconds, LastStatus: WatchStatusNever, UpdatedBy: actor, UpdatedAt: at}
	if enabled {
		next := at.Add(time.Duration(intervalSeconds) * time.Second)
		watch.NextRunAt = &next
	}
	return watch, nil
}

func (w Watch) Configure(actor id.ID, enabled bool, intervalSeconds int, at time.Time) (Watch, error) {
	if actor.IsZero() || at.IsZero() || !validWatchInterval(intervalSeconds) || w.WorkspaceID.IsZero() || w.SourceID.IsZero() {
		return w, ErrWatchInvalid
	}
	next := w
	next.Enabled = enabled
	next.IntervalSeconds = intervalSeconds
	next.UpdatedBy = actor
	next.UpdatedAt = at
	next.NextRunAt = nil
	next.LeaseOwner = ""
	next.LeaseUntil = nil
	if enabled {
		run := at.Add(time.Duration(intervalSeconds) * time.Second)
		next.NextRunAt = &run
	}
	return next, nil
}

func (w Watch) RecordRun(actor id.ID, status string, capture id.ID, runError string, at time.Time) (Watch, error) {
	return w.recordRun(actor, status, capture, runError, "", at)
}

func (w Watch) RecordClaimedRun(actor id.ID, status string, capture id.ID, runError, leaseOwner string, at time.Time) (Watch, error) {
	return w.recordRun(actor, status, capture, runError, strings.TrimSpace(leaseOwner), at)
}

func (w Watch) recordRun(actor id.ID, status string, capture id.ID, runError, leaseOwner string, at time.Time) (Watch, error) {
	if actor.IsZero() || at.IsZero() || !w.Enabled || !validWatchStatus(status) || !metadata(strings.TrimSpace(runError), 2000) {
		return w, ErrWatchInvalid
	}
	if w.LeaseOwner != "" && (w.LeaseOwner != leaseOwner || w.LeaseUntil == nil || !w.LeaseUntil.After(at)) {
		return w, ErrWatchLeased
	}
	if (status == WatchStatusChanged && capture.IsZero()) || (status != WatchStatusChanged && !capture.IsZero()) {
		return w, ErrWatchInvalid
	}
	next := w
	next.LastRunAt = &at
	next.LastStatus = status
	next.LastCaptureID = capture
	next.LastError = strings.TrimSpace(runError)
	next.UpdatedBy = actor
	next.UpdatedAt = at
	next.LeaseOwner = ""
	next.LeaseUntil = nil
	run := at.Add(time.Duration(w.IntervalSeconds) * time.Second)
	next.NextRunAt = &run
	return next, nil
}

func (w Watch) Claim(owner string, at, leaseUntil time.Time) (Watch, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || len(owner) > 200 || at.IsZero() || leaseUntil.IsZero() || !leaseUntil.After(at) || !w.Enabled || w.NextRunAt == nil || w.NextRunAt.After(at) {
		return w, ErrWatchInvalid
	}
	if w.LeaseOwner != "" && w.LeaseUntil != nil && w.LeaseUntil.After(at) {
		return w, ErrWatchLeased
	}
	next := w
	next.LeaseOwner = owner
	next.LeaseUntil = &leaseUntil
	return next, nil
}

func validWatchInterval(seconds int) bool {
	return seconds >= MinWatchIntervalSeconds && seconds <= MaxWatchIntervalSeconds
}

func validWatchStatus(status string) bool {
	switch status {
	case WatchStatusChanged, WatchStatusUnchanged, WatchStatusFailed:
		return true
	default:
		return false
	}
}
