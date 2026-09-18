package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired                = errors.New(errors.Invalid, "an identifier is required")
	ErrWorkspaceRequired         = errors.New(errors.Invalid, "a brief belongs to an investigation")
	ErrTimeRequired              = errors.New(errors.Invalid, "a brief needs an instant")
	ErrTitleRequired             = errors.New(errors.Invalid, "a brief needs a title")
	ErrTitleTooLong              = errors.New(errors.Invalid, "that brief title is too long")
	ErrQuestionRequired          = errors.New(errors.Invalid, "a brief needs an investigation question")
	ErrQuestionTooLong           = errors.New(errors.Invalid, "that investigation question is too long")
	ErrAccountTooLong            = errors.New(errors.Invalid, "that current account is too long")
	ErrAlternativesTooLong       = errors.New(errors.Invalid, "those alternatives are too long")
	ErrLimitationsTooLong        = errors.New(errors.Invalid, "those limitations are too long")
	ErrNextStepsTooLong          = errors.New(errors.Invalid, "those next steps are too long")
	ErrTooManyObservations       = errors.New(errors.Invalid, "a brief can cite at most twelve observations")
	ErrTooManyQuestions          = errors.New(errors.Invalid, "a brief can link at most eight questions")
	ErrTooManyConnections        = errors.New(errors.Invalid, "a brief can link at most eight connections")
	ErrDuplicateObservation      = errors.New(errors.Invalid, "a brief cannot cite the same observation twice")
	ErrDuplicateQuestion         = errors.New(errors.Invalid, "a brief cannot link the same question twice")
	ErrDuplicateConnection       = errors.New(errors.Invalid, "a brief cannot link the same connection twice")
	ErrNotFound                  = errors.New(errors.NotFound, "brief")
	ErrQuestionSnapshotMissing   = errors.New(errors.NotFound, "linked investigation question")
	ErrConnectionSnapshotMissing = errors.New(errors.NotFound, "linked investigation connection")
)
