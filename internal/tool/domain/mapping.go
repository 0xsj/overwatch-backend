package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	MaxFieldLength      = 120
	MaxExpressionLength = 4000
)

// State is where a version sits. Exactly one version of a (tool, field) is live,
// and the partial unique index is what says so rather than this type.
type State uint8

const (
	StateDraft State = iota
	StateLive
	StateRetired
)

var stateNames = map[State]string{
	StateDraft: "draft", StateLive: "live", StateRetired: "retired",
}

func (s State) String() string {
	if n, ok := stateNames[s]; ok {
		return n
	}
	return "draft"
}

func ParseState(s string) (State, error) {
	for st, name := range stateNames {
		if name == s {
			return st, nil
		}
	}
	return StateDraft, ErrStatusUnknown
}

// Mapping is one version of "this tool's output, read as this field".
//
// **It is never edited.** An observation cites the version that produced it, so
// an expression that moved under a citation makes that observation's lineage a
// lie — the same reasoning decisions/0030 applies to a scope rule. Correcting a
// mapping is adding a version, and `correction` therefore gets no table of its
// own: it is a version whose author is a person.
type Mapping struct {
	ID     id.ID
	OrgID  id.ID
	ToolID id.ID

	// Field is what this produces — the name an observation is filed under.
	Field string

	// Expression is held as text and never interpreted here. What it means is
	// the reader's question, and giving this package an opinion about the
	// syntax would put a parser in the domain.
	Expression string

	// Version counts within (tool, field) and starts at 1. It is what a citation
	// carries, so it is assigned once and never reused.
	Version int

	State      State
	CreatedBy  id.ID
	CreatedAt  time.Time
	PromotedAt time.Time
	RetiredAt  time.Time
}

func NewMapping(newID, org, tool, by id.ID, field, expression string, version int, at time.Time) (Mapping, error) {
	if newID.IsZero() || tool.IsZero() || by.IsZero() {
		return Mapping{}, ErrIDRequired
	}
	if org.IsZero() {
		return Mapping{}, ErrOrgRequired
	}
	if at.IsZero() {
		return Mapping{}, ErrTimeRequired
	}
	field = strings.TrimSpace(field)
	if field == "" || len(field) > MaxFieldLength {
		return Mapping{}, ErrFieldRequired
	}
	expression = strings.TrimSpace(expression)
	if expression == "" || len(expression) > MaxExpressionLength {
		return Mapping{}, ErrExpressionRequired
	}
	if version < 1 {
		version = 1
	}
	return Mapping{
		ID: newID, OrgID: org, ToolID: tool, Field: field,
		Expression: expression, Version: version, State: StateDraft,
		CreatedBy: by, CreatedAt: at,
	}, nil
}

func (m Mapping) Live() bool { return m.State == StateLive }

// Promote makes this the version that runs. The caller retires the incumbent in
// the same transaction — the index refuses a second live row, so the order is
// retire then promote, and a crash between them leaves a field with no live
// mapping rather than two.
func (m Mapping) Promote(at time.Time) (Mapping, error) {
	if at.IsZero() {
		return m, ErrTimeRequired
	}
	if m.Live() {
		return m, ErrAlreadyLive
	}
	if m.State == StateRetired {
		return m, ErrMappingGone
	}
	next := m
	next.State = StateLive
	next.PromotedAt = at
	return next, nil
}

func (m Mapping) Retire(at time.Time) (Mapping, error) {
	if at.IsZero() {
		return m, ErrTimeRequired
	}
	if m.State == StateRetired {
		return m, ErrMappingGone
	}
	next := m
	next.State = StateRetired
	next.RetiredAt = at
	return next, nil
}
