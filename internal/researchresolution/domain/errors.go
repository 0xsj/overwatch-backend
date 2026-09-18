package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired         = errors.New(errors.Invalid, "a resolution requires an identifier")
	ErrWorkspaceRequired  = errors.New(errors.Invalid, "a resolution belongs to an investigation")
	ErrTimeRequired       = errors.New(errors.Invalid, "a resolution needs a timestamp")
	ErrRationaleRequired  = errors.New(errors.Invalid, "a resolution needs a rationale")
	ErrRationaleTooLong   = errors.New(errors.Invalid, "that resolution rationale is too long")
	ErrSameRecord         = errors.New(errors.Invalid, "a record cannot resolve to itself")
	ErrStateUnknown       = errors.New(errors.Invalid, "resolution state is unknown")
	ErrDecisionUnknown    = errors.New(errors.Invalid, "resolution decision is unknown")
	ErrStateConflict      = errors.New(errors.Conflict, "that resolution is no longer awaiting review")
	ErrObservationTooMany = errors.New(errors.Invalid, "the canonical record would have too many observations")
	ErrAlreadyResolved    = errors.New(errors.Conflict, "the alias record already has an active resolution")
	ErrNotFound           = errors.New(errors.NotFound, "research resolution")
)
