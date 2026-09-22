package egress

import (
	"fmt"

	"github.com/0xsj/overwatch-backend/pkg/errors"
)

var (
	ErrBlocked          = errors.New(errors.Forbidden, "the address is not one this system may connect to")
	ErrTooLarge         = errors.New(errors.Unprocessable, "the response body exceeded the limit")
	ErrTooManyRedirects = errors.New(errors.Unprocessable, "too many redirects")
	ErrNotHTTP          = errors.New(errors.Invalid, "only http and https are dialled")
)

func ResponseError(response *Response) error {
	if response == nil {
		return errors.New(errors.Internal, "egress: missing response")
	}
	kind := errors.Unavailable
	message := fmt.Sprintf("reference returned HTTP status %d", response.Status)
	switch {
	case response.Status == 408:
		kind = errors.Timeout
	case response.Status == 429:
		kind = errors.RateLimited
		message = "reference asked us to slow down"
	}
	return errors.New(kind, message).WithRetryAfter(response.RetryAfter)
}
