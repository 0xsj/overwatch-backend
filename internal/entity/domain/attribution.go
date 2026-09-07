package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Claimant is who PROPOSED an attribution — decisions/0008. It is NEVER absent
// and NEVER rewritten: a shape that overwrites it retains only the claims the
// model got wrong.
type Claimant uint8

const (
	ByRule Claimant = iota
	ByModel
	ByHuman
)

var claimantNames = map[Claimant]string{ByRule: "rule", ByModel: "model", ByHuman: "human"}

func (c Claimant) String() string {
	if n, ok := claimantNames[c]; ok {
		return n
	}
	return "rule"
}

func ParseClaimant(s string) (Claimant, error) {
	for k, name := range claimantNames {
		if name == s {
			return k, nil
		}
	}
	return ByRule, ErrClaimantUnknown
}

// BornAccepted says whether creating this claim and agreeing with it are the
// same act — decisions/0008 for a human, 0036 extending it to a rule.
//
// **A model is the only claimant that proposes.** A human writing an attribution
// is agreeing with it; a scope claim rule that matched has already said this is
// in the engagement, and asking somebody to then agree with a rule they wrote is
// the `reviewed vs in scope` pair collapsing backwards.
func (c Claimant) BornAccepted() bool { return c != ByModel }

// CarriesConfidence is 0003's disjointness in one place. Only machines carry
// confidence, and a rule is not a machine in that sense: its assignment is a
// CATEGORY, and storing 1.0 destroys the distinction permanently.
func (c Claimant) CarriesConfidence() bool { return c == ByModel }

type ClaimState uint8

const (
	Proposed ClaimState = iota
	Accepted
	Rejected
)

var claimStateNames = map[ClaimState]string{
	Proposed: "proposed", Accepted: "accepted", Rejected: "rejected",
}

func (s ClaimState) String() string {
	if n, ok := claimStateNames[s]; ok {
		return n
	}
	return "proposed"
}

func ParseClaimState(s string) (ClaimState, error) {
	for k, name := range claimStateNames {
		if name == s {
			return k, nil
		}
	}
	return Proposed, ErrStateUnknown
}

// Attribution runs ENTITY → FRAGMENT and IS a claim — decisions/0003. It is the
// only edge anybody accepts or rejects, and it is the thing an asset needs.
//
// Its shape is DISJOINT from a derivation's rather than being one shape with
// optional fields. There is no derivation in this package yet (0036) and this
// type is the reason the distinction survives: nothing here can be built without
// a claimant.
type Attribution struct {
	ID          id.ID
	WorkspaceID id.ID

	EntityID   id.ID
	FragmentID id.ID

	// Claimant is who said it FIRST, and it is never rewritten.
	Claimant Claimant
	// ClaimantRef is the account when the claimant is human, and the RULE ID
	// when it is a rule. Zero for a model, which has no id in this system yet.
	ClaimantRef id.ID

	// Confidence is present iff the claimant is a model — 0003 and 0008.
	Confidence    float64
	HasConfidence bool

	// Basis is why. For a rule it names the scope rule; for a model it is the
	// sentence the model gave; for a human it is what they typed.
	Basis string

	State ClaimState

	// DecidedAt is set iff the state is not proposed.
	DecidedAt time.Time
	// DecidedBy is set iff a PERSON decided — decisions/0036 amending 0008.
	// A rule deciding is still a decision; it just has no account behind it,
	// which is how `run.started_by` already spells "a schedule did this".
	DecidedBy   id.ID
	DecidedNote string

	CreatedAt time.Time
}

// Propose builds an attribution in the state its claimant is born in.
//
// A rule or a human produces an ACCEPTED one, decided at `at` — a rule with no
// decider, a human deciding themselves. A model produces a PROPOSED one.
func Propose(newID, workspace, entity, fragment id.ID, claimant Claimant,
	ref id.ID, confidence float64, hasConfidence bool, basis string, at time.Time) (Attribution, error) {
	if newID.IsZero() || entity.IsZero() || fragment.IsZero() {
		return Attribution{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Attribution{}, ErrWorkspaceRequired
	}
	if at.IsZero() {
		return Attribution{}, ErrTimeRequired
	}
	basis = strings.TrimSpace(basis)
	if basis == "" {
		return Attribution{}, ErrBasisRequired
	}
	switch {
	case claimant.CarriesConfidence() && !hasConfidence:
		return Attribution{}, ErrConfidenceMissing
	case !claimant.CarriesConfidence() && hasConfidence:
		return Attribution{}, ErrConfidenceOnRule
	case hasConfidence && (confidence < 0 || confidence > 1):
		return Attribution{}, ErrConfidenceRange
	}

	out := Attribution{
		ID: newID, WorkspaceID: workspace, EntityID: entity, FragmentID: fragment,
		Claimant: claimant, ClaimantRef: ref, Basis: basis,
		Confidence: confidence, HasConfidence: hasConfidence,
		State: Proposed, CreatedAt: at,
	}
	if !claimant.BornAccepted() {
		return out, nil
	}
	out.State = Accepted
	out.DecidedAt = at
	if claimant == ByHuman {
		// The claimant IS the decider, and the screen's copy is "accepted by
		// you" — which is what happened. A rule leaves DecidedBy zero.
		out.DecidedBy = ref
	}
	return out, nil
}

// Decide is a PERSON ruling on a proposed claim. It never touches the claimant.
func (a Attribution) Decide(state ClaimState, by id.ID, note string, at time.Time) (Attribution, error) {
	if at.IsZero() {
		return a, ErrTimeRequired
	}
	if by.IsZero() {
		return a, ErrDeciderRequired
	}
	if state == Proposed {
		return a, ErrStateUnknown
	}
	if a.State != Proposed {
		return a, ErrAlreadyDecided
	}
	next := a
	next.State = state
	next.DecidedAt = at
	next.DecidedBy = by
	next.DecidedNote = strings.TrimSpace(note)
	return next, nil
}

// Valid states 0008's invariant as amended by 0036, over a loaded row — so the
// store and the constraint cannot disagree about it.
func (a Attribution) Valid() error {
	switch {
	case a.Claimant.CarriesConfidence() != a.HasConfidence:
		if a.HasConfidence {
			return ErrConfidenceOnRule
		}
		return ErrConfidenceMissing
	case a.State == Proposed && (!a.DecidedAt.IsZero() || !a.DecidedBy.IsZero()):
		return ErrDecidedOnProposed
	case a.State != Proposed && a.DecidedAt.IsZero():
		return ErrUndecided
	}
	return nil
}

// Attributed is what 0009's asset predicate asks of the edge.
func (a Attribution) Attributed() bool { return a.State == Accepted }

const (
	EventProposed = "entity.attribution.proposed"
	EventAccepted = "entity.attribution.accepted"
	EventRejected = "entity.attribution.rejected"
)

// Claimed carries the CLAIMANT and the state separately, because "a model
// proposed this" and "somebody accepted it" are the two facts a subscriber
// cannot reconstruct from one another — 0008's whole point.
type Claimed struct {
	AttributionID string `json:"attribution_id"`
	WorkspaceID   string `json:"workspace_id"`
	EntityID      string `json:"entity_id"`
	FragmentID    string `json:"fragment_id"`
	Claimant      string `json:"claimant"`
	State         string `json:"state"`
	Basis         string `json:"basis"`
	// DecidedBy is absent when a RULE decided. Not null — absent, because "no
	// person" is a fact and a null reads as unknown.
	DecidedBy string `json:"decided_by,omitempty"`
}

// Claim is the CLAIM gate's answer in entity's own vocabulary — 0010's second
// gate, asked through a port because `scope` is a peer.
//
// A `Rule` of zero with `Covered` false is "nothing said this is in the
// engagement", which is 0010's default and is a different fact from a rule
// having excluded it. Both produce a fragment and neither produces an asset.
type Claim struct {
	Covered bool
	Rule    id.ID
	Basis   string
}
