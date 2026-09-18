package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired           = errors.New(errors.Invalid, "an identifier is required")
	ErrWorkspaceRequired    = errors.New(errors.Invalid, "a research record belongs to an investigation")
	ErrTimeRequired         = errors.New(errors.Invalid, "a research record needs a timestamp")
	ErrKindUnknown          = errors.New(errors.Invalid, "research record kind must be person, account, organisation, or place")
	ErrNameRequired         = errors.New(errors.Invalid, "a research record needs a name")
	ErrNameTooLong          = errors.New(errors.Invalid, "that research record name is too long")
	ErrDescriptionTooLong   = errors.New(errors.Invalid, "that research record description is too long")
	ErrObservationTooMany   = errors.New(errors.Invalid, "a research record can cite at most twelve observations")
	ErrDuplicateObservation = errors.New(errors.Invalid, "a research record cannot cite the same observation twice")
	ErrNotFound             = errors.New(errors.NotFound, "research record")
)
