package postgres

import (
	"context"
	stderrors "errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/0xsj/overwatch-backend/pkg/errors"
)

// Translate turns a driver error into this codebase's vocabulary. It is the only
// place a SQLSTATE is read; above this line nothing knows what 23505 means.
func Translate(ctx context.Context, err error, msg string) error {
	if err == nil {
		return nil
	}
	if stderrors.Is(err, pgx.ErrNoRows) {
		return errors.Wrap(err, errors.NotFound, msg)
	}
	if stderrors.Is(err, context.Canceled) {
		return errors.Wrap(err, errors.Canceled, msg)
	}
	if stderrors.Is(err, context.DeadlineExceeded) {
		return errors.Wrap(err, errors.Timeout, msg)
	}

	var pg *pgconn.PgError
	if !stderrors.As(err, &pg) {
		// A dial failure, a closed pool, a TLS problem: no SQLSTATE, and none of
		// them is the caller's doing.
		return errors.Wrap(err, errors.Unavailable, msg)
	}

	out := errors.Wrap(err, kindOf(ctx, pg.Code), msg)
	if pg.ConstraintName != "" {
		// A diagnostic, never a response. WithDetail is the half of pkg/errors
		// that no caller-facing path may read.
		out = out.WithDetail("constraint", pg.ConstraintName)
	}
	if pg.TableName != "" {
		out = out.WithDetail("table", pg.TableName)
	}
	return out.WithDetail("sqlstate", pg.Code)
}

func kindOf(ctx context.Context, code string) errors.Kind {
	switch code {
	case "23505", "23P01": // unique_violation, exclusion_violation
		return errors.Conflict
	case "23514": // check_violation — well-formed, refused by a rule
		return errors.Unprocessable
	case "23503", "23502": // foreign_key_violation, not_null_violation
		return errors.Invalid
	case "22P02", "22001", "22003", "22007", "22008":
		// invalid text, too long, out of range, bad date/time
		return errors.Invalid
	case "40001", "40P01": // serialization_failure, deadlock_detected
		return errors.Unavailable
	case "55P03", "53300", "53400": // lock_not_available, too_many_connections
		return errors.Unavailable
	case "57014": // query_canceled — two events, one code
		if ctx.Err() != nil {
			return errors.Canceled
		}
		return errors.Timeout
	case "57P01", "57P02", "57P03": // admin shutdown, crash shutdown, cannot connect now
		return errors.Unavailable
	case "42501": // insufficient_privilege
		return errors.Forbidden
	}
	switch {
	case strings.HasPrefix(code, "08"): // connection exception
		return errors.Unavailable
	case strings.HasPrefix(code, "53"): // insufficient resources
		return errors.Unavailable
	case strings.HasPrefix(code, "40"): // transaction rollback
		return errors.Unavailable
	}
	// 42*** is a broken statement, 22012 is our arithmetic, 0A000 is a feature we
	// asked for and did not get. None of them is fixable by sending different
	// input, so none of them is the caller's.
	return errors.Internal
}

// IsConstraint reports whether err came from the named constraint. It is how a
// repository turns "the database refused this" into a domain sentinel, without
// anything above it learning a SQLSTATE.
func IsConstraint(err error, name string) bool {
	var pg *pgconn.PgError
	return stderrors.As(err, &pg) && pg.ConstraintName == name
}
