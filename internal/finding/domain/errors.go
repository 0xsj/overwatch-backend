package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired        = errors.New(errors.Invalid, "an identifier is required")
	ErrTimeRequired      = errors.New(errors.Invalid, "an instant is required")
	ErrWorkspaceRequired = errors.New(errors.Invalid, "a finding belongs to an engagement")

	ErrStateUnknown    = errors.New(errors.Invalid, "not a state this system knows")
	ErrSeverityUnknown = errors.New(errors.Invalid, "not a severity this system knows")
	ErrClaimantUnknown = errors.New(errors.Invalid, "a claimant is a rule, a model or a human")
	ErrSubjectRequired = errors.New(errors.Invalid, "a finding is on a fragment, which is a kind and a value")
	ErrFieldRequired   = errors.New(errors.Invalid, "a detail is named by its mapping")
	ErrValueRequired   = errors.New(errors.Invalid, "a detail with no value is not a reading")
	ErrReasonTooLong   = errors.New(errors.Invalid, "that reason is too long")

	// ErrSignatureRequired is half a finding's IDENTITY missing — decisions/0041
	// §1. Without it every match on one fragment collapses into a single
	// finding whose identity is a lie.
	ErrSignatureRequired = errors.New(errors.Invalid, "a finding names what the tool called this class of problem")

	// ErrUnsourced is 0003's rule one noun over: a finding this system cannot
	// point back at an invocation and an artifact would arrive looking
	// trustworthy.
	ErrUnsourced = errors.New(errors.Invalid, "a finding names the invocation and the artifact it was read from")

	// The 0004 invariants.
	ErrConfidenceOnRule  = errors.New(errors.Invalid, "a rule's assessment is a category, not a probability")
	ErrConfidenceOnHuman = errors.New(errors.Invalid, "only machines carry confidence")
	ErrConfidenceMissing = errors.New(errors.Invalid, "a model's assessment carries a confidence")
	ErrConfidenceRange   = errors.New(errors.Invalid, "a confidence is between 0 and 1")
	ErrBasisRequired     = errors.New(errors.Invalid, "replacing somebody else's assessment says why")
	ErrActorRequired     = errors.New(errors.Invalid, "a person deciding names themselves")

	// ErrReasonRequired is the asymmetry 0041 §3 draws between resolving and
	// dismissing. A fix needs no argument; deciding a real problem does not
	// matter is a disagreement, and one with no reason records that somebody
	// disagreed without saying why they were right.
	ErrReasonRequired = errors.New(errors.Invalid, "dismissing a finding says why it does not matter")

	ErrNotFound     = errors.New(errors.NotFound, "finding")
	ErrAlreadyThere = errors.New(errors.Conflict, "the finding is already in that state")
	ErrStaleWrite   = errors.New(errors.Conflict, "the record changed since it was read")
)
