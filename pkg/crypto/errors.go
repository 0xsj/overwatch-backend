package crypto

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrPasswordEmpty  = errors.New(errors.Invalid, "the password is empty")
	ErrHashUnreadable = errors.New(errors.Invalid, "the stored hash cannot be read")
)
