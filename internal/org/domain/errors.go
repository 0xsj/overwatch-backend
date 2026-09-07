package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired   = errors.New(errors.Invalid, "an identifier is required")
	ErrTimeRequired = errors.New(errors.Invalid, "an instant is required")

	ErrNameRequired = errors.New(errors.Invalid, "an organisation needs a name")
	ErrNameTooLong  = errors.New(errors.Invalid, "the name is too long")

	ErrRoleUnknown   = errors.New(errors.Invalid, "not a role this system knows")
	ErrStatusUnknown = errors.New(errors.Invalid, "not a status this system knows")

	ErrOrgNotFound    = errors.New(errors.NotFound, "organisation")
	ErrMemberNotFound = errors.New(errors.NotFound, "member")
	ErrMemberExists   = errors.New(errors.Conflict, "that account is already a member")
	ErrMemberArchived = errors.New(errors.Conflict, "the member is archived")
	ErrLastOwner      = errors.New(errors.Conflict, "an organisation cannot lose its last owner")

	ErrStaleWrite = errors.New(errors.Conflict, "the record changed since it was read")

	// A redelivery, not a failure. decisions/0007 makes delivery at-least-once,
	// so a subscriber seeing this has already done its work.
	ErrAlreadyProvisioned = errors.New(errors.Conflict, "already provisioned from that event")
)
