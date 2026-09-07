package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired        = errors.New(errors.Invalid, "an identifier is required")
	ErrTimeRequired      = errors.New(errors.Invalid, "an instant is required")
	ErrWorkspaceRequired = errors.New(errors.Invalid, "a target belongs to a workspace")

	ErrNameRequired = errors.New(errors.Invalid, "a target needs a name")
	ErrNameTooLong  = errors.New(errors.Invalid, "the name is too long")
	ErrKindUnknown  = errors.New(errors.Invalid, "not a target kind this system knows")

	ErrNotFound   = errors.New(errors.NotFound, "target")
	ErrNameTaken  = errors.New(errors.Conflict, "that name is already used in this engagement")
	ErrArchived   = errors.New(errors.Conflict, "the target is archived")
	ErrStaleWrite = errors.New(errors.Conflict, "the record changed since it was read")
)
