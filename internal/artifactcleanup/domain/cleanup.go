// Package domain describes the reference-aware physical cleanup boundary.
// Source purge changes access and keeps its record; this package only removes
// bytes after every known citation family says the content address is unused.
package domain

import (
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	KindSourceCapture    = "source_capture"
	KindSourceExtraction = "source_extraction"
	EventSwept           = "artifact.swept"
	EventSweepFailed     = "artifact.sweep_failed"
	SweepRunning         = "running"
	SweepCompleted       = "completed"
	SweepFailed          = "failed"
	SweepInterrupted     = "interrupted"
	ReviewNone           = "none"
	ReviewOpen           = "open"
	ReviewCompleted      = "completed"
	ReviewDiscarded      = "discarded"
	StateProtected       = "protected"
	StateEligible        = "eligible"
	StateMissing         = "missing"
	StateSwept           = "swept"
	StateAlreadyGone     = "already_gone"
	StateMismatched      = "mismatched"
	StateCorrupted       = "corrupted"
	StateUnavailable     = "unavailable"
	OutcomeDeleted       = "deleted"
	OutcomeAlreadyGone   = "already_gone"
	OutcomeSkipped       = "skipped"
	OutcomeFailed        = "failed"
)

type Candidate struct {
	Ref          string    `json:"ref"`
	Kind         string    `json:"kind"`
	WorkspaceID  id.ID     `json:"workspace_id"`
	SourceID     id.ID     `json:"source_id"`
	CaptureID    id.ID     `json:"capture_id,omitempty"`
	ExtractionID id.ID     `json:"extraction_id,omitempty"`
	Bytes        int64     `json:"bytes"`
	PurgedAt     time.Time `json:"purged_at"`
	CreatedAt    time.Time `json:"created_at"`
}

type Inventory struct {
	Items          []Candidate `json:"items"`
	CandidateCount int         `json:"candidate_count"`
	CandidateBytes int64       `json:"candidate_bytes"`
	RecentSweeps   []SweepRun  `json:"recent_sweeps"`
}

type SweepRun struct {
	ID               id.ID      `json:"sweep_id"`
	WorkspaceID      id.ID      `json:"workspace_id"`
	RequestedBy      id.ID      `json:"requested_by"`
	ReviewID         *id.ID     `json:"review_id,omitempty"`
	Status           string     `json:"status"`
	Limit            int        `json:"limit"`
	CandidateCount   int        `json:"candidate_count"`
	CandidateBytes   int64      `json:"candidate_bytes"`
	DeletedCount     int        `json:"deleted_count"`
	DeletedBytes     int64      `json:"deleted_bytes"`
	AlreadyGoneCount int        `json:"already_gone_count"`
	SkippedCount     int        `json:"skipped_count"`
	Error            string     `json:"error,omitempty"`
	StartedAt        time.Time  `json:"started_at"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
}

type Review struct {
	ID            id.ID       `json:"review_id"`
	WorkspaceID   id.ID       `json:"workspace_id"`
	CreatedBy     id.ID       `json:"created_by"`
	UpdatedBy     id.ID       `json:"updated_by"`
	Status        string      `json:"status"`
	Items         []Candidate `json:"items"`
	SelectedRefs  []string    `json:"selected_refs"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
	CompletedAt   *time.Time  `json:"completed_at,omitempty"`
	SweepID       *id.ID      `json:"sweep_id,omitempty"`
	DiscardedBy   *id.ID      `json:"discarded_by,omitempty"`
	DiscardedAt   *time.Time  `json:"discarded_at,omitempty"`
	DiscardReason string      `json:"discard_reason,omitempty"`
}

func (r Review) Refs() []string {
	refs := make([]string, 0, len(r.Items))
	for _, item := range r.Items {
		refs = append(refs, item.Ref)
	}
	return refs
}

type ReviewSummary struct {
	ID            id.ID      `json:"review_id"`
	WorkspaceID   id.ID      `json:"workspace_id"`
	CreatedBy     id.ID      `json:"created_by"`
	UpdatedBy     id.ID      `json:"updated_by"`
	Status        string     `json:"status"`
	ItemCount     int        `json:"item_count"`
	ItemBytes     int64      `json:"item_bytes"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	SweepID       *id.ID     `json:"sweep_id,omitempty"`
	DiscardedBy   *id.ID     `json:"discarded_by,omitempty"`
	DiscardedAt   *time.Time `json:"discarded_at,omitempty"`
	DiscardReason string     `json:"discard_reason,omitempty"`
}

type ReviewPage struct {
	Items []ReviewSummary `json:"items"`
	Count int             `json:"count"`
}

// Reference is the durable reference view before physical storage is checked.
// It deliberately includes protection counts from every known reference family
// so the lifecycle reader cannot mistake a workspace-local absence for global
// unreferenced bytes.
type Reference struct {
	Ref                 string
	Kind                string
	WorkspaceID         id.ID
	SourceID            id.ID
	CaptureID           id.ID
	ExtractionID        id.ID
	Bytes               int64
	PurgedAt            *time.Time
	CreatedAt           time.Time
	ReferenceCount      int
	LiveSourceCount     int
	LiveExtractionCount int
	RunReferenced       bool
	ReportReferenced    bool
}

type SweepItem struct {
	SweepID      id.ID
	WorkspaceID  id.ID
	Ref          string
	Kind         string
	SourceID     id.ID
	CaptureID    id.ID
	ExtractionID id.ID
	Bytes        int64
	Outcome      string
	Reason       string
	RecordedAt   time.Time
}

type ArtifactStatus struct {
	Ref                 string     `json:"ref"`
	Kind                string     `json:"kind"`
	WorkspaceID         id.ID      `json:"workspace_id"`
	SourceID            id.ID      `json:"source_id"`
	CaptureID           id.ID      `json:"capture_id,omitempty"`
	ExtractionID        id.ID      `json:"extraction_id,omitempty"`
	Bytes               int64      `json:"bytes"`
	PurgedAt            *time.Time `json:"purged_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	ReferenceCount      int        `json:"reference_count"`
	LiveSourceCount     int        `json:"live_source_count"`
	LiveExtractionCount int        `json:"live_extraction_count"`
	RunReferenced       bool       `json:"run_referenced"`
	ReportReferenced    bool       `json:"report_referenced"`
	State               string     `json:"state"`
	Reason              string     `json:"reason"`
	LastSweepID         *id.ID     `json:"last_sweep_id,omitempty"`
	LastSweepOutcome    string     `json:"last_sweep_outcome,omitempty"`
	LastSweepAt         *time.Time `json:"last_sweep_at,omitempty"`
}

type StatusPage struct {
	Items       []ArtifactStatus `json:"items"`
	Count       int              `json:"count"`
	StateCounts map[string]int   `json:"state_counts"`
	NextCursor  *string          `json:"next_cursor"`
}

type SweepResult struct {
	Inventory    Inventory   `json:"inventory"`
	Run          SweepRun    `json:"run"`
	Deleted      []Candidate `json:"deleted"`
	AlreadyGone  []Candidate `json:"already_gone"`
	Skipped      []Candidate `json:"skipped"`
	DeletedBytes int64       `json:"deleted_bytes"`
}

type Skip struct {
	Candidate Candidate `json:"candidate"`
	Reason    string    `json:"reason"`
}
