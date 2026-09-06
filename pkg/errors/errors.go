package errors

import (
	"fmt"
	"maps"
)

type Error struct {
	Kind    Kind
	Msg     string
	Err     error
	Fields  map[string]string
	Details map[string]string
	Type    string
}

func New(kind Kind, msg string) *Error {
	return &Error{Kind: kind, Msg: msg}
}

func Newf(kind Kind, format string, args ...any) *Error {
	return &Error{Kind: kind, Msg: fmt.Sprintf(format, args...)}
}

func Wrap(err error, kind Kind, msg string) *Error {
	return &Error{Kind: kind, Msg: msg, Err: err}
}

func (e *Error) Error() string {
	switch {
	case e.Err == nil && e.Msg == "":
		return e.Kind.String()
	case e.Err == nil:
		return e.Msg
	case e.Msg == "":
		return e.Err.Error()
	default:
		return e.Msg + ": " + e.Err.Error()
	}
}

func (e *Error) Unwrap() error {
	return e.Err
}

func (e *Error) clone() *Error {
	c := *e
	c.Fields = maps.Clone(e.Fields)
	c.Details = maps.Clone(e.Details)
	return &c
}

func (e *Error) WithType(slug string) *Error {
	c := e.clone()
	c.Type = slug
	return c
}

func (e *Error) WithField(field, problem string) *Error {
	return e.WithFields(map[string]string{field: problem})
}

func (e *Error) WithFields(fields map[string]string) *Error {
	c := e.clone()
	if c.Fields == nil {
		c.Fields = make(map[string]string, len(fields))
	}
	maps.Copy(c.Fields, fields)
	return c
}

// WithDetail attaches a diagnostic that must never reach a caller. Constraint
// names, the subject of a refused authorization, the reason a policy said no.
func (e *Error) WithDetail(key, value string) *Error {
	return e.WithDetails(map[string]string{key: value})
}

func (e *Error) WithDetails(details map[string]string) *Error {
	c := e.clone()
	if c.Details == nil {
		c.Details = make(map[string]string, len(details))
	}
	maps.Copy(c.Details, details)
	return c
}
