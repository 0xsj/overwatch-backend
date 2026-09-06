package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired    = errors.New(errors.Invalid, "an identifier is required")
	ErrTimeRequired  = errors.New(errors.Invalid, "an instant is required")
	ErrActorRequired = errors.New(errors.Invalid, "an entry with no actor records nothing")
	ErrScopeUnknown  = errors.New(errors.Invalid, "not a scope this system knows")
	ErrEntryGone     = errors.New(errors.NotFound, "entry")
)
