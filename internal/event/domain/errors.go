package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired           = errors.New(errors.Invalid, "an identifier is required")
	ErrWorkspaceRequired    = errors.New(errors.Invalid, "an event belongs to an investigation")
	ErrTimeRequired         = errors.New(errors.Invalid, "an event time precision is required")
	ErrTimeUnknown          = errors.New(errors.Invalid, "not an event time precision this system knows")
	ErrTitleRequired        = errors.New(errors.Invalid, "an event needs a title")
	ErrTitleTooLong         = errors.New(errors.Invalid, "that event title is too long")
	ErrDescriptionTooLong   = errors.New(errors.Invalid, "that event description is too long")
	ErrTimeTextTooLong      = errors.New(errors.Invalid, "that reported time is too long")
	ErrLocationTooLong      = errors.New(errors.Invalid, "that event location is too long")
	ErrSortDateInvalid      = errors.New(errors.Invalid, "event ordering date must be YYYY-MM-DD")
	ErrObservationTooMany   = errors.New(errors.Invalid, "an event can cite at most eight observations")
	ErrDuplicateObservation = errors.New(errors.Invalid, "an event cannot cite the same observation twice")
	ErrNotFound             = errors.New(errors.NotFound, "event")
)
