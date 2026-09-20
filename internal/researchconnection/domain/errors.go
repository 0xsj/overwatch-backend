package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrInvalid             = errors.New(errors.Invalid, "a research connection needs two different records")
	ErrKindUnknown         = errors.New(errors.Invalid, "research connection kind is unknown")
	ErrStateUnknown        = errors.New(errors.Invalid, "research connection state must be proposed, accepted, rejected, or deferred")
	ErrReviewFilterUnknown = errors.New(errors.Invalid, "research connection review filter must be open, conflicted, or uncited")
	ErrRationaleRequired   = errors.New(errors.Invalid, "a research connection needs a rationale")
	ErrEvidenceTooMany     = errors.New(errors.Invalid, "a research connection can cite at most twelve observations per side")
	ErrDuplicateEvidence   = errors.New(errors.Invalid, "a research connection cannot cite the same observation twice")
	ErrEvidenceConflict    = errors.New(errors.Invalid, "an observation cannot support and oppose the same connection")
	ErrNotFound            = errors.New(errors.NotFound, "research connection")
)
