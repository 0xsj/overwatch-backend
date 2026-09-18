package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrInvalid  = errors.New(errors.Invalid, "invalid evidence relationship")
	ErrNotFound = errors.New(errors.NotFound, "evidence relationship or observation not found")
)
