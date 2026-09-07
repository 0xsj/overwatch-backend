package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired    = errors.New(errors.Invalid, "an identifier is required")
	ErrTimeRequired  = errors.New(errors.Invalid, "an instant is required")
	ErrActorRequired = errors.New(errors.Invalid, "an entry with no actor records nothing")
	ErrScopeUnknown  = errors.New(errors.Invalid, "not a scope this system knows")
	// A read with no subject is a read of the whole ledger, which is never what
	// the caller meant and always what it would do.
	ErrSubjectRequired = errors.New(errors.Invalid, "a subject is required")
	ErrEntryGone       = errors.New(errors.NotFound, "entry")
)
