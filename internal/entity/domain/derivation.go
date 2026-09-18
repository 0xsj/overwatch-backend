package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Derivation is `0003`'s second edge kind: **fragment → fragment, "read out
// of", and NOT a claim.**
//
//	attribution   root entity -> fragment.  claimant, confidence, state
//	derivation    fragment -> fragment.     NONE of those three
//
// `0003` is explicit about why the two are separate shapes rather than one with
// optional fields:
//
//	> It carries no claimant, no confidence and no state, because nobody claimed
//	> anything: one source said so and the bytes are on disk. There is nothing on
//	> it to agree with.
//
// **A derivation this package cannot source does not exist.** `Invocation` and
// `Artifact` are refused when zero, because `0003` says an edge without them is
// a similarity edge wearing a costume — and `CLAUDE.md` §out_of_scope bans those
// outright. The ban is held here and by a not-null constraint, in two places, on
// purpose.
type Derivation struct {
	ID          id.ID
	WorkspaceID id.ID

	// From is what it was read OUT OF; To is what was read.
	From id.ID
	To   id.ID

	// Label names the ACT — `SAN entry`, `commit author`, `input`. It is the
	// MAPPING'S FIELD NAME, because for a `derived_from` mapping the thing the
	// field names IS the act, and a second column would be a second place to
	// write the same word.
	Label string

	Invocation id.ID
	Artifact   id.ID
	Mapping    id.ID

	CreatedAt time.Time
}

// NewDerivation refuses every way an unsourceable edge could be built. There is
// deliberately no constructor that omits the invocation or the artifact.
func NewDerivation(newID, workspace, from, to id.ID, label string,
	invocation, artifact, mapping id.ID, at time.Time) (Derivation, error) {
	if newID.IsZero() || from.IsZero() || to.IsZero() {
		return Derivation{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Derivation{}, ErrWorkspaceRequired
	}
	if invocation.IsZero() || artifact.IsZero() {
		// 0003, and this is the line that stops a similarity edge existing.
		return Derivation{}, ErrUnsourced
	}
	if mapping.IsZero() {
		return Derivation{}, ErrIDRequired
	}
	if at.IsZero() {
		return Derivation{}, ErrTimeRequired
	}
	if from == to {
		// A fragment is not read out of itself. No tool does this on purpose;
		// a mapping pointed at the wrong path does it on every record.
		return Derivation{}, ErrDerivationToSelf
	}
	label = strings.TrimSpace(label)
	if label == "" || len(label) > MaxLabelLength {
		return Derivation{}, ErrLabelRequired
	}
	return Derivation{
		ID: newID, WorkspaceID: workspace, From: from, To: to, Label: label,
		Invocation: invocation, Artifact: artifact, Mapping: mapping,
		CreatedAt: at,
	}, nil
}

// Unresolved is a provenance the tool named and nothing could be found for —
// decisions/0040 §5.
//
// **It is a record, not a failure**, the same way [Unmapped] is in
// `observation`: the alternative is dropping it, and then a derivation that does
// not exist and one that could not be resolved become the same thing — which is
// `CLAUDE.md`'s `never checked vs found nothing` wearing different clothes.
type Unresolved struct {
	ID          id.ID
	WorkspaceID id.ID
	Invocation  id.ID
	Mapping     id.ID

	// To is the half that DID resolve.
	To id.ID

	// FromKind is the tool's `consumes`, supplied by the composition root.
	FromKind string
	// FromValue is folded — it is what was looked up. FromRaw is what the tool
	// actually wrote. BOTH, because a fold mismatch is one of the two reasons
	// this row exists and keeping only the folded form would hide it.
	FromValue string
	FromRaw   string

	Label     string
	CreatedAt time.Time
}

func NewUnresolved(newID, workspace, invocation, mapping, to id.ID,
	fromKind, fromRaw, label string, at time.Time) (Unresolved, error) {
	if newID.IsZero() || invocation.IsZero() || mapping.IsZero() || to.IsZero() {
		return Unresolved{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Unresolved{}, ErrWorkspaceRequired
	}
	if at.IsZero() {
		return Unresolved{}, ErrTimeRequired
	}
	fromKind = strings.TrimSpace(fromKind)
	folded := Fold(fromRaw)
	if fromKind == "" || folded == "" {
		return Unresolved{}, ErrSubjectRequired
	}
	label = strings.TrimSpace(label)
	if label == "" || len(label) > MaxLabelLength {
		return Unresolved{}, ErrLabelRequired
	}
	return Unresolved{
		ID: newID, WorkspaceID: workspace, Invocation: invocation,
		Mapping: mapping, To: to, FromKind: fromKind, FromValue: folded,
		FromRaw: strings.TrimSpace(fromRaw), Label: label, CreatedAt: at,
	}, nil
}

const (
	EventDerivationDrawn = "entity.derivation.drawn"
)

// Drawn is what one delivery's provenance pass did. The UNRESOLVED count is in
// the envelope beside the drawn one, because a subscriber cannot compute it —
// decisions/0013 — and "httpx cited 37 inputs and 2 did not resolve" is the
// whole reason 0040 §5 records them rather than dropping them.
type Drawn struct {
	WorkspaceID  string `json:"workspace_id"`
	InvocationID string `json:"invocation_id"`
	Drawn        int    `json:"drawn"`
	Unresolved   int    `json:"unresolved"`
}
