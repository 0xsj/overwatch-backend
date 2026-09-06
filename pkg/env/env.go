package env

import (
	"slices"
	"strconv"
	"strings"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

type Reader struct {
	lookup   Lookup
	declared []Var
	problems map[string]string
}

func New(lookup Lookup) *Reader {
	return &Reader{lookup: lookup, problems: make(map[string]string)}
}

func (r *Reader) fail(key, problem string) { r.problems[key] = problem }

func (r *Reader) resolved(v Var) { r.declared = append(r.declared, v) }

func (r *Reader) present(key string) (string, bool) {
	v, ok := r.lookup(key)
	switch {
	case !ok:
		r.fail(key, "required")
		return "", false
	case v == "":
		r.fail(key, "must not be empty")
		return "", false
	}
	return v, true
}

func (r *Reader) Required(key string) string {
	v, ok := r.present(key)
	if !ok {
		return ""
	}
	r.resolved(Var{Key: key, Value: v, Set: true})
	return v
}

func (r *Reader) String(key, fallback string) string {
	v, ok := r.lookup(key)
	if !ok {
		r.resolved(Var{Key: key, Value: fallback, Default: fallback})
		return fallback
	}
	r.resolved(Var{Key: key, Value: v, Default: fallback, Set: true})
	return v
}

func (r *Reader) RequiredInt(key string) int {
	v, ok := r.present(key)
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		r.fail(key, "not a number: "+v)
		return 0
	}
	r.resolved(Var{Key: key, Value: v, Set: true})
	return n
}

func (r *Reader) Int(key string, fallback int) int {
	def := strconv.Itoa(fallback)
	v, ok := r.lookup(key)
	if !ok {
		r.resolved(Var{Key: key, Value: def, Default: def})
		return fallback
	}
	if v == "" {
		r.fail(key, "must not be empty")
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		r.fail(key, "not a number: "+v)
		return 0
	}
	r.resolved(Var{Key: key, Value: v, Default: def, Set: true})
	return n
}

func (r *Reader) Enum(key, fallback string, allowed ...string) string {
	if !slices.Contains(allowed, fallback) {
		panic("env: Enum fallback " + strconv.Quote(fallback) + " for " + key +
			" is not in the allowed set " + strings.Join(allowed, "|"))
	}
	v, ok := r.lookup(key)
	if !ok {
		r.resolved(Var{Key: key, Value: fallback, Default: fallback})
		return fallback
	}
	if v == "" {
		r.fail(key, "must not be empty")
		return ""
	}
	if !slices.Contains(allowed, v) {
		r.fail(key, "not one of "+strings.Join(allowed, "|")+": "+v)
		return ""
	}
	r.resolved(Var{Key: key, Value: v, Default: fallback, Set: true})
	return v
}

func (r *Reader) Secret(key string) secret.String {
	v, ok := r.present(key)
	if !ok {
		return secret.String{}
	}
	r.resolved(Var{Key: key, Value: secret.Redacted, Set: true, Secret: true})
	return secret.New(v)
}

func (r *Reader) Declared() []Var {
	out := slices.Clone(r.declared)
	slices.SortFunc(out, func(a, b Var) int { return strings.Compare(a.Key, b.Key) })
	return out
}

func (r *Reader) Err() error {
	if len(r.problems) == 0 {
		return nil
	}
	return errors.New(errors.Invalid, "invalid configuration").WithFields(r.problems)
}
