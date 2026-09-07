package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired        = errors.New(errors.Invalid, "an identifier is required")
	ErrTimeRequired      = errors.New(errors.Invalid, "an instant is required")
	ErrWorkspaceRequired = errors.New(errors.Invalid, "a scope rule belongs to a workspace")
	ErrTargetRequired    = errors.New(errors.Invalid, "a scope rule belongs to a target")

	ErrPatternRequired  = errors.New(errors.Invalid, "a scope rule needs a pattern")
	ErrPatternTooLong   = errors.New(errors.Invalid, "the pattern is too long")
	ErrGateUnknown      = errors.New(errors.Invalid, "not a gate this system knows")
	ErrPolarityUnknown  = errors.New(errors.Invalid, "not a polarity this system knows")
	ErrKindUnknown      = errors.New(errors.Invalid, "not a subject kind this system knows")
	ErrIntensityUnknown = errors.New(errors.Invalid, "not a tool intensity this system knows")

	ErrKindsRequired = errors.New(errors.Invalid, "a scope rule names the kinds it can match")

	// decisions/0010: the two gates take disjoint kinds, and the qualifier
	// exists because "a range in scope for passive collection is not thereby in
	// scope for a loud scan" — a statement about PROCESSES.
	ErrKindWrongGate = errors.New(errors.Invalid,
		"that subject kind cannot be matched on that gate")
	ErrToolsOnClaim = errors.New(errors.Invalid,
		"a claim rule carries no tool intensities — there are no processes on that gate")

	ErrNotFound   = errors.New(errors.NotFound, "scope rule")
	ErrSuperseded = errors.New(errors.Conflict, "that rule has already been superseded")
)
