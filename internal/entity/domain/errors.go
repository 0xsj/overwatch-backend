package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired        = errors.New(errors.Invalid, "an identifier is required")
	ErrTimeRequired      = errors.New(errors.Invalid, "an instant is required")
	ErrWorkspaceRequired = errors.New(errors.Invalid, "this belongs to an engagement")

	ErrKindUnknown   = errors.New(errors.Invalid, "not a kind this system knows")
	ErrValueRequired = errors.New(errors.Invalid, "a fragment is a kind and a value")
	ErrLabelRequired = errors.New(errors.Invalid, "an entity needs a label")
	ErrOriginUnknown = errors.New(errors.Invalid, "not an origin this system knows")

	ErrClaimantUnknown = errors.New(errors.Invalid, "a claimant is a rule, a model or a human")
	ErrStateUnknown    = errors.New(errors.Invalid, "not a state this system knows")
	ErrBasisRequired   = errors.New(errors.Invalid, "an attribution says on what basis")

	// ErrConfidenceOnRule is decisions/0003's disjointness, enforced. A rule's
	// assignment is a category and not a probability, and storing 1.0 destroys
	// the distinction permanently.
	ErrConfidenceOnRule  = errors.New(errors.Invalid, "a rule's assignment is a category, not a probability")
	ErrConfidenceMissing = errors.New(errors.Invalid, "a model's claim carries a confidence")
	ErrConfidenceRange   = errors.New(errors.Invalid, "a confidence is between 0 and 1")

	// The 0008 invariant, as amended by 0036.
	ErrDecidedOnProposed = errors.New(errors.Invalid, "an undecided attribution names nobody and no time")
	ErrUndecided         = errors.New(errors.Invalid, "a decided attribution says when it was decided")
	ErrDeciderRequired   = errors.New(errors.Invalid, "a person deciding names themselves")

	// The judgement rules — 0009, verbatim.
	ErrJudgeOnUnopened = errors.New(errors.Invalid, "nobody has opened it, so nobody has ruled")
	ErrJudgeRequired   = errors.New(errors.Invalid, "a judgement names who made it and when")
	ErrDismissalReason = errors.New(errors.Invalid, "dismissing needs a reason — a dismissal with no reason reads as `never looked at` in six months")

	ErrNotFound            = errors.New(errors.NotFound, "entity")
	ErrFragmentNotFound    = errors.New(errors.NotFound, "fragment")
	ErrAttributionNotFound = errors.New(errors.NotFound, "attribution")
	ErrAlreadyDecided      = errors.New(errors.Conflict, "that attribution has already been ruled on")
)
