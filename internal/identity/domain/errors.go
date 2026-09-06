package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrEmailRequired       = errors.New(errors.Invalid, "an email address is required")
	ErrEmailTooLong        = errors.New(errors.Invalid, "the email address is too long")
	ErrEmailNotAddressable = errors.New(errors.Invalid, "the email address is not addressable")
	ErrEmailNoDomain       = errors.New(errors.Invalid, "the email address has no domain")

	ErrNameRequired = errors.New(errors.Invalid, "a name is required")
	ErrNameTooLong  = errors.New(errors.Invalid, "the name is too long")

	ErrIDRequired    = errors.New(errors.Invalid, "an identifier is required")
	ErrTimeRequired  = errors.New(errors.Invalid, "an instant is required")
	ErrStatusUnknown = errors.New(errors.Invalid, "not a status this system knows")
	ErrKindUnknown   = errors.New(errors.Invalid, "not a credential kind this system knows")

	ErrAccountNotFound = errors.New(errors.NotFound, "account")
	ErrAccountExists   = errors.New(errors.Conflict, "an account with that email already exists")
	ErrAccountArchived = errors.New(errors.Conflict, "the account is archived")
	ErrAlreadyActive   = errors.New(errors.Conflict, "the account is already active")

	ErrHashRequired    = errors.New(errors.Invalid, "a credential with no hash would authenticate anybody")
	ErrKeyNeedsName    = errors.New(errors.Invalid, "an api key with no name is an api key nobody can revoke")
	ErrAlreadyRevoked  = errors.New(errors.Conflict, "already revoked")
	ErrCredentialGone  = errors.New(errors.NotFound, "credential")
	ErrExpiryInThePast = errors.New(errors.Invalid, "the expiry is before the moment of issue")

	ErrSessionGone = errors.New(errors.NotFound, "session")

	ErrStaleWrite = errors.New(errors.Conflict, "the record changed since it was read")
)
