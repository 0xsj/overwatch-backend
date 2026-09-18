package domain

import (
	"sort"
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Candidate is ONE THING a step was pointed at — decisions/0039.
//
// A step is one invocation however many things it touches, because `httpx -l
// hosts.txt` is one process and modelling forty hosts as forty invocations would
// be a record of something that did not happen. What it touched is a row each,
// and this is that row.
//
// **THIS IS WHERE THE SCOPE PROOF LIVES.** `0037` put `subject_kind` and
// `subject_value` on the invocation, which was right while a step touched one
// thing. Widening those to an array was the obvious move and it is refused: a
// list of what was permitted silently discards *"and these three were not"*,
// which is the half a client's report cites and the reason `0030` keeps a rule
// append-only.
//
//	Permitted   it went into the argv.  COVERAGE reads these
//	refused     a rule said no.         THE SCOPE PROOF reads these
type Candidate struct {
	ID           id.ID
	WorkspaceID  id.ID
	RunID        id.ID
	InvocationID id.ID

	// Kind is the shared vocabulary's word — 0034. It comes from the tool's
	// `produces` at a source step and from the OBSERVATION at a downstream one,
	// and only the first of those is wrong (owed item R: a target carries no
	// seed fragment kind).
	Kind string

	// Value is FOLDED, matching `entity.fragment.value` and
	// `observation.subject_value`. Unfolded, every coverage cell reads `never`
	// and every row is otherwise correct — 0037 named that as its own quiet
	// failure and this is the join it was about.
	Value string

	Permitted bool

	// RefusalRule is zero when NOTHING PERMITTED this rather than a rule having
	// excluded it — 0010's default, and a different fact from an exclusion. The
	// reason is present on every refusal either way.
	RefusalRule   id.ID
	RefusalReason string

	CreatedAt time.Time
}

// NewCandidate records something a step was aimed at, before the gate has
// answered. It is deliberately born neither permitted nor refused: the two
// constructors below are the only way to settle it, so a candidate that was
// never asked about cannot read as permitted.
func NewCandidate(newID, workspace, run, invocation id.ID, kind, value string,
	at time.Time) (Candidate, error) {
	if newID.IsZero() || run.IsZero() || invocation.IsZero() {
		return Candidate{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Candidate{}, ErrWorkspaceRequired
	}
	if at.IsZero() {
		return Candidate{}, ErrTimeRequired
	}
	kind = strings.TrimSpace(kind)
	value = Fold(value)
	if kind == "" || value == "" {
		return Candidate{}, ErrCandidateEmpty
	}
	return Candidate{
		ID: newID, WorkspaceID: workspace, RunID: run, InvocationID: invocation,
		Kind: kind, Value: value, CreatedAt: at,
	}, nil
}

// Permit marks a candidate as having gone into the argv.
func (c Candidate) Permit() Candidate {
	next := c
	next.Permitted = true
	next.RefusalRule = id.ID{}
	next.RefusalReason = ""
	return next
}

// Refuse records the rule that excluded this one.
func (c Candidate) Refuse(rule id.ID, reason string) (Candidate, error) {
	if rule.IsZero() {
		return c, ErrRuleRequired
	}
	if strings.TrimSpace(reason) == "" {
		return c, ErrReasonRequired
	}
	next := c
	next.Permitted = false
	next.RefusalRule = rule
	next.RefusalReason = reason
	return next, nil
}

// NotInScope is the refusal with NO RULE — nothing permitted it. `0010`'s
// "nothing is in scope until a rule says so" makes the empty rule set a refusal,
// and it is a separate constructor for the same reason [Invocation.NotInScope]
// is: a report that cites a rule must not cite one that does not exist.
func (c Candidate) NotInScope(reason string) (Candidate, error) {
	if strings.TrimSpace(reason) == "" {
		return c, ErrReasonRequired
	}
	next := c
	next.Permitted = false
	next.RefusalRule = id.ID{}
	next.RefusalReason = reason
	return next, nil
}

// Fold is the one place a candidate's value is normalised, so the schema
// constraint, the domain and the coverage join cannot disagree about what
// "the same host" means.
func Fold(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// PermittedValues is what goes into the argv, in the order the candidates were
// resolved. A refused one is absent — that is the whole point, and the reason
// this returns values rather than the caller filtering inline.
func PermittedValues(cs []Candidate) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		if c.Permitted {
			out = append(out, c.Value)
		}
	}
	return out
}

// FirstRefusal is what the INVOCATION records when every candidate was refused.
// The step did not spawn, and the row says which rule is answerable for that.
//
// It picks the first refusal in resolution order rather than merging them: an
// invocation cites ONE rule (`0010`'s three surfaces), the per-candidate rows
// carry the rest, and inventing a summary rule id would be a citation to
// something nobody wrote.
func FirstRefusal(cs []Candidate) (Candidate, bool) {
	for _, c := range cs {
		if !c.Permitted {
			return c, true
		}
	}
	return Candidate{}, false
}

// DistinctValues folds and de-duplicates the subjects a set of upstream
// observations carried, keeping a STABLE order so two runs of one chain build
// the same argv. Two feeders UNION — decisions/0039 §3 — and the union is here
// rather than in the query because the query answers per invocation.
func DistinctValues(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = Fold(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	// Sorted, so an argv is a function of WHAT was found and not of the order
	// two feeders happened to finish in. A diff between two runs of one chain
	// should be a difference in findings.
	sort.Strings(out)
	return out
}
