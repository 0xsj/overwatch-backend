package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired        = errors.New(errors.Invalid, "an identifier is required")
	ErrTimeRequired      = errors.New(errors.Invalid, "an instant is required")
	ErrWorkspaceRequired = errors.New(errors.Invalid, "a note belongs to an engagement")
	ErrBodyRequired      = errors.New(errors.Invalid, "a note with no text is not a note")
	ErrSearchTooLong     = errors.New(errors.Invalid, "note search must be 200 characters or fewer")

	// ErrSubjectHalfSet is a note about nothing, spelled like a note about
	// something. The two columns move together or neither does.
	ErrSubjectHalfSet     = errors.New(errors.Invalid, "a subject is a kind and a value, and neither is optional")
	ErrSubjectTooLong     = errors.New(errors.Invalid, "that subject value is too long")
	ErrKindUnknown        = errors.New(errors.Invalid, "not a kind this system knows")
	ErrContextHalfSet     = errors.New(errors.Invalid, "a research context is a kind and an identifier, and neither is optional")
	ErrContextKindUnknown = errors.New(errors.Invalid, "not a research context this system knows")

	// ErrNotYours is the one write rule. Anybody on the engagement may READ a
	// note; only its author may change the words under their own name.
	ErrNotYours = errors.New(errors.Forbidden, "only the person who wrote a note may edit it")

	ErrNotFound = errors.New(errors.NotFound, "note")
)
