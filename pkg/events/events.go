package events

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

const (
	MaxNameLength    = 128
	MaxSubjectLength = 256
)

type Minter interface {
	NewID() id.ID
}

type Clock interface {
	Now() time.Time
}

type Event struct {
	ID         id.ID
	Name       string
	Subject    string
	Decision   bool
	OccurredAt time.Time
	Provenance provenance.Provenance
	Payload    json.RawMessage
}

// New mints an event for a UNIT OF WORK. It refuses everything a subscriber
// could not act on: a name that is not a name, a subject that names nothing, a
// provenance that says nothing caused this, and a payload that will not encode.
func New(m Minter, c Clock, name, subject string, p provenance.Provenance, payload any) (Event, error) {
	return mint(m, c, name, subject, false, p, payload)
}

// NewDecision mints an event for an act a PERSON is accountable for — one that
// changed stored state or disclosed something. decisions/0014 for which is
// which. It is a separate constructor rather than a bool parameter so the choice
// is legible at the call site and cannot be left to a default, and so that
// grepping NewDecision enumerates everything claiming to be auditable.
func NewDecision(m Minter, c Clock, name, subject string, p provenance.Provenance, payload any) (Event, error) {
	return mint(m, c, name, subject, true, p, payload)
}

func mint(m Minter, c Clock, name, subject string, decision bool, p provenance.Provenance, payload any) (Event, error) {
	if m == nil {
		panic("events: New with a nil Minter")
	}
	if c == nil {
		panic("events: New with a nil Clock")
	}
	if !ValidName(name) {
		return Event{}, errors.Newf(errors.Internal,
			"events: %q is not a name — lowercase, dot-separated, at least two segments of [a-z0-9_], up to %d characters",
			name, MaxNameLength)
	}
	if !ValidSubject(subject) {
		return Event{}, errors.Newf(errors.Internal,
			"events: %q is not a subject — kind:id, kind of [a-z0-9_], a non-empty id, up to %d characters",
			subject, MaxSubjectLength)
	}
	// An event with no provenance cannot answer what caused it, and that answer
	// cannot be reconstructed later from anything else.
	if p.IsZero() {
		return Event{}, errors.Newf(errors.Internal, "events: %s carries no provenance", name)
	}
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return Event{}, errors.Wrap(err, errors.Internal, "events: payload for "+name)
		}
		raw = b
	}
	return Event{
		ID:         m.NewID(),
		Name:       name,
		Subject:    subject,
		Decision:   decision,
		OccurredAt: c.Now(),
		Provenance: p,
		Payload:    raw,
	}, nil
}

// Namespace is the first segment: the domain that emitted this. It is the facet
// a client groups by, so it is a method rather than something every reader
// re-splits.
func (e Event) Namespace() string {
	if i := strings.IndexByte(e.Name, '.'); i > 0 {
		return e.Name[:i]
	}
	return e.Name
}

// SubjectKind and SubjectID split the subject so a subscriber groups by kind
// without parsing it and without a lookup. Both are empty for a subject that is
// not well formed, which [New] does not mint.
func (e Event) SubjectKind() string {
	kind, _, ok := strings.Cut(e.Subject, ":")
	if !ok {
		return ""
	}
	return kind
}

func (e Event) SubjectID() string {
	_, ident, ok := strings.Cut(e.Subject, ":")
	if !ok {
		return ""
	}
	return ident
}

func (e Event) IsZero() bool { return e.ID.IsZero() }

// Into decodes the payload. A handler that cannot decode has been handed an
// event it does not understand, which is a programming error rather than bad
// input — the publisher is in this repository.
func (e Event) Into(v any) error {
	if len(e.Payload) == 0 {
		return errors.Newf(errors.Internal, "events: %s has no payload", e.Name)
	}
	if err := json.Unmarshal(e.Payload, v); err != nil {
		return errors.Wrap(err, errors.Internal, "events: decode "+e.Name)
	}
	return nil
}

func ValidName(s string) bool {
	if len(s) == 0 || len(s) > MaxNameLength {
		return false
	}
	segments := 0
	for _, seg := range strings.Split(s, ".") {
		if seg == "" {
			return false
		}
		for i := 0; i < len(seg); i++ {
			c := seg[i]
			ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_'
			if !ok {
				return false
			}
		}
		segments++
	}
	return segments >= 2
}

// ValidSubject reports whether s names a thing: a kind of [a-z0-9_], a colon,
// and a non-empty id. The id is deliberately unconstrained beyond being present
// and free of whitespace — it crosses schemas, and a rule about its shape here
// would be a rule about every domain's identifiers.
func ValidSubject(s string) bool {
	if len(s) == 0 || len(s) > MaxSubjectLength {
		return false
	}
	kind, ident, ok := strings.Cut(s, ":")
	if !ok || kind == "" || ident == "" {
		return false
	}
	for i := 0; i < len(kind); i++ {
		c := kind[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return strings.IndexFunc(ident, unicode.IsSpace) < 0
}

type Publisher interface {
	Publish(ctx context.Context, evs ...Event) error
}

type Handler func(ctx context.Context, e Event) error
