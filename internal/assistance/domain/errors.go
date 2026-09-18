package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired          = errors.New(errors.Invalid, "an identifier is required")
	ErrWorkspaceRequired   = errors.New(errors.Invalid, "assistance belongs to an investigation")
	ErrCaptureRequired     = errors.New(errors.Invalid, "assistance requires one retained capture")
	ErrTextCaptureRequired = errors.New(errors.Invalid, "assistance requires a retained text capture")
	ErrProviderRequired    = errors.New(errors.Invalid, "an assistance provider is required")
	ErrMethodRequired      = errors.New(errors.Invalid, "an assistance method is required")
	ErrProposalRequired    = errors.New(errors.Invalid, "a proposal requires a statement and exact passage")
	ErrProposalTooLong     = errors.New(errors.Invalid, "an assistance proposal is too long")
	ErrStateUnknown        = errors.New(errors.Invalid, "not an assistance proposal state this system knows")
	ErrDecisionRequired    = errors.New(errors.Invalid, "a proposal review needs a decision")
	ErrDecisionUnknown     = errors.New(errors.Invalid, "not a proposal review decision this system knows")
	ErrReviewRequired      = errors.New(errors.Invalid, "an accepted proposal needs reviewed text")
	ErrCitation            = errors.New(errors.Invalid, "a proposal passage must match the retained capture exactly")
	ErrCandidateInvalid    = errors.New(errors.Invalid, "an assistance candidate must have a supported type and name")
	ErrSynthesisRequired   = errors.New(errors.Invalid, "a synthesis needs at least one observation")
	ErrSynthesisTooLarge   = errors.New(errors.Invalid, "a synthesis can use at most six observations")
	ErrSynthesisOutput     = errors.New(errors.Invalid, "a synthesis output is too large or invalid")
	ErrNotFound            = errors.New(errors.NotFound, "assistance operation or proposal")
)
