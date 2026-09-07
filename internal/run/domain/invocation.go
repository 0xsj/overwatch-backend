package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Phase is where one process got to. Six, and FOUR OF THEM FINISH WITH NO
// ARTIFACT for reasons that have nothing to do with each other — decisions/0033
// and the client's own module note.
//
// It is `Phase` rather than `State` because [Run] already owns that name in this
// package and two `State` types one import apart is how a caller passes the
// wrong one.
type Phase uint8

const (
	PhasePending Phase = iota
	PhaseRunning
	PhaseOK
	PhaseFailed
	PhaseRefused
	PhaseSkipped
)

var phaseNames = map[Phase]string{
	PhasePending: "pending", PhaseRunning: "running", PhaseOK: "ok",
	PhaseFailed: "failed", PhaseRefused: "refused", PhaseSkipped: "skipped",
}

func (p Phase) String() string {
	if n, ok := phaseNames[p]; ok {
		return n
	}
	return "pending"
}

func ParsePhase(s string) (Phase, error) {
	for k, name := range phaseNames {
		if name == s {
			return k, nil
		}
	}
	return PhasePending, ErrStateUnknown
}

// Terminal says whether anything will ever touch this row again.
func (p Phase) Terminal() bool {
	switch p {
	case PhaseOK, PhaseFailed, PhaseRefused, PhaseSkipped:
		return true
	}
	return false
}

// HadProcess is the question `exit_code` depends on, and it is asked in one
// place so the schema constraint and the domain cannot disagree.
func (p Phase) HadProcess() bool { return p == PhaseOK || p == PhaseFailed }

// Invocation is one process inside a run — or the record of one that never
// existed.
type Invocation struct {
	ID          id.ID
	RunID       id.ID
	WorkspaceID id.ID

	// StepID is which node of the chain this was, and ToolID is what it ran.
	// Both are recorded because a chain edited tomorrow must not change what
	// this row says happened — decisions/0032.
	StepID id.ID
	ToolID id.ID

	// Subject is WHAT THIS WAS AIMED AT — decisions/0037. Both values exist at
	// plan time (they are what the spawn gate is asked) and were thrown away
	// until coverage needed them.
	//
	// SubjectValue is FOLDED, matching `entity.fragment.value`. 0037 names the
	// fold mismatch as its own quiet failure: unfolded here, every coverage cell
	// reads `never` and every row is otherwise correct.
	SubjectKind  string
	SubjectValue string

	// Sequence is the position in topological order, so a reader can lay the
	// run out without re-deriving the graph from a chain that may have moved.
	Sequence int

	Phase Phase

	// Argv is WHAT RAN, resolved and verbatim — never the template. It is
	// populated at plan time even for a refusal, because "the command we would
	// have run" is the thing a person reviews, and a refusal with no argv is a
	// scope proof nobody can read.
	Argv []string

	// Binary is the resolved path, not the name asked for. Which `httpx` ran
	// matters when there are two on the PATH. Empty when nothing spawned.
	Binary string

	// ExitCode is meaningful only when Phase.HadProcess(). Present is not the
	// same as zero and absent is not the same as unknown: absent means NO
	// PROCESS EVER EXISTED.
	ExitCode    int
	HasExitCode bool

	// Signal names how it died when it did not exit — a tool killed at the
	// timeout has no exit code and this is the only record of why.
	Signal string

	// RefusalRule is the scope rule that refused this spawn — decisions/0010
	// and 0030. It is one of the three surfaces that cite a rule id, and it is
	// why a rule is superseded rather than edited.
	RefusalRule   id.ID
	RefusalReason string

	// PermitRule is the rule that ALLOWED this spawn — decisions/0035's lineage
	// step four. It is a SEPARATE field from RefusalRule because a refusal and a
	// permission are different facts, which is the same reason `Refuse` and
	// `NotInScope` are separate constructors.
	//
	// Zero when nothing had to permit by name, which today means the plan
	// never asked — a skipped step.
	PermitRule id.ID

	// SkippedBecause names what upstream did not produce. Today it always names
	// `observation`, because nothing parses output yet.
	SkippedBecause string

	// Unavailable carries execx's reason when the tool could not start at all —
	// off PATH, not executable. CLAUDE.md's `health` noun exists because that
	// failure otherwise looks like silence.
	Unavailable string

	StartedAt  time.Time
	FinishedAt time.Time
	DurationMS int64
}

// Permit records which rule allowed this spawn. It does NOT change the phase:
// a permitted step stays `pending` until something runs it, and 0033's plan is
// written before anything spawns.
func (i Invocation) Permit(rule id.ID) Invocation {
	next := i
	next.PermitRule = rule
	return next
}

// Plan builds an invocation that has not run. Every field that describes an
// outcome is deliberately absent, and the constructors below are the only way
// to fill them.
func Plan(newID, run, workspace, step, tool id.ID, sequence int, argv []string,
	subjectKind, subjectValue string) (Invocation, error) {
	if newID.IsZero() || run.IsZero() || step.IsZero() || tool.IsZero() {
		return Invocation{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Invocation{}, ErrWorkspaceRequired
	}
	if len(argv) == 0 {
		return Invocation{}, ErrArgvEmpty
	}
	subjectValue = strings.ToLower(strings.TrimSpace(subjectValue))
	if subjectKind == "" || subjectValue == "" {
		// A step the gate could not be asked about — an untranslatable kind —
		// carries neither. The pair moves together so a half-set subject is
		// impossible, which is the schema's constraint stated in Go.
		subjectKind, subjectValue = "", ""
	}
	return Invocation{
		ID: newID, RunID: run, WorkspaceID: workspace, StepID: step, ToolID: tool,
		Sequence: sequence, Phase: PhasePending, Argv: argv,
		SubjectKind: subjectKind, SubjectValue: subjectValue,
	}, nil
}

// Refuse records a spawn that never happened. It KEEPS the argv, because the
// command that would have run is what a person reviews, and it refuses an exit
// code by construction rather than by a comment.
func (i Invocation) Refuse(rule id.ID, reason string, at time.Time) (Invocation, error) {
	if at.IsZero() {
		return i, ErrTimeRequired
	}
	if rule.IsZero() {
		return i, ErrRuleRequired
	}
	next := i
	next.Phase = PhaseRefused
	next.RefusalRule = rule
	next.RefusalReason = reason
	next.HasExitCode = false
	next.ExitCode = 0
	next.FinishedAt = at
	return next, nil
}

// NotInScope is the refusal with NO RULE, and it is a different fact from a
// rule that excluded this: nothing permitted it. decisions/0010 — nothing is in
// scope until a rule says so — makes the empty rule set a refusal rather than a
// permission, and this is what that looks like in the record.
func (i Invocation) NotInScope(reason string, at time.Time) (Invocation, error) {
	if at.IsZero() {
		return i, ErrTimeRequired
	}
	next := i
	next.Phase = PhaseRefused
	next.RefusalReason = reason
	next.FinishedAt = at
	return next, nil
}

func (i Invocation) Skip(because string, at time.Time) (Invocation, error) {
	if at.IsZero() {
		return i, ErrTimeRequired
	}
	if because == "" {
		return i, ErrReasonRequired
	}
	next := i
	next.Phase = PhaseSkipped
	next.SkippedBecause = because
	next.FinishedAt = at
	return next, nil
}

func (i Invocation) Start(at time.Time) (Invocation, error) {
	if at.IsZero() {
		return i, ErrTimeRequired
	}
	if i.Phase != PhasePending {
		return i, ErrNotRunnable
	}
	next := i
	next.Phase = PhaseRunning
	next.StartedAt = at
	return next, nil
}

// Ended records a process that finished. `succeeded` is the TOOL's answer, not
// this package's: nuclei exits 1 when it finds nothing, and decisions/0033 puts
// that list on the tool because collapsing it here would make "found nothing"
// and "broke" the same row.
func (i Invocation) Ended(argv []string, binary string, exit int, succeeded bool,
	signal string, at time.Time, took time.Duration) (Invocation, error) {
	if at.IsZero() {
		return i, ErrTimeRequired
	}
	next := i
	if len(argv) > 0 {
		next.Argv = argv
	}
	next.Binary = binary
	next.ExitCode = exit
	next.HasExitCode = true
	next.Signal = signal
	next.FinishedAt = at
	next.DurationMS = took.Milliseconds()
	if succeeded {
		next.Phase = PhaseOK
	} else {
		next.Phase = PhaseFailed
	}
	return next, nil
}

// Broke records a process that could not be observed — off PATH, not
// executable, killed at the timeout. It is `failed` WITH NO EXIT CODE, which is
// the one case where those two travel together: something went wrong and no
// number describes it.
func (i Invocation) Broke(reason string, signal string, at time.Time, took time.Duration) (Invocation, error) {
	if at.IsZero() {
		return i, ErrTimeRequired
	}
	next := i
	next.Phase = PhaseFailed
	next.Unavailable = reason
	next.Signal = signal
	next.HasExitCode = false
	next.ExitCode = 0
	next.FinishedAt = at
	next.DurationMS = took.Milliseconds()
	return next, nil
}

// Valid is the invariant the schema also holds, stated once here so a store and
// a constraint cannot drift about it.
func (i Invocation) Valid() error {
	if i.HasExitCode && !i.Phase.HadProcess() {
		return ErrExitOnRefusal
	}
	if !i.PermitRule.IsZero() && i.Phase == PhaseRefused {
		// A row cannot have been both permitted and refused. The schema holds
		// the same rule; stating it here means the error names it.
		return ErrExitOnRefusal
	}
	if i.Phase == PhaseSkipped && i.SkippedBecause == "" {
		return ErrReasonRequired
	}
	return nil
}

// Checked is the newest finished invocation of one check against one subject —
// what coverage joins against.
//
// A REFUSED invocation is deliberately not here. The scope gate said no, so
// nothing looked, and that is `never` — rendering a refusal as coverage would
// make a wall look like a measurement.
type Checked struct {
	CheckID id.ID
	Kind    string
	Value   string
	At      time.Time
}

// InvocationCheck maps an invocation to the check its run answered. Coverage
// needs it because a check has looked at every subject it PRODUCED as well as
// the one it was aimed at.
type InvocationCheck struct {
	InvocationID id.ID
	CheckID      id.ID
}
