package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired        = errors.New(errors.Invalid, "an identifier is required")
	ErrTimeRequired      = errors.New(errors.Invalid, "an instant is required")
	ErrWorkspaceRequired = errors.New(errors.Invalid, "an observation belongs to an engagement")

	ErrSubjectRequired = errors.New(errors.Invalid, "an observation is about something")
	ErrFieldRequired   = errors.New(errors.Invalid, "an observation names the field it read")
	ErrValueRequired   = errors.New(errors.Invalid, "an observation carries what the source said")
	ErrPathRequired    = errors.New(errors.Invalid, "a path is required")

	// ErrPathMalformed is the parse failure, and it is deliberately not a
	// silent no-match: a mapping whose expression is not a path at all is a
	// mistake somebody can fix, while a path that matches nothing this time is
	// the tool not having said that.
	ErrPathMalformed = errors.New(errors.Invalid, "not a path this system can read")

	// ErrNoSubjectMapping is the one place extraction REFUSES rather than
	// records — decisions/0035. A tool with no mapping for its own produces
	// kind cannot say what any of its values are about.
	ErrNoSubjectMapping = errors.New(errors.Invalid,
		"this tool has no live mapping for the kind it produces, so nothing it "+
			"emits can be attached to a subject")

	ErrNotFound = errors.New(errors.NotFound, "observation")
)
