package domain

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// Path is a dotted key path into a decoded JSON record, and it is the WHOLE
// expression language — decisions/0035.
//
//	.host      a key
//	.a.b       nested
//	.a[].b     flatten an array: one value per element
//
// **There are no filters, no functions and no arithmetic**, and that is a
// decision rather than an unfinished feature. The moment an expression can
// COMPUTE, a mapping stops being a reading and becomes a derivation — `0003` —
// and the resulting row would be a value no source stated, carrying lineage
// pointing at bytes that do not contain it.
type Path struct {
	// Steps alternate keys and flattens. A `[]` step is spelled as the empty
	// key with Flatten set, so the parse is one pass and the walk is one loop.
	Steps []Step
}

type Step struct {
	Key string
	// Flatten means "and then, for each element". It rides on the step that
	// PRECEDED the brackets, so `.a[].b` is {a, flatten} then {b}.
	Flatten bool
}

// ParsePath reads `.a.b[].c`. The leading dot is optional, because half the
// world writes `.host` and half writes `host` and refusing one of them buys
// nothing.
func ParsePath(expression string) (Path, error) {
	raw := strings.TrimSpace(expression)
	if raw == "" {
		return Path{}, ErrPathRequired
	}
	raw = strings.TrimPrefix(raw, ".")
	if raw == "" {
		return Path{}, ErrPathRequired
	}

	var out Path
	for _, segment := range strings.Split(raw, ".") {
		step := Step{Key: segment}
		for strings.HasSuffix(step.Key, "[]") {
			// Repeated brackets would mean a nested array and this walk does one
			// level per step. Refuse rather than silently reading the first.
			if step.Flatten {
				return Path{}, ErrPathMalformed
			}
			step.Flatten = true
			step.Key = strings.TrimSuffix(step.Key, "[]")
		}
		if step.Key == "" {
			return Path{}, ErrPathMalformed
		}
		if strings.ContainsAny(step.Key, "[]") {
			return Path{}, ErrPathMalformed
		}
		out.Steps = append(out.Steps, step)
	}
	if len(out.Steps) == 0 {
		return Path{}, ErrPathRequired
	}
	return out, nil
}

func (p Path) String() string {
	var b strings.Builder
	for _, s := range p.Steps {
		b.WriteByte('.')
		b.WriteString(s.Key)
		if s.Flatten {
			b.WriteString("[]")
		}
	}
	return b.String()
}

// Read walks a decoded record and answers every value the path reaches.
//
// **An empty result is NOT an error.** The tool did not say that, this time —
// which is a different thing from the mapping being wrong, and only the second
// deserves a message. `0035` argues that distinction is invisible per-record and
// shows up only as a low MAPPED ratio.
func (p Path) Read(record any) []string {
	at := []any{record}
	for _, step := range p.Steps {
		next := make([]any, 0, len(at))
		for _, node := range at {
			object, ok := node.(map[string]any)
			if !ok {
				continue
			}
			child, ok := object[step.Key]
			if !ok {
				continue
			}
			if !step.Flatten {
				next = append(next, child)
				continue
			}
			list, ok := child.([]any)
			if !ok {
				// A `[]` against a scalar reads nothing rather than reading the
				// scalar. The mapping said "for each"; there is no each.
				continue
			}
			next = append(next, list...)
		}
		at = next
	}

	out := make([]string, 0, len(at))
	for _, node := range at {
		if text, ok := scalar(node); ok {
			out = append(out, text)
		}
	}
	return out
}

// scalar renders a leaf. An object or an array is NOT a value: a mapping that
// lands on one has named a branch, and stringifying it would put a JSON blob in
// a field that a person is going to read as a server header.
func scalar(node any) (string, bool) {
	switch v := node.(type) {
	case string:
		return v, true
	case bool:
		return strconv.FormatBool(v), true
	case float64:
		// json decodes every number as float64. Integers are rendered without a
		// decimal point, because `443` in a port field reading `443.000000` is
		// the record lying about what the source said.
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10), true
		}
		return strconv.FormatFloat(v, 'g', -1, 64), true
	case json.Number:
		return v.String(), true
	case nil:
		// A JSON null is the source declining to say, which is not a value.
		return "", false
	default:
		return "", false
	}
}

// Leaves enumerates every distinct leaf path in a record, in the same spelling
// ParsePath accepts. It is what makes FIELDS SEEN and LEFT ALONE countable —
// decisions/0035 — and it is the reason a field nobody mapped is a record rather
// than a guess.
//
// An array becomes ONE path with `[]` rather than one per index, because
// `.a[0].b` and `.a[1].b` are the same field said twice and a mapping names the
// shape, not the position.
func Leaves(record any) []string {
	seen := map[string]bool{}
	walk(record, nil, seen)
	out := make([]string, 0, len(seen))
	for path := range seen {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func walk(node any, trail []string, seen map[string]bool) {
	switch v := node.(type) {
	case map[string]any:
		for key, child := range v {
			walk(child, append(trail, key), seen)
		}
	case []any:
		if len(trail) == 0 {
			// A root array is the JSONL case handled by the caller. Nothing to
			// name here.
			return
		}
		// Mark the LAST step as flattened and descend once for every element.
		// The recursion collapses them because the trail is identical.
		flattened := append([]string(nil), trail...)
		flattened[len(flattened)-1] += "[]"
		for _, child := range v {
			walk(child, flattened, seen)
		}
	default:
		if _, ok := scalar(node); !ok {
			return
		}
		if len(trail) == 0 {
			return
		}
		seen["."+strings.Join(trail, ".")] = true
	}
}
