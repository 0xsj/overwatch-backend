package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrWorkspaceRequired = errors.New(errors.Invalid, "health is asked about one engagement")

	// ErrProbeUnknown is a kind in `All` with no read behind it. It surfaces as
	// an UNMEASURED probe rather than a missing one, because a report that
	// silently skipped a kind would be the untrustworthy-but-clean answer this
	// package exists to refuse.
	ErrProbeUnknown = errors.New(errors.Internal, "no probe answers that kind")
)
