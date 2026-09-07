package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired        = errors.New(errors.Invalid, "an identifier is required")
	ErrTimeRequired      = errors.New(errors.Invalid, "an instant is required")
	ErrWorkspaceRequired = errors.New(errors.Invalid, "a run belongs to an engagement")
	ErrTargetRequired    = errors.New(errors.Invalid, "a run names the target it looks at")
	ErrCheckRequired     = errors.New(errors.Invalid, "a run names the check it answers")

	ErrStateUnknown   = errors.New(errors.Invalid, "not a state this system knows")
	ErrOutcomeUnknown = errors.New(errors.Invalid, "not an outcome this system knows")
	ErrStreamUnknown  = errors.New(errors.Invalid, "not a stream this system knows")

	ErrArgvEmpty      = errors.New(errors.Invalid, "a tool with no argv cannot be run")
	ErrChainEmpty     = errors.New(errors.Invalid, "this check has no chain, so there is nothing to run")
	ErrExitOnRefusal  = errors.New(errors.Invalid, "a refused invocation never had a process, so it has no exit code")
	ErrRuleRequired   = errors.New(errors.Invalid, "a refusal names the rule that refused it")
	ErrReasonRequired = errors.New(errors.Invalid, "a skip names what did not arrive")
	ErrHashRequired   = errors.New(errors.Invalid, "an artifact is named by the hash of its bytes")

	ErrNotFound           = errors.New(errors.NotFound, "run")
	ErrInvocationNotFound = errors.New(errors.NotFound, "invocation")
	ErrArtifactNotFound   = errors.New(errors.NotFound, "artifact")
	ErrFinished           = errors.New(errors.Conflict, "the run has already finished")
	ErrNotRunnable        = errors.New(errors.Conflict, "this invocation is not waiting to run")
	ErrStaleWrite         = errors.New(errors.Conflict, "the record changed since it was read")
)
