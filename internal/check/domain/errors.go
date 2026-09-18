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

	// The three ways an edge cannot carry anything — decisions/0032's deferred
	// rule, made urgent by 0039. The messages name the SHAPE of the mistake
	// rather than the ids, because the fix is the reader's and an id is not a
	// sentence.
	ErrEdgeProducesNothing = errors.New(errors.Invalid, "that step's tool produces nothing, so it cannot feed another")
	ErrEdgeConsumesNothing = errors.New(errors.Invalid, "that step's tool is seeded from the target and cannot be fed by another")
	ErrEdgeMismatched      = errors.New(errors.Invalid, "that connection carries nothing: the two tools do not deal in the same kind")
	ErrChainCyclic         = errors.New(errors.Invalid, "the chain has a cycle")

	ErrNotFound   = errors.New(errors.NotFound, "check")
	ErrNameTaken  = errors.New(errors.Conflict, "that name is already used in this organisation")
	ErrArchived   = errors.New(errors.Conflict, "the check is archived")
	ErrStaleWrite = errors.New(errors.Conflict, "the record changed since it was read")
)
