package domain

import (
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

// State is where a run is. Three, matching the client's fixture, and `stopped`
// is deliberately not `failed`: a run whose every invocation was refused did not
// break, and one a person cancelled did not either.
type State uint8

const (
	StateRunning State = iota
	StateComplete
	StateStopped
)

var stateNames = map[State]string{
	StateRunning: "running", StateComplete: "complete", StateStopped: "stopped",
}

func (s State) String() string {
	if n, ok := stateNames[s]; ok {
		return n
	}
	return "running"
}

func ParseState(s string) (State, error) {
	for k, name := range stateNames {
		if name == s {
			return k, nil
		}
	}
	return StateRunning, ErrStateUnknown
}

// Run is one pass of a check over a target.
//
// **It does NOT reference the chain it came from.** decisions/0032 required
// that: each invocation records the argv it ran, so the record is
// self-describing and a chain edited tomorrow does not rewrite what happened
// today. CheckID is here to answer "which question was being asked", which is
// what coverage counts, and never to reconstruct the commands.
type Run struct {
	ID          id.ID
	WorkspaceID id.ID
	TargetID    id.ID
	CheckID     id.ID

	State State

	// StartedBy is zero when a schedule started it. Zero means "no person",
	// which is a fact and not a missing value — the pair CLAUDE.md keeps apart
	// everywhere else applies here too.
	StartedBy id.ID

	StartedAt  time.Time
	FinishedAt time.Time
	Version    int
}

func New(newID, workspace, target, check, by id.ID, at time.Time) (Run, error) {
	if newID.IsZero() {
		return Run{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Run{}, ErrWorkspaceRequired
	}
	if target.IsZero() {
		return Run{}, ErrTargetRequired
	}
	if check.IsZero() {
		return Run{}, ErrCheckRequired
	}
	if at.IsZero() {
		return Run{}, ErrTimeRequired
	}
	return Run{
		ID: newID, WorkspaceID: workspace, TargetID: target, CheckID: check,
		State: StateRunning, StartedBy: by, StartedAt: at, Version: 1,
	}, nil
}

func (r Run) Finished() bool { return r.State != StateRunning }

// Finish closes a run. `complete` means every invocation reached a terminal
// state, which INCLUDES every one of them being refused — a run that was wholly
// refused is a complete answer to "may we look at this", and calling it stopped
// would make the scope proof read as a failure.
func (r Run) Finish(state State, at time.Time) (Run, error) {
	if at.IsZero() {
		return r, ErrTimeRequired
	}
	if r.Finished() {
		return r, ErrFinished
	}
	if state == StateRunning {
		return r, ErrStateUnknown
	}
	next := r
	next.State = state
	next.FinishedAt = at
	next.Version = r.Version + 1
	return next, nil
}

const (
	// EventRunStarted is a DECISION — decisions/0014. A person chose to look at
	// a client, and that act has an author, no outcome column, and a reason to
	// outlive the journal's retention.
	EventRunStarted = "run.started"

	// The rest are WORK: each can fail, and the outcome is the whole point.
	EventRunFinished       = "run.finished"
	EventInvocationStarted = "invocation.started"
	EventInvocationEnded   = "invocation.ended"

	SubjectKind = "run"
)

type Started struct {
	RunID       string `json:"run_id"`
	WorkspaceID string `json:"workspace_id"`
	TargetID    string `json:"target_id"`
	CheckID     string `json:"check_id"`
	// Planned, Refused and Skipped are what the SPAWN GATE decided before
	// anything ran. They are in the envelope because a subscriber cannot
	// compute them — decisions/0013 — and because "we planned six and the gate
	// refused four" is the sentence somebody wants without opening the run.
	Planned int `json:"planned"`
	Refused int `json:"refused"`
	Skipped int `json:"skipped"`
}

type Finished struct {
	RunID       string `json:"run_id"`
	WorkspaceID string `json:"workspace_id"`
	State       string `json:"state"`
	Ran         int    `json:"ran"`
	Failed      int    `json:"failed"`
}

type InvocationEnded struct {
	InvocationID string `json:"invocation_id"`
	RunID        string `json:"run_id"`
	WorkspaceID  string `json:"workspace_id"`
	ToolID       string `json:"tool_id"`
	State        string `json:"state"`
	// Bytes is a POINTER because 0 and absent are different facts: it ran and
	// wrote an empty artifact, versus nothing was written. A plain int would
	// encode both as 0.
	Bytes *int64 `json:"bytes,omitempty"`

	// Observations is a POINTER for the same reason: `0` means the mapping ran
	// and matched nothing, and ABSENT means nothing was read out of anything —
	// no mappings exist for this tool yet. The client's own fixture already
	// states that distinction.
	Observations *int `json:"observations,omitempty"`
}
