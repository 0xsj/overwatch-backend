package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	MaxSignatureLength = 400
	MaxReasonLength    = 4000
	MaxFieldLength     = 200
	MaxValueLength     = 8000
)

// State is where a finding sits — decisions/0041 §3. Four, and they deliberately
// do NOT share the judgement quartet's words: `0009` is explicit that a finding
// is not a judgement, and reusing `unopened`/`watching` would invite exactly the
// collapse that record refused.
//
//	open       nobody has looked
//	triaged    somebody looked, it is real
//	resolved   THE THING WAS FIXED
//	dismissed  ruled not to matter, WITH a reason
//
// **`resolved` and `dismissed` must never collapse.** One is a change to the
// world, the other is a change of mind, and a client report cites them
// differently: *"we fixed eleven"* and *"we decided eleven did not matter"* are
// not the same sentence to hand a client.
type State uint8

const (
	StateOpen State = iota
	StateTriaged
	StateResolved
	StateDismissed
)

var stateNames = map[State]string{
	StateOpen: "open", StateTriaged: "triaged",
	StateResolved: "resolved", StateDismissed: "dismissed",
}

func (s State) String() string {
	if n, ok := stateNames[s]; ok {
		return n
	}
	return "open"
}

func ParseState(s string) (State, error) {
	for state, name := range stateNames {
		if name == s {
			return state, nil
		}
	}
	return StateOpen, ErrStateUnknown
}

// Live says whether this finding still counts against the estate. It is what
// `0009`'s badge counts — "this fragment has an open finding" — and it is one
// function so the badge and the board cannot disagree.
func (s State) Live() bool { return s == StateOpen || s == StateTriaged }

// Severity is the ordered scale, and it is a SCALAR beside its claim rather than
// nested inside it — `0004`, because it is the sort key and the filter key for
// the board.
type Severity uint8

const (
	SeverityInfo Severity = iota
	SeverityLow
	SeverityMedium
	SeverityHigh
	SeverityCritical
)

var severityNames = map[Severity]string{
	SeverityInfo: "info", SeverityLow: "low", SeverityMedium: "medium",
	SeverityHigh: "high", SeverityCritical: "critical",
}

func (s Severity) String() string {
	if n, ok := severityNames[s]; ok {
		return n
	}
	return "info"
}

// ParseSeverity accepts what tools actually write. `nuclei` emits lowercase;
// something else will emit `Medium` or `INFO`, and refusing those would drop a
// real finding over a capital letter.
//
// **An unrecognised severity is an ERROR and never a default.** Quietly filing
// an unknown word as `info` would hide the loudest thing a scanner said, and
// `unknown` is not a level on `0004`'s scale.
func ParseSeverity(s string) (Severity, error) {
	folded := strings.ToLower(strings.TrimSpace(s))
	for severity, name := range severityNames {
		if name == folded {
			return severity, nil
		}
	}
	return SeverityInfo, ErrSeverityUnknown
}

// SeverityAt turns the ORDER back into a severity — 0 is critical. The order
// lives in SQL because the board sorts on it; this is the one place it is read
// back, so the two cannot drift into disagreeing about which end is worst.
func SeverityAt(order int) Severity {
	switch order {
	case 0:
		return SeverityCritical
	case 1:
		return SeverityHigh
	case 2:
		return SeverityMedium
	case 3:
		return SeverityLow
	default:
		return SeverityInfo
	}
}

// Badge is 0009's "this fragment has an open finding", counted. It carries the
// WORST live severity beside the count so a badge can be coloured without a
// second read.
type Badge struct {
	Live  int
	Worst Severity
}

// Claimant is who assigned a severity — `0004`, the same three an attribution
// carries. It is NEVER absent.
type Claimant uint8

const (
	ByRule Claimant = iota
	ByModel
	ByHuman
)

var claimantNames = map[Claimant]string{
	ByRule: "rule", ByModel: "model", ByHuman: "human",
}

func (c Claimant) String() string {
	if n, ok := claimantNames[c]; ok {
		return n
	}
	return "rule"
}

func ParseClaimant(s string) (Claimant, error) {
	for claimant, name := range claimantNames {
		if name == s {
			return claimant, nil
		}
	}
	return ByRule, ErrClaimantUnknown
}

// Assessment is `0004`'s `severity_by`, and the record's rules are held here
// rather than described in a comment.
//
//	a RULE carries no confidence      a template asserting `high` is a category,
//	                                  not a probability. Storing 1.0 destroys
//	                                  the distinction permanently
//	a MODEL carries one, 0..1         only machines carry confidence
//	an OVERRIDE requires a basis      replacing somebody else's assessment is a
//	                                  DISAGREEMENT, and one with no stated
//	                                  reason records that somebody disagreed
//	                                  without saying why they were right
type Assessment struct {
	Claimant Claimant

	// Actor is set when the claimant is human, and zero otherwise.
	Actor id.ID

	Confidence    float64
	HasConfidence bool

	Basis string
	At    time.Time
}

func (a Assessment) Valid(override bool) error {
	switch a.Claimant {
	case ByRule:
		if a.HasConfidence {
			return ErrConfidenceOnRule
		}
	case ByModel:
		if !a.HasConfidence {
			return ErrConfidenceMissing
		}
		if a.Confidence < 0 || a.Confidence > 1 {
			return ErrConfidenceRange
		}
	case ByHuman:
		if a.HasConfidence {
			// A person is not a probability either. 0004 puts confidence on
			// machines only, and a human slider would make the two claimants
			// comparable when the whole point is that they are not.
			return ErrConfidenceOnHuman
		}
		if a.Actor.IsZero() {
			return ErrActorRequired
		}
	}
	if override && strings.TrimSpace(a.Basis) == "" {
		return ErrBasisRequired
	}
	return nil
}

// Finding is ONE PROBLEM ON ONE FRAGMENT — decisions/0041 §1.
//
// `nuclei` matching one template on one URL every night for a month is one
// finding seen thirty times, not thirty findings. The board answers *"what is
// wrong with this estate"*, and one row per sighting turns it into a scan log
// where triage does not stick: a ruling made on Monday would face a fresh
// untriaged row on Tuesday, forever.
type Finding struct {
	ID          id.ID
	WorkspaceID id.ID

	// The identity — unique together. The TOOL is in it because two scanners'
	// identifier spaces can collide and nothing here can know that one tool's
	// `weak-cipher` means another's; merging them would be asserting that two
	// signatures mean the same thing, which is the similarity claim CLAUDE.md
	// bans.
	ToolID     id.ID
	Signature  string
	FragmentID id.ID

	// FragmentKind and FragmentValue are carried so a board renders without a
	// join into a peer domain. They are a COPY of the fragment's tuple, which
	// is immutable — `0036` makes a fragment IS the tuple — so this cannot go
	// stale the way a denormalised mutable field would.
	FragmentKind  string
	FragmentValue string

	State State

	// ResolvedReason and DismissedReason are ONE column in the schema and two
	// names here would be a lie. `Reason` it is — see [Finding.Dismiss] for why
	// only one of the two requires it.
	Reason string

	// DecidedBy and DecidedAt are who moved it out of `open` and when. Zero
	// while nobody has.
	DecidedBy id.ID
	DecidedAt time.Time

	Severity   Severity
	Assessment Assessment

	// Superseded is the assessment this one replaced, and `0004` requires it to
	// survive: *"an override never deletes what it overrode"*. A human raising
	// a model's severity is the correction this product claims compounds, and a
	// shape keeping only the outcome cannot see it.
	Superseded         *Assessment
	SupersededSeverity Severity

	FirstSeen time.Time
	LastSeen  time.Time
	Sightings int

	// Invocation and Artifact are the run that MOST RECENTLY saw it. The first
	// sighting's are not kept: a finding is a live claim about the estate, and
	// the citation a reader wants is the newest evidence rather than the oldest.
	Invocation id.ID
	Artifact   id.ID
	MappingID  id.ID

	CreatedAt time.Time
}

// New builds a finding from its first sighting. It is BORN OPEN, with the
// tool's own severity as a `rule` assessment.
func New(newID, workspace, tool, fragment id.ID, signature, fragmentKind, fragmentValue string,
	severity Severity, basis string, invocation, artifact, mapping id.ID,
	seenAt, at time.Time) (Finding, error) {
	if newID.IsZero() || tool.IsZero() || fragment.IsZero() {
		return Finding{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Finding{}, ErrWorkspaceRequired
	}
	if invocation.IsZero() || artifact.IsZero() || mapping.IsZero() {
		// A finding this system cannot source does not exist, for the same
		// reason `0003` refuses an unsourceable derivation: it would arrive
		// looking trustworthy.
		return Finding{}, ErrUnsourced
	}
	if seenAt.IsZero() || at.IsZero() {
		return Finding{}, ErrTimeRequired
	}
	signature = strings.TrimSpace(signature)
	if signature == "" || len(signature) > MaxSignatureLength {
		return Finding{}, ErrSignatureRequired
	}
	fragmentKind = strings.TrimSpace(fragmentKind)
	fragmentValue = strings.TrimSpace(fragmentValue)
	if fragmentKind == "" || fragmentValue == "" {
		return Finding{}, ErrSubjectRequired
	}
	assessment := Assessment{Claimant: ByRule, Basis: strings.TrimSpace(basis), At: at}
	if err := assessment.Valid(false); err != nil {
		return Finding{}, err
	}
	return Finding{
		ID: newID, WorkspaceID: workspace, ToolID: tool, Signature: signature,
		FragmentID: fragment, FragmentKind: fragmentKind, FragmentValue: fragmentValue,
		State: StateOpen, Severity: severity, Assessment: assessment,
		FirstSeen: seenAt, LastSeen: seenAt, Sightings: 1,
		Invocation: invocation, Artifact: artifact, MappingID: mapping,
		CreatedAt: at,
	}, nil
}

// Seen records a rescan. **It never changes state and never moves `FirstSeen`
// backwards.**
//
// That is the whole of `0041` §1: a human's ruling is about the problem, not
// about the run that noticed it, and a rescan that reset `triaged` to `open`
// would make triage impossible to keep.
//
// It does NOT re-assert severity either. The tool said `high` on the first
// sighting and says `high` again tonight; overwriting a human's override with
// the template's opinion every night is the same failure one field over.
func (f Finding) Seen(invocation, artifact, mapping id.ID, at time.Time) (Finding, error) {
	if invocation.IsZero() || artifact.IsZero() || mapping.IsZero() {
		return f, ErrUnsourced
	}
	if at.IsZero() {
		return f, ErrTimeRequired
	}
	next := f
	next.Sightings = f.Sightings + 1
	if at.After(f.LastSeen) {
		next.LastSeen = at
		// The citation follows the NEWEST evidence, and only when it is newer:
		// a re-extraction of an old artifact must not make a stale run look
		// like the current one.
		next.Invocation = invocation
		next.Artifact = artifact
		next.MappingID = mapping
	}
	if at.Before(f.FirstSeen) {
		// A re-extraction of an older artifact legitimately moves this back.
		next.FirstSeen = at
	}
	// A RESOLVED finding seen again is OPEN. There is no `regressed` state —
	// 0041 accepted that cost — so a fix that did not hold reads as a new
	// finding, and the evidence a reader has is an old FirstSeen beside a large
	// Sightings.
	if f.State == StateResolved {
		next.State = StateOpen
		next.Reason = ""
		next.DecidedBy = id.ID{}
		next.DecidedAt = time.Time{}
	}
	return next, nil
}

// Triage records that somebody looked and it is real.
func (f Finding) Triage(by id.ID, at time.Time) (Finding, error) {
	return f.decide(StateTriaged, by, "", at, false)
}

// Resolve records that THE THING WAS FIXED. No reason is required: a fix needs
// no argument, because the thing is gone.
func (f Finding) Resolve(by id.ID, reason string, at time.Time) (Finding, error) {
	return f.decide(StateResolved, by, reason, at, false)
}

// Dismiss records a ruling that it does not matter, and REQUIRES a reason.
//
// The asymmetry with [Finding.Resolve] is `0004`'s override rule one noun over:
// deciding a real problem does not matter is a disagreement with the tool that
// found it, and one with no stated reason leaves a record saying THAT somebody
// disagreed without saying WHY THEY WERE RIGHT.
func (f Finding) Dismiss(by id.ID, reason string, at time.Time) (Finding, error) {
	return f.decide(StateDismissed, by, reason, at, true)
}

func (f Finding) decide(to State, by id.ID, reason string, at time.Time, needsReason bool) (Finding, error) {
	if by.IsZero() {
		// Every one of these is a person's act. There is no system path here,
		// deliberately: nothing closes a finding automatically — 0041.
		return f, ErrActorRequired
	}
	if at.IsZero() {
		return f, ErrTimeRequired
	}
	reason = strings.TrimSpace(reason)
	if needsReason && reason == "" {
		return f, ErrReasonRequired
	}
	if len(reason) > MaxReasonLength {
		return f, ErrReasonTooLong
	}
	if f.State == to {
		return f, ErrAlreadyThere
	}
	next := f
	next.State = to
	next.Reason = reason
	next.DecidedBy = by
	next.DecidedAt = at
	return next, nil
}

// Reassess replaces the severity and KEEPS what it replaced — `0004`. A basis is
// required because this is by definition an override.
func (f Finding) Reassess(to Severity, by Assessment) (Finding, error) {
	if err := by.Valid(true); err != nil {
		return f, err
	}
	next := f
	prior := f.Assessment
	next.Superseded = &prior
	next.SupersededSeverity = f.Severity
	next.Severity = to
	next.Assessment = by
	return next, nil
}

// Detail is one field a finding's tool read beside the identity — the name, the
// description, the matcher, the curl command.
//
// **It is not an observation.** An observation says what a source said about a
// SUBJECT, and these are about the FINDING: filing `name: Log4j RCE` as an
// observation of the URL would put it in that asset's field list, where it reads
// as a property of the url. Same shape, different subject, and the subject is
// what an observation IS.
//
// One row per (finding, field): a nightly rescan UPDATES rather than appends, so
// this stays bounded while `sightings` grows.
type Detail struct {
	ID        id.ID
	FindingID id.ID

	Field string
	Value string

	MappingID  id.ID
	ArtifactID id.ID
	SeenAt     time.Time
}

func NewDetail(newID, finding id.ID, field, value string,
	mapping, artifact id.ID, at time.Time) (Detail, error) {
	if newID.IsZero() || finding.IsZero() || mapping.IsZero() || artifact.IsZero() {
		return Detail{}, ErrIDRequired
	}
	if at.IsZero() {
		return Detail{}, ErrTimeRequired
	}
	field = strings.TrimSpace(field)
	if field == "" || len(field) > MaxFieldLength {
		return Detail{}, ErrFieldRequired
	}
	// The VALUE is not trimmed — it is what the source said — and only the
	// upper bound applies, which is the column's.
	if value == "" || len(value) > MaxValueLength {
		return Detail{}, ErrValueRequired
	}
	return Detail{
		ID: newID, FindingID: finding, Field: field, Value: value,
		MappingID: mapping, ArtifactID: artifact, SeenAt: at,
	}, nil
}

const (
	EventFindingOpened   = "finding.opened"
	EventFindingSeen     = "finding.seen"
	EventFindingDecided  = "finding.decided"
	EventFindingReassess = "finding.reassessed"

	SubjectKind = "workspace"
)

// Opened is a NEW problem. `finding.seen` is deliberately a different event: one
// is news and the other is a heartbeat, and a subscriber that wants to page
// somebody wants only the first.
type Opened struct {
	WorkspaceID string `json:"workspace_id"`
	FindingID   string `json:"finding_id"`
	Signature   string `json:"signature"`
	Severity    string `json:"severity"`
	Fragment    string `json:"fragment"`
}

type Decided struct {
	WorkspaceID string `json:"workspace_id"`
	FindingID   string `json:"finding_id"`
	State       string `json:"state"`
	// ByPerson is always true today and is carried anyway, because the day
	// something closes a finding without a person this payload has to be able
	// to say so — and 0041 says nothing may.
	ByPerson bool `json:"by_person"`
}
