package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired   = errors.New(errors.Invalid, "an identifier is required")
	ErrTimeRequired = errors.New(errors.Invalid, "an instant is required")

	ErrNameRequired = errors.New(errors.Invalid, "an organisation needs a name")
	ErrNameTooLong  = errors.New(errors.Invalid, "the name is too long")

	ErrRoleUnknown  = errors.New(errors.Invalid, "not a role this system knows")
	ErrLevelUnknown = errors.New(errors.Invalid, "not an access level this system knows")

	// A grant of `none` is a revocation, and revoking is deleting the row.
	ErrGrantEmpty = errors.New(errors.Invalid, "a grant of none is a deletion, not a row")

	ErrGrantNotFound = errors.New(errors.NotFound, "grant")
	ErrGrantExists   = errors.New(errors.Conflict, "that member already has a grant on this workspace")

	// Refused at WRITE time as ergonomics; the read-time min() is the security
	// property — decisions/0023. Both, and neither is redundant.
	ErrAboveCeiling = errors.New(errors.Conflict,
		"that role cannot hold that level on a workspace")
	ErrNotAMember = errors.New(errors.Conflict,
		"that account is not a member of this organisation")
	ErrStatusUnknown = errors.New(errors.Invalid, "not a status this system knows")

	ErrOrgNotFound    = errors.New(errors.NotFound, "organisation")
	ErrMemberNotFound = errors.New(errors.NotFound, "member")
	ErrMemberExists   = errors.New(errors.Conflict, "that account is already a member")
	ErrMemberArchived = errors.New(errors.Conflict, "the member is archived")
	ErrLastOwner      = errors.New(errors.Conflict, "an organisation cannot lose its last owner")

	ErrStaleWrite = errors.New(errors.Conflict, "the record changed since it was read")

	ErrHashRequired  = errors.New(errors.Invalid, "a token hash is required")
	ErrEmailRequired = errors.New(errors.Invalid, "an email address is required")
	ErrEmailTooLong  = errors.New(errors.Invalid, "that email address is too long")

	// ONE answer for unknown, expired, spent and withdrawn — decisions/0025 and
	// the same rule every link in this system follows. Telling a caller which
	// tells somebody holding a guess which half of the guess was wrong.
	ErrInviteGone = errors.New(errors.Unauthenticated, "that invitation is not valid")
	// A DIFFERENT answer, because the caller is authenticated and the fix is
	// theirs: sign in as the address it was sent to.
	ErrInviteAddress = errors.New(errors.Forbidden,
		"that invitation was sent to a different address")
	ErrInviteTaken   = errors.New(errors.Conflict, "that invitation has already been accepted")
	ErrInviteExists  = errors.New(errors.Conflict, "there is already a live invitation for that address")
	ErrAlreadyMember = errors.New(errors.Conflict, "that address is already a member")
	ErrOwnerInvite   = errors.New(errors.Forbidden, "only an owner can invite an owner")
	ErrOwnerRole     = errors.New(errors.Forbidden,
		"only an owner can promote, demote or remove an owner")

	// A redelivery, not a failure. decisions/0007 makes delivery at-least-once,
	// so a subscriber seeing this has already done its work.
	ErrAlreadyProvisioned = errors.New(errors.Conflict, "already provisioned from that event")
)
