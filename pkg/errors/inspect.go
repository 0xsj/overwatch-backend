package errors

import (
	"context"
	stderrors "errors"
	"maps"
)

const internalMessage = "internal error"

func first(err error) *Error {
	var e *Error
	if As(err, &e) {
		return e
	}
	return nil
}

func KindOf(err error) Kind {
	if e := first(err); e != nil {
		return e.Kind
	}
	switch {
	case stderrors.Is(err, context.DeadlineExceeded):
		return Timeout
	case stderrors.Is(err, context.Canceled):
		return Canceled
	}
	return Internal
}

func IsKind(err error, kind Kind) bool {
	return err != nil && KindOf(err) == kind
}

func KindsOf(err error) []Kind {
	var kinds []Kind
	var walk func(error)
	walk = func(e error) {
		for e != nil {
			if ke, ok := e.(*Error); ok {
				kinds = append(kinds, ke.Kind)
				return
			}
			if j, ok := e.(interface{ Unwrap() []error }); ok {
				for _, sub := range j.Unwrap() {
					walk(sub)
				}
				return
			}
			u, ok := e.(interface{ Unwrap() error })
			if !ok {
				switch {
				case stderrors.Is(e, context.DeadlineExceeded):
					kinds = append(kinds, Timeout)
				case stderrors.Is(e, context.Canceled):
					kinds = append(kinds, Canceled)
				}
				return
			}
			e = u.Unwrap()
		}
	}
	walk(err)
	return kinds
}

func Message(err error) string {
	e := first(err)
	if e == nil || e.Kind == Internal || e.Msg == "" {
		return internalMessage
	}
	return e.Msg
}

func TypeOf(err error) string {
	if e := first(err); e != nil {
		return e.Type
	}
	return ""
}

func FieldsOf(err error) map[string]string {
	if e := first(err); e != nil {
		return maps.Clone(e.Fields)
	}
	return nil
}

// DetailsOf collects the diagnostics along the whole chain. They are for logs;
// nothing that writes a response may read them.
//
// Unlike FieldsOf it does not stop at the outermost *Error, because a detail is
// attached where the fact is known — pkg/postgres knows the constraint, the
// command that wraps it does not — and wrapping must not discard it. An inner
// key wins, since it is the more specific claim.
//
// It visits EVERY *Error in the chain, not merely the outermost and the
// innermost. That sounds implied by "the whole chain" and is not: a walk that
// advances twice per iteration collects the ends and steps over the middle,
// which is a bug this function actually had. It survives any test built on a
// two-level chain and needs three to show. Stated because a specification that
// only says "does not stop early" is satisfied by an implementation that skips.
func DetailsOf(err error) map[string]string {
	var out map[string]string
	for err != nil {
		var e *Error
		if !As(err, &e) {
			break
		}
		for k, v := range e.Details {
			if out == nil {
				out = make(map[string]string, len(e.Details))
			}
			out[k] = v
		}
		err = e.Err
	}
	return out
}

func Retryable(err error) bool {
	if err == nil {
		return false
	}
	kinds := KindsOf(err)
	if len(kinds) == 0 {
		return false
	}
	for _, k := range kinds {
		if !k.Retryable() {
			return false
		}
	}
	return true
}
