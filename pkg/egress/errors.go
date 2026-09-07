package egress

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrBlocked          = errors.New(errors.Forbidden, "the address is not one this system may connect to")
	ErrTooLarge         = errors.New(errors.Unprocessable, "the response body exceeded the limit")
	ErrTooManyRedirects = errors.New(errors.Unprocessable, "too many redirects")
	ErrNotHTTP          = errors.New(errors.Invalid, "only http and https are dialled")
)
