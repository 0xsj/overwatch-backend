package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired           = errors.New(errors.Invalid, "an identifier is required")
	ErrTimeRequired         = errors.New(errors.Invalid, "an instant is required")
	ErrWorkspaceRequired    = errors.New(errors.Invalid, "a question belongs to an investigation")
	ErrQuestionRequired     = errors.New(errors.Invalid, "an open question needs text")
	ErrQuestionTooLong      = errors.New(errors.Invalid, "that question is too long")
	ErrContextTooLong       = errors.New(errors.Invalid, "that question context is too long")
	ErrResolutionRequired   = errors.New(errors.Invalid, "an answer or dismissal needs a resolution")
	ErrResolutionNotAllowed = errors.New(errors.Invalid, "an open question cannot carry a resolution")
	ErrResolutionTooLong    = errors.New(errors.Invalid, "that resolution is too long")
	ErrStateUnknown         = errors.New(errors.Invalid, "not a question state this system knows")
	ErrTooManyObservations  = errors.New(errors.Invalid, "a question can cite at most eight observations")
	ErrDuplicateObservation = errors.New(errors.Invalid, "a question cannot cite the same observation twice")
	ErrNotFound             = errors.New(errors.NotFound, "question")
)
