package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired   = errors.New(errors.Invalid, "an identifier is required")
	ErrTimeRequired = errors.New(errors.Invalid, "an instant is required")
	ErrOrgRequired  = errors.New(errors.Invalid, "a check belongs to an organisation")

	ErrNameRequired     = errors.New(errors.Invalid, "a check needs a name")
	ErrNameTooLong      = errors.New(errors.Invalid, "the name is too long")
	ErrQuestionRequired = errors.New(errors.Invalid, "a check needs the question it asks")
	ErrQuestionTooLong  = errors.New(errors.Invalid, "the question is too long")
	// The message NAMES THE SET. `domain` was a subject until decisions/0034 and
	// a client that still sends it gets a dead end otherwise — the fix is
	// `host`, and the error is the only place anybody will read that.
	ErrSubjectUnknown = errors.New(errors.Invalid,
		"a coverage subject is one of host, cidr, ip, asn, url, repo, email — "+
			"there is no `domain`, because a domain is a host")
	ErrAppliesEmpty     = errors.New(errors.Invalid, "a check applies to at least one kind")
	ErrIntervalNegative = errors.New(errors.Invalid, "an interval cannot be negative")

	ErrToolRequired  = errors.New(errors.Invalid, "a step names the tool it runs")
	ErrStepUnknown   = errors.New(errors.Invalid, "a flow names a step this check does not have")
	ErrFlowToSelf    = errors.New(errors.Invalid, "a step cannot feed itself")
	ErrFlowDuplicate = errors.New(errors.Invalid, "that flow is already in the chain")
	ErrChainCyclic   = errors.New(errors.Invalid, "the chain has a cycle")

	ErrNotFound   = errors.New(errors.NotFound, "check")
	ErrNameTaken  = errors.New(errors.Conflict, "that name is already used in this organisation")
	ErrArchived   = errors.New(errors.Conflict, "the check is archived")
	ErrStaleWrite = errors.New(errors.Conflict, "the record changed since it was read")
)
