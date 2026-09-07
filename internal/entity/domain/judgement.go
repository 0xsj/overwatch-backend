package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Judgement is a VALUE OBJECT stored on the subject row — decisions/0009 — on
// `fragment` and on `entity`, as two identical column groups.
//
// **Not a polymorphic foreign key.** Postgres cannot reference two tables from
// one column, and the alternatives to duplication were all worse. The trade is a
// drift that a schema diff can catch, against an invariant that nothing could —
// and `root` holds a test asserting the two groups have the same shape.
//
// # Four values, and `finding` is not one
//
//	unopened   nobody has looked
//	triaged    somebody looked
//	watching   somebody is keeping an eye on it
//	dismissed  somebody closed the question, WITH A REASON
//
// A finding is a claim with its own lifecycle (0004) and it hangs off a fragment
// without being a state of one — so a fragment may be `watching` AND carry an
// open finding at the same time, which is the case a fifth enum value could not
// have represented.
type Judgement struct {
	State JudgementState

	// By is ABSENT while unopened, and is an account. Nothing rules on
	// something automatically: a judgement is by definition a person having
	// looked, which is what makes "never read" answerable.
	By id.ID
	At time.Time

	// Reason is REQUIRED when dismissed and optional otherwise. The asymmetry
	// is 0009's and it is deliberate: the cost is highest where somebody closed
	// a question, and lowest on the hot path.
	Reason string
}

type JudgementState uint8

const (
	Unopened JudgementState = iota
	Triaged
	Watching
	Dismissed
)

var judgementNames = map[JudgementState]string{
	Unopened: "unopened", Triaged: "triaged", Watching: "watching", Dismissed: "dismissed",
}

func (s JudgementState) String() string {
	if n, ok := judgementNames[s]; ok {
		return n
	}
	return "unopened"
}

// ParseJudgement takes "" as `unopened`, because that is what an unset column
// holds and a row that has never been ruled on is exactly unopened.
func ParseJudgement(s string) (JudgementState, error) {
	if s == "" {
		return Unopened, nil
	}
	for k, name := range judgementNames {
		if name == s {
			return k, nil
		}
	}
	return Unopened, ErrStateUnknown
}

// Opened is what "what have I never looked at" asks — the question PRODUCT.md
// says is not answerable in any competitor.
func (j Judgement) Opened() bool { return j.State != Unopened }

// Rule builds a judgement and refuses every shape 0009 lists. It is one
// constructor for both tables, which is how the two column groups stay the same
// thing even while they are two sets of columns.
func Rule(state JudgementState, by id.ID, reason string, at time.Time) (Judgement, error) {
	reason = strings.TrimSpace(reason)
	if state == Unopened {
		// `unopened` with a `by` set is refused: somebody ruling on it is what
		// stops it being unopened.
		if !by.IsZero() || !at.IsZero() || reason != "" {
			return Judgement{}, ErrJudgeOnUnopened
		}
		return Judgement{State: Unopened}, nil
	}
	if by.IsZero() {
		return Judgement{}, ErrJudgeRequired
	}
	if at.IsZero() {
		return Judgement{}, ErrJudgeRequired
	}
	if state == Dismissed && reason == "" {
		return Judgement{}, ErrDismissalReason
	}
	return Judgement{State: state, By: by, At: at, Reason: reason}, nil
}

// Valid is the same rule stated over a loaded row, so a store and a constraint
// cannot disagree about what a judgement is.
func (j Judgement) Valid() error {
	switch {
	case j.State == Unopened && (!j.By.IsZero() || !j.At.IsZero()):
		return ErrJudgeOnUnopened
	case j.State != Unopened && (j.By.IsZero() || j.At.IsZero()):
		return ErrJudgeRequired
	case j.State == Dismissed && strings.TrimSpace(j.Reason) == "":
		return ErrDismissalReason
	}
	return nil
}

const EventJudgementSet = "entity.judgement.set"

type JudgementSet struct {
	WorkspaceID string `json:"workspace_id"`
	SubjectKind string `json:"subject_kind"`
	SubjectID   string `json:"subject_id"`
	From        string `json:"from"`
	To          string `json:"to"`
	Reason      string `json:"reason,omitempty"`
}
