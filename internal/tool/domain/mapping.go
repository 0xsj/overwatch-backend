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

// Role is what a mapping is FOR — decisions/0040. Three, and one of them draws
// an edge in the entity graph.
//
//	subject       what every other reading in this record is ABOUT
//	attribute     a value about that subject.        THE DEFAULT
//	derived_from  the value this record was READ OUT OF. Draws a derivation
//
// **It is a declaration and it replaces a name match.** `0035` resolved a
// record's subject by SPELLING — the tool's `produces` kind written as a field
// name — which works and works by luck. Doing the same trick a second time for
// provenance would let somebody create edges in the entity graph by typing a
// field called `derived_from` for an unrelated reason, and that failure is
// silent where the spelling one is loud.
type Role uint8

const (
	// RoleAttribute is the zero value and the default, so a mapping nobody
	// thought about is the harmless one.
	RoleAttribute Role = iota
	RoleSubject
	RoleDerivedFrom

	// The two a FINDING needs — decisions/0041 §2. `signature` is what the tool
	// calls this class of problem and is half the finding's identity;
	// `severity` is the tool's own assessment, and it arrives as a `rule`
	// claimant with NO confidence because a template asserting `high` is a
	// category rather than a probability — 0004.
	RoleSignature
	RoleSeverity
)

var roleNames = map[Role]string{
	RoleAttribute: "attribute", RoleSubject: "subject", RoleDerivedFrom: "derived_from",
	RoleSignature: "signature", RoleSeverity: "severity",
}

func (r Role) String() string {
	if n, ok := roleNames[r]; ok {
		return n
	}
	return "attribute"
}

func ParseRole(s string) (Role, error) {
	for role, name := range roleNames {
		if name == s {
			return role, nil
		}
	}
	return RoleAttribute, ErrRoleUnknown
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

	// Role is what this mapping is for — 0040. At most one LIVE `subject` and
	// one LIVE `derived_from` per tool, held by two partial unique indexes; the
	// predicate is the decision, and a draft is unconstrained because it is
	// something somebody is still editing.
	//
	// For a `derived_from` mapping the FIELD IS THE LABEL the derivation
	// carries — `0003` requires one naming the act, and a separate column would
	// be a second place to write the same word.
	Role Role

	State      State
	CreatedBy  id.ID
	CreatedAt  time.Time
	PromotedAt time.Time
	RetiredAt  time.Time
}

// NewMapping refuses a `derived_from` role on a tool that CONSUMES NOTHING —
// decisions/0040 §3. A source tool is seeded from the target, so there is no
// upstream fragment for an edge to point back at and the mapping could never
// fire. `consumes` is passed rather than looked up because this package's domain
// takes no ports, and a mapping that can never fire is one somebody believes is
// working.
func NewMapping(newID, org, tool, by id.ID, field, expression string, version int,
	role Role, consumes Feed, at time.Time) (Mapping, error) {
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
	if role == RoleDerivedFrom && consumes == FeedNone {
		return Mapping{}, ErrSourceToolHasNoInput
	}
	return Mapping{
		ID: newID, OrgID: org, ToolID: tool, Field: field,
		Expression: expression, Version: version, State: StateDraft,
		Role: role, CreatedBy: by, CreatedAt: at,
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
