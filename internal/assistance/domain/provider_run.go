package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	MaxProviderRunKind     = 80
	MaxProviderRunProvider = 200
	MaxProviderRunMethod   = 200
	MaxProviderRunTemplate = 200
	MaxProviderRunError    = 2000
)

// ProviderRun is the operational record for one bounded assistance attempt.
// It contains measurements and failure metadata only; retained evidence and
// provider payloads remain in their type-specific, permission-aware histories.
type ProviderRun struct {
	ID              id.ID     `json:"provider_run_id"`
	WorkspaceID     id.ID     `json:"workspace_id"`
	Kind            string    `json:"kind"`
	ResultID        id.ID     `json:"result_id"`
	Provider        string    `json:"provider"`
	Method          string    `json:"method"`
	TemplateVersion string    `json:"template_version"`
	Status          string    `json:"status"`
	InputBytes      int64     `json:"input_bytes"`
	OutputBytes     int64     `json:"output_bytes"`
	DurationMS      int64     `json:"duration_ms"`
	TimedOut        bool      `json:"timed_out"`
	Error           string    `json:"error,omitempty"`
	CreatedBy       id.ID     `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
	CompletedAt     time.Time `json:"completed_at"`
}

func NewProviderRun(want, workspace, result, actor id.ID, kind, provider, method, templateVersion, status string, inputBytes, outputBytes, durationMS int64, timedOut bool, failure string, createdAt, completedAt time.Time) (ProviderRun, error) {
	if want.IsZero() || workspace.IsZero() || result.IsZero() || actor.IsZero() {
		return ProviderRun{}, ErrIDRequired
	}
	if !boundedProviderRunText(kind, MaxProviderRunKind) || !boundedProviderRunText(provider, MaxProviderRunProvider) || !boundedProviderRunText(method, MaxProviderRunMethod) || !boundedProviderRunText(templateVersion, MaxProviderRunTemplate) || !boundedProviderRunText(status, MaxProviderRunKind) {
		return ProviderRun{}, errors.New(errors.Invalid, "provider run metadata is invalid")
	}
	if inputBytes < 0 || outputBytes < 0 || durationMS < 0 {
		return ProviderRun{}, errors.New(errors.Invalid, "provider run measurements are invalid")
	}
	if !utf8.ValidString(failure) || strings.ContainsRune(failure, 0) || len(failure) > MaxProviderRunError {
		return ProviderRun{}, errors.New(errors.Invalid, "provider run error is invalid")
	}
	if createdAt.IsZero() || completedAt.IsZero() || completedAt.Before(createdAt) {
		return ProviderRun{}, ErrTimeRequired
	}
	return ProviderRun{
		ID: want, WorkspaceID: workspace, Kind: strings.TrimSpace(kind), ResultID: result,
		Provider: strings.TrimSpace(provider), Method: strings.TrimSpace(method), TemplateVersion: strings.TrimSpace(templateVersion), Status: strings.TrimSpace(status),
		InputBytes: inputBytes, OutputBytes: outputBytes, DurationMS: durationMS, TimedOut: timedOut, Error: strings.TrimSpace(failure),
		CreatedBy: actor, CreatedAt: createdAt, CompletedAt: completedAt,
	}, nil
}

func boundedProviderRunText(value string, max int) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= max && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}
