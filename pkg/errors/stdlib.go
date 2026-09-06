package errors

import stderrors "errors"

func Is(err, target error) bool { return stderrors.Is(err, target) }

func As(err error, target any) bool { return stderrors.As(err, target) }

func Unwrap(err error) error { return stderrors.Unwrap(err) }

func Join(errs ...error) error { return stderrors.Join(errs...) }
