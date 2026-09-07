package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	MaxValueLength = 2000
	MaxLabelLength = 400
)

// Origin says where a fragment came from — decisions/0036, resolving the doubt
// `0009` recorded against itself.
type Origin uint8

const (
	// Observed: read out of an artifact. Carries first_seen and last_seen.
	Observed Origin = iota
	// Manual: typed in — a `/24` written into a scope rule is the case 0009
	// named. It carries NEITHER timestamp, and that is not a gap: nothing has
	// seen it, and rendering `–` there is CLAUDE.md's rule about an unmeasured
	// total applied to a date.
	Manual
)

func (o Origin) String() string {
	if o == Manual {
		return "manual"
	}
	return "observed"
}

func ParseOrigin(s string) (Origin, error) {
	switch s {
	case "observed":
		return Observed, nil
	case "manual":
		return Manual, nil
	default:
		return Observed, ErrOriginUnknown
	}
}

// Fragment is an identifier a source produced — or a person typed.
//
// **It IS the tuple `(workspace, kind, value)`**, deduplicated. That is not a
// denormalisation of an id: an observation joins to one on exactly those three
// columns, and `observation` carries no fragment id because a subscriber creates
// fragments AFTER the observations that imply them.
type Fragment struct {
	ID          id.ID
	WorkspaceID id.ID

	// Kind is the shared vocabulary — decisions/0034 — held as its SPELLING,
	// because `scope` owns the canonical copy and this package may not import
	// it. `root/vocabulary_test.go` is what stops the two drifting.
	Kind  string
	Value string

	Origin Origin

	// FirstSeen and LastSeen are zero for a manual fragment. For an observed
	// one they come from its observations, and LastSeen is what the asset
	// table's freshness column reads.
	FirstSeen time.Time
	LastSeen  time.Time

	// Observations is how many statements exist about it. It is a COUNT kept on
	// the row rather than computed, because the asset list renders it for every
	// row and a count per row is a query per row.
	Observations int

	Judgement Judgement

	// ReadAt and ReadBy are a HUMAN READ, and they are NOT the judgement —
	// decisions/0037. `0011` says READ BY YOU being a check is what keeps
	// `never read` and `no judgement` separable, and computing one from the
	// other would be that collapse: a person can open a fragment, read it, and
	// decline to rule, which is the commonest thing an analyst does.
	ReadAt time.Time
	ReadBy id.ID

	CreatedAt time.Time
}

// Read records that a person looked. **It does not touch the judgement** —
// reading is not ruling, and a caller wanting both makes two calls because they
// are two acts.
func (f Fragment) Read(by id.ID, at time.Time) (Fragment, error) {
	if by.IsZero() {
		return f, ErrJudgeRequired
	}
	if at.IsZero() {
		return f, ErrTimeRequired
	}
	next := f
	next.ReadAt = at
	next.ReadBy = by
	return next, nil
}

// HasBeenRead is the `READ BY YOU` coverage cell. It never goes stale — 0011
// requires the human check to report `stale` on no input at any age — so this is
// a boolean and the timestamp beside it is for the screen.
func (f Fragment) HasBeenRead() bool { return !f.ReadAt.IsZero() }

func NewFragment(newID, workspace id.ID, kind, value string, origin Origin, at time.Time) (Fragment, error) {
	if newID.IsZero() {
		return Fragment{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Fragment{}, ErrWorkspaceRequired
	}
	if at.IsZero() {
		return Fragment{}, ErrTimeRequired
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return Fragment{}, ErrKindUnknown
	}
	// The value is FOLDED to lower case, because `ACME.test` and `acme.test` are
	// one host and a fragment that admitted both would put the same asset on the
	// list twice. The Postgres unique index folds identically.
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > MaxValueLength {
		return Fragment{}, ErrValueRequired
	}
	out := Fragment{
		ID: newID, WorkspaceID: workspace, Kind: kind, Value: value,
		Origin: origin, CreatedAt: at,
	}
	if origin == Observed {
		out.FirstSeen, out.LastSeen = at, at
		out.Observations = 1
	}
	return out, nil
}

// Seen records another statement about this fragment. It moves LastSeen forward
// and NEVER backward: a re-extraction of a three-month-old artifact must not make
// a fragment look stale, and it must not make it look fresh either.
func (f Fragment) Seen(at time.Time, count int) Fragment {
	next := f
	next.Observations += count
	if next.Origin == Manual {
		// A manual fragment that is later observed becomes observed — somebody
		// typed it in and then a tool found it, which is a stronger claim than
		// either alone.
		next.Origin = Observed
		next.FirstSeen = at
	}
	if next.FirstSeen.IsZero() || at.Before(next.FirstSeen) {
		next.FirstSeen = at
	}
	if at.After(next.LastSeen) {
		next.LastSeen = at
	}
	return next
}

func (f Fragment) Judge(j Judgement) Fragment {
	next := f
	next.Judgement = j
	return next
}

// Entity is identity across sources and time. It is NOT a fragment — every
// attribution runs from an entity to one — which is why the canvas draws it
// apart from the nodes.
type Entity struct {
	ID          id.ID
	WorkspaceID id.ID

	Kind  string
	Label string

	// TargetID is set when this entity is a target's ROOT — decisions/0029 and
	// 0036. Zero for any other entity, and there is at most one root per target.
	TargetID id.ID

	Judgement Judgement

	CreatedAt time.Time
}

func NewEntity(newID, workspace id.ID, kind, label string, at time.Time) (Entity, error) {
	if newID.IsZero() {
		return Entity{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Entity{}, ErrWorkspaceRequired
	}
	if at.IsZero() {
		return Entity{}, ErrTimeRequired
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return Entity{}, ErrKindUnknown
	}
	label = strings.TrimSpace(label)
	if label == "" || len(label) > MaxLabelLength {
		return Entity{}, ErrLabelRequired
	}
	return Entity{
		ID: newID, WorkspaceID: workspace, Kind: kind, Label: label, CreatedAt: at,
	}, nil
}

// Root marks this entity as a target's root. It is a separate call rather than a
// constructor argument because the two are created at different moments — the
// subscriber makes the entity, and the target learns its id from an event.
func (e Entity) Root(target id.ID) Entity {
	next := e
	next.TargetID = target
	return next
}

func (e Entity) Judge(j Judgement) Entity {
	next := e
	next.Judgement = j
	return next
}

const (
	EventRootCreated  = "entity.root.created"
	EventFragmentSeen = "entity.fragment.seen"

	SubjectKind = "workspace"
)

// RootCreated is what `target` subscribes to in order to fill in the column
// `0029` declared and left zero. It carries the target id because a subscriber
// must not have to work out which target this was for — decisions/0013.
type RootCreated struct {
	EntityID    string `json:"entity_id"`
	WorkspaceID string `json:"workspace_id"`
	TargetID    string `json:"target_id"`
	Kind        string `json:"kind"`
	Label       string `json:"label"`
}

type FragmentSeen struct {
	WorkspaceID string `json:"workspace_id"`
	Fragments   int    `json:"fragments"`
	New         int    `json:"new"`
	Attributed  int    `json:"attributed"`
}

// Asset is a fragment IN A ROLE — decisions/0009 — read out of the view that
// carries the name. It is not a table and it is not constructible: the only way
// to get one is to read something that already satisfies the predicate.
type Asset struct {
	Fragment

	AttributionID id.ID
	Claimant      Claimant
	Basis         string
	RootEntityID  id.ID
	TargetID      id.ID
}
