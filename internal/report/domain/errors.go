package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired        = errors.New(errors.Invalid, "an identifier is required")
	ErrTimeRequired      = errors.New(errors.Invalid, "an instant is required")
	ErrWorkspaceRequired = errors.New(errors.Invalid, "a report belongs to an engagement")
	ErrTitleRequired     = errors.New(errors.Invalid, "a report needs a title")
	ErrPreparedByTooLong = errors.New(errors.Invalid, "that name is too long")
	ErrPeriodBackwards   = errors.New(errors.Invalid, "the engagement period ends before it starts")

	ErrSectionUnknown = errors.New(errors.Invalid, "not a section this system knows")

	// ErrSectionMandatory is the product's thesis, enforced. A report that omits
	// coverage implies a completeness nobody achieved.
	ErrSectionMandatory = errors.New(errors.Invalid,
		"coverage cannot be left out: a report that omits it implies a completeness nobody achieved")

	ErrRevisionNumber = errors.New(errors.Invalid, "a revision counts from one")
	ErrHashRequired   = errors.New(errors.Invalid, "an issued report is named by the hash of its bytes")
	ErrNoSections     = errors.New(errors.Invalid, "a report with no sections is not a document")

	ErrNotFound         = errors.New(errors.NotFound, "report")
	ErrRevisionNotFound = errors.New(errors.NotFound, "revision")
	ErrStaleWrite       = errors.New(errors.Conflict, "the record changed since it was read")
)
