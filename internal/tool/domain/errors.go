package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired   = errors.New(errors.Invalid, "an identifier is required")
	ErrTimeRequired = errors.New(errors.Invalid, "an instant is required")
	ErrOrgRequired  = errors.New(errors.Invalid, "a tool belongs to an organisation")

	ErrNameRequired     = errors.New(errors.Invalid, "a tool needs a name")
	ErrNameTooLong      = errors.New(errors.Invalid, "the name is too long")
	ErrArgvRequired     = errors.New(errors.Invalid, "a tool needs an argv template")
	ErrIntensityUnknown = errors.New(errors.Invalid, "not a tool intensity this system knows")
	// Names the set, for the reason check's does. `domain` was a feed until
	// decisions/0034.
	ErrFeedUnknown = errors.New(errors.Invalid,
		"a feed is one of host, cidr, ip, asn, url, repo, email, account, "+
			"document, org, person, cert, key, whois, finding — there is no "+
			"`domain`, because a domain is a host")
	ErrStatusUnknown = errors.New(errors.Invalid, "not a status this system knows")

	ErrFieldRequired      = errors.New(errors.Invalid, "a mapping names the field it produces")
	ErrExpressionRequired = errors.New(errors.Invalid, "a mapping needs an expression")

	ErrNotFound    = errors.New(errors.NotFound, "tool")
	ErrMappingGone = errors.New(errors.NotFound, "mapping version")
	ErrNameTaken   = errors.New(errors.Conflict, "that name is already used in this organisation")
	ErrArchived    = errors.New(errors.Conflict, "the tool is archived")
	ErrAlreadyLive = errors.New(errors.Conflict, "that version is already the live one")
	ErrStaleWrite  = errors.New(errors.Conflict, "the record changed since it was read")
)
