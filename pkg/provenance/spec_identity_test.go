// Package provenance_test covers the IDENTITY and CARRIAGE half of
// pkg/provenance: actors, tenant, delegation, context carriage, the log/JSON
// renderings, and the string-validation half of Adopt.
//
// It is deliberately an external test package. Everything here goes through the
// exported surface, so no test can accidentally pin an unexported detail.
//
// Causal behaviour (New/Derive/DeriveFrom/Retry, correlation-vs-causation,
// Origin, depth, attempt, Root, Minter, struct comparability) belongs to a
// second suite and is NOT asserted here. New and Derive are called only to
// obtain a value to test identity and carriage against.
//
// Where a test rests on an inference rather than on a sentence of the
// specification, the comment above it says so and starts with "INFERENCE:".
package provenance_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// The W3C example traceparent: version 00, 32 hex trace-id, 16 hex span-id,
// 2 hex flags.
const validTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newSeq() *id.Sequence {
	return id.NewSequence(time.Unix(0, 0))
}

// newProvenance returns a root provenance. Nothing here asserts on how New
// fills the causal fields; it is only a source of a real value.
func newProvenance(t *testing.T) provenance.Provenance {
	t.Helper()
	return provenance.New(provenance.OriginRequest, newSeq())
}

// mintedIDStrings returns n distinct textual identifiers in this system's own
// format. Hand-written literals are not used: only a minted id is guaranteed to
// satisfy id.Parse.
func mintedIDStrings(t *testing.T, n int) []string {
	t.Helper()
	s := newSeq()
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		v := s.NewID().String()
		if v == "" {
			t.Fatalf("test setup is broken: a minted id must render as a non-empty string")
		}
		out = append(out, v)
	}
	return out
}

func mustUser(t *testing.T, s string) provenance.Actor {
	t.Helper()
	a, err := provenance.User(s)
	if err != nil {
		t.Fatalf("User(%q) must be constructible; it is the ordinary case: %v", s, err)
	}
	return a
}

func mustService(t *testing.T, s string) provenance.Actor {
	t.Helper()
	a, err := provenance.Service(s)
	if err != nil {
		t.Fatalf("Service(%q) must be constructible; it is the ordinary case: %v", s, err)
	}
	return a
}

func mustSystem(t *testing.T, s string) provenance.Actor {
	t.Helper()
	a, err := provenance.System(s)
	if err != nil {
		t.Fatalf("System(%q) must be constructible; it is the ordinary case: %v", s, err)
	}
	return a
}

func mustTenant(t *testing.T, p provenance.Provenance, tenant string) provenance.Provenance {
	t.Helper()
	q, err := p.WithTenant(tenant)
	if err != nil {
		t.Fatalf("WithTenant(%q) must accept a tenant made only of documented characters: %v", tenant, err)
	}
	return q
}

func mustOnBehalfOf(t *testing.T, p provenance.Provenance, a provenance.Actor) provenance.Provenance {
	t.Helper()
	q, err := p.WithOnBehalfOf(a)
	if err != nil {
		t.Fatalf("WithOnBehalfOf(%v) must succeed for an actor delegating to a different actor; that is the case the field exists for: %v", a, err)
	}
	return q
}

// attrMap flattens Attrs into key -> rendered value, and fails on a duplicate
// key: a repeated key makes the field ambiguous to every consumer of the record.
func attrMap(t *testing.T, attrs []slog.Attr) map[string]string {
	t.Helper()
	m := make(map[string]string, len(attrs))
	for _, a := range attrs {
		if _, dup := m[a.Key]; dup {
			t.Fatalf("Attrs emitted the key %q twice; the log fields are a contract and a duplicated key makes the field ambiguous to every reader", a.Key)
		}
		m[a.Key] = a.Value.String()
	}
	return m
}

func keySet(m map[string]string) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

func jsonKeys(t *testing.T, p provenance.Provenance, what string) map[string]any {
	t.Helper()
	b, err := p.MarshalJSON()
	if err != nil {
		t.Fatalf("%s: MarshalJSON must not fail for a value this package itself constructed: %v", what, err)
	}
	if !json.Valid(b) {
		t.Fatalf("%s: MarshalJSON must emit valid JSON, got %q", what, b)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: MarshalJSON must emit a JSON object so a stored record carries the fields at its top level; got %q (%v)", what, b, err)
	}
	return m
}

func assertInternal(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected an error and got nil", what)
	}
	if !errors.IsKind(err, errors.Internal) {
		t.Errorf("%s: every failure in this package must report errors.Internal, never Invalid - nobody can fix any of these by sending different input, and Invalid would put a 400 on a fault that is ours; got kind %q",
			what, errors.KindOf(err).String())
	}
}

// Characters the specification enumerates as accepted: A-Z a-z 0-9 . _ : / -
var acceptedIdentifiers = []struct {
	name  string
	value string
}{
	{"upper case letters", "ABCXYZ"},
	{"lower case letters", "abcxyz"},
	{"digits", "0123456789"},
	{"a dot", "svc.collector"},
	{"an underscore", "tenant_one"},
	{"a colon", "urn:acme:1"},
	{"a slash", "extract/llm"},
	{"a hyphen", "acme-corp"},
	{"every accepted character in one value", "AZaz09._:/-"},
	{"a single character, the lower bound of the length rule", "a"},
}

// The injection surface the specification names: quotes, backslashes, angle
// brackets, control characters and whitespace - plus characters simply outside
// the enumerated set, since the set is enumerated exhaustively.
var rejectedIdentifiers = []struct {
	name  string
	value string
}{
	{"a double quote", `acme"corp`},
	{"a single quote", "acme'corp"},
	{"a backslash", `acme\corp`},
	{"an opening angle bracket", "<script"},
	{"a closing angle bracket", "script>"},
	{"a newline, which forges a whole log entry", "acme\nlevel=error msg=forged"},
	{"a carriage return", "acme\rcorp"},
	{"a tab", "acme\tcorp"},
	{"a space", "acme corp"},
	{"a NUL byte", "acme\x00corp"},
	{"an ANSI escape, which drives a terminal", "acme\x1b[31mcorp"},
	{"an at sign, outside the enumerated set", "user@example.com"},
	{"a plus sign, outside the enumerated set", "acme+corp"},
	{"a percent sign, outside the enumerated set", "acme%20corp"},
	{"a comma, outside the enumerated set", "acme,corp"},
	{"a non-ASCII letter, outside the enumerated set", "acmé"},
}

// ---------------------------------------------------------------------------
// The field constants are a contract
// ---------------------------------------------------------------------------

func TestFieldConstantsHaveTheirContractedNames(t *testing.T) {
	cases := []struct {
		claim string
		got   string
		want  string
	}{
		{"the request field is named request_id everywhere", provenance.FieldRequest, "request_id"},
		{"the correlation field is named correlation_id everywhere", provenance.FieldCorrelation, "correlation_id"},
		{"the causation field is named causation_id everywhere", provenance.FieldCausation, "causation_id"},
		{"the origin field is named origin everywhere", provenance.FieldOrigin, "origin"},
		{"the depth field is named depth everywhere", provenance.FieldDepth, "depth"},
		{"the attempt field is named attempt everywhere", provenance.FieldAttempt, "attempt"},
		{"the actor field is named actor everywhere", provenance.FieldActor, "actor"},
		{"the on-behalf-of field is named on_behalf_of everywhere", provenance.FieldOnBehalfOf, "on_behalf_of"},
		{"the tenant field is named tenant everywhere", provenance.FieldTenant, "tenant"},
		{"the traceparent field is named traceparent everywhere", provenance.FieldTraceparent, "traceparent"},
	}
	for _, tc := range cases {
		t.Run(tc.claim, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("field name is %q, want %q - a component emitting one spelling while another emits a different one passes its own tests while the corpus drifts apart, and nothing fails anywhere", tc.got, tc.want)
			}
		})
	}
}

func TestFieldConstantsAreAllDistinct(t *testing.T) {
	fields := map[string]string{
		"FieldRequest":     provenance.FieldRequest,
		"FieldCorrelation": provenance.FieldCorrelation,
		"FieldCausation":   provenance.FieldCausation,
		"FieldOrigin":      provenance.FieldOrigin,
		"FieldDepth":       provenance.FieldDepth,
		"FieldAttempt":     provenance.FieldAttempt,
		"FieldActor":       provenance.FieldActor,
		"FieldOnBehalfOf":  provenance.FieldOnBehalfOf,
		"FieldTenant":      provenance.FieldTenant,
		"FieldTraceparent": provenance.FieldTraceparent,
	}
	seen := make(map[string]string, len(fields))
	for name, value := range fields {
		if prev, dup := seen[value]; dup {
			t.Errorf("%s and %s are both %q; two fields sharing a name collapse into one column and neither can be read back", prev, name, value)
			continue
		}
		seen[value] = name
	}
}

// ---------------------------------------------------------------------------
// Kind
// ---------------------------------------------------------------------------

func TestKindsListsEveryKindExactlyOnceInDeclaredOrder(t *testing.T) {
	want := []provenance.Kind{
		provenance.KindAnonymous,
		provenance.KindUser,
		provenance.KindService,
		provenance.KindSystem,
	}
	if len(provenance.Kinds) != len(want) {
		t.Fatalf("Kinds has %d entries, want %d - Kinds is what a caller ranges over to handle every kind, so a missing member is a kind nobody handles", len(provenance.Kinds), len(want))
	}
	for i := range want {
		if provenance.Kinds[i] != want[i] {
			t.Errorf("Kinds[%d] = %v, want %v", i, provenance.Kinds[i], want[i])
		}
	}
}

func TestEveryKindRendersNonEmptyAndDistinctly(t *testing.T) {
	seen := make(map[string]provenance.Kind, len(provenance.Kinds))
	for _, k := range provenance.Kinds {
		s := k.String()
		if s == "" {
			t.Errorf("kind %d renders as the empty string; a kind that renders as nothing cannot be read back out of a log line or an audit row", uint8(k))
			continue
		}
		if prev, dup := seen[s]; dup {
			t.Errorf("kind %d and kind %d both render as %q; a user and a service that render identically make an audit row unable to say who acted", uint8(prev), uint8(k), s)
			continue
		}
		seen[s] = k
	}
}

// Fail-closed: an out-of-range Kind must not borrow the name of a real one.
// The specification does not say what an unknown kind renders as, so this
// asserts only that it does not claim to be one of the four.
func TestAnUnknownKindDoesNotRenderAsAValidKind(t *testing.T) {
	unknown := provenance.Kind(200)
	got := unknown.String()
	for _, k := range provenance.Kinds {
		if got == k.String() {
			t.Errorf("Kind(200).String() = %q, which is the rendering of a real kind (%d); an unrecognised kind that renders as a valid one is a silent authority claim", got, uint8(k))
		}
	}
}

// ---------------------------------------------------------------------------
// Actor: construction, zero value, accessors
// ---------------------------------------------------------------------------

// KindAnonymous is iota 0, so the zero Actor's kind is the anonymous one. This
// is the fail-closed member: an unset actor claims no identity and no authority.
func TestTheZeroActorIsAnonymous(t *testing.T) {
	var a provenance.Actor
	if a.Kind() != provenance.KindAnonymous {
		t.Errorf("the zero Actor has kind %v, want KindAnonymous - an unset actor must land on the member that claims nothing, not on one that confers identity", a.Kind())
	}
}

func TestTheZeroActorIsZeroAndAConstructedActorIsNot(t *testing.T) {
	var zero provenance.Actor
	if !zero.IsZero() {
		t.Errorf("the zero Actor reports IsZero() == false; a caller cannot then tell an unset actor from a real one")
	}
	constructed := []struct {
		name  string
		actor provenance.Actor
	}{
		{"a user", mustUser(t, "u1")},
		{"a service", mustService(t, "collector")},
		{"a system path", mustSystem(t, "extract/llm")},
	}
	for _, tc := range constructed {
		t.Run(tc.name+" is not the zero actor", func(t *testing.T) {
			if tc.actor.IsZero() {
				t.Errorf("%s reports IsZero() == true; a constructed actor carrying an identity is not the unset state", tc.name)
			}
		})
	}
}

func TestActorConstructorsCarryTheirKindAndIdentifier(t *testing.T) {
	cases := []struct {
		name  string
		actor provenance.Actor
		id    string
		kind  provenance.Kind
	}{
		{"User carries KindUser and the id it was given", mustUser(t, "u-1234"), "u-1234", provenance.KindUser},
		{"Service carries KindService and the name it was given", mustService(t, "pulsepoint"), "pulsepoint", provenance.KindService},
		{"System carries KindSystem and the path it was given", mustSystem(t, "extract/llm"), "extract/llm", provenance.KindSystem},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.actor.Kind() != tc.kind {
				t.Errorf("Kind() = %v, want %v - the kind answers WHO caused it and a constructor is the only place it is decided", tc.actor.Kind(), tc.kind)
			}
			if tc.actor.ID() != tc.id {
				t.Errorf("ID() = %q, want %q - the identifier must come back out exactly as it went in, or the audit row names somebody else", tc.actor.ID(), tc.id)
			}
		})
	}
}

// Both of these actors are named in the specification itself: the actor for an
// extraction is System("extract/llm"), and an unauthenticated webhook's claimed
// sender would be Service("pulsepoint").
func TestTheActorsTheSpecificationNamesAreConstructible(t *testing.T) {
	if _, err := provenance.System("extract/llm"); err != nil {
		t.Errorf(`System("extract/llm") must be constructible - the specification names it as the actor for every extraction: %v`, err)
	}
	if _, err := provenance.Service("pulsepoint"); err != nil {
		t.Errorf(`Service("pulsepoint") must be constructible - the specification names it as the shape of an authenticated sender identity: %v`, err)
	}
}

func TestAnonymousIsTheAnonymousKind(t *testing.T) {
	a := provenance.Anonymous()
	if a.Kind() != provenance.KindAnonymous {
		t.Errorf("Anonymous().Kind() = %v, want KindAnonymous", a.Kind())
	}
}

// ---------------------------------------------------------------------------
// Actor: rendering and parsing
// ---------------------------------------------------------------------------

// Round trip. This holds whatever textual form Actor.String chooses, which is
// why no literal appears here: the specification never states the form.
func TestParseActorRoundTripsActorString(t *testing.T) {
	actors := []struct {
		name  string
		actor provenance.Actor
	}{
		{"an anonymous actor", provenance.Anonymous()},
		{"a user", mustUser(t, "u-1234")},
		{"a service", mustService(t, "collector")},
		{"a system path", mustSystem(t, "extract/llm")},
		{"a user whose id looks like a service name", mustUser(t, "collector")},
		{"an identifier using every accepted character", mustUser(t, "AZaz09._:/-")},
	}
	for _, tc := range actors {
		t.Run(tc.name+" survives String then ParseActor", func(t *testing.T) {
			s := tc.actor.String()
			got, err := provenance.ParseActor(s)
			if err != nil {
				t.Fatalf("ParseActor(%q) failed on the output of Actor.String; a value this package rendered must parse back, or a stored actor cannot be read out of the record it was written to: %v", s, err)
			}
			if got.Kind() != tc.actor.Kind() {
				t.Errorf("kind after round trip is %v, want %v (rendered as %q) - losing the kind turns a service into a user in the audit trail", got.Kind(), tc.actor.Kind(), s)
			}
			if got.ID() != tc.actor.ID() {
				t.Errorf("id after round trip is %q, want %q (rendered as %q)", got.ID(), tc.actor.ID(), s)
			}
		})
	}
}

// Injectivity is what makes the round trip possible at all, and it is the
// security-relevant half: two different actors that render identically make an
// audit row unable to say who acted.
func TestDifferentActorsRenderDifferently(t *testing.T) {
	actors := []struct {
		name  string
		actor provenance.Actor
	}{
		{"anonymous", provenance.Anonymous()},
		{"user a", mustUser(t, "a")},
		{"service a", mustService(t, "a")},
		{"system a", mustSystem(t, "a")},
		{"user b", mustUser(t, "b")},
	}
	seen := make(map[string]string, len(actors))
	for _, tc := range actors {
		s := tc.actor.String()
		if prev, dup := seen[s]; dup {
			t.Errorf("%s and %s both render as %q; an identity claim that collides with another identity claim cannot be audited", prev, tc.name, s)
			continue
		}
		seen[s] = tc.name
	}
}

// ParseActor consumes stored bytes, so its failures are Internal like every
// other failure in this package.
//
// Note: ParseActor("") is deliberately not tested. If Anonymous renders as the
// empty string then "" must parse; the specification does not say, so guessing
// either way would pin a coin flip.
func TestParseActorRejectsTheInjectionSurfaceAndFailsInternal(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"a newline, which would forge a log entry", "user\nlevel=error"},
		{"an angle bracket", "<script>alert(1)</script>"},
		{"a double quote", `user"1`},
		{"a NUL byte", "user\x001"},
		{"an ANSI escape", "user\x1b[31m1"},
	}
	for _, tc := range cases {
		t.Run("ParseActor rejects "+tc.name, func(t *testing.T) {
			_, err := provenance.ParseActor(tc.input)
			assertInternal(t, err, "ParseActor("+strings.ToValidUTF8(tc.input, "")+")")
		})
	}
}

// ---------------------------------------------------------------------------
// The identifier charset and the length bound
//
// INFERENCE: the specification states the charset and the 1..MaxIDLength bound
// for "an adopted string" in a section about the injection surface. It does not
// enumerate which entry points apply it. These tests apply it to every string
// this package accepts from a caller - actor identifiers and tenant - because
// all of them are echoed into log lines and that is the surface the rule exists
// to close. A failure here is a real finding either way: either the rule is not
// applied where it should be, or the rule's scope is narrower than the
// specification's own security argument.
// ---------------------------------------------------------------------------

func TestIdentifiersRejectTheInjectionSurface(t *testing.T) {
	base := newProvenance(t)
	setters := []struct {
		name string
		set  func(string) error
	}{
		{"User", func(s string) error { _, err := provenance.User(s); return err }},
		{"Service", func(s string) error { _, err := provenance.Service(s); return err }},
		{"System", func(s string) error { _, err := provenance.System(s); return err }},
		{"WithTenant", func(s string) error { _, err := base.WithTenant(s); return err }},
	}
	for _, setter := range setters {
		for _, bad := range rejectedIdentifiers {
			t.Run(setter.name+" rejects "+bad.name, func(t *testing.T) {
				err := setter.set(bad.value)
				assertInternal(t, err, setter.name+" given "+bad.name)
			})
		}
	}
}

func TestIdentifiersAcceptEveryDocumentedCharacter(t *testing.T) {
	base := newProvenance(t)
	setters := []struct {
		name string
		set  func(string) error
	}{
		{"User", func(s string) error { _, err := provenance.User(s); return err }},
		{"Service", func(s string) error { _, err := provenance.Service(s); return err }},
		{"System", func(s string) error { _, err := provenance.System(s); return err }},
		{"WithTenant", func(s string) error { _, err := base.WithTenant(s); return err }},
	}
	for _, setter := range setters {
		for _, good := range acceptedIdentifiers {
			t.Run(setter.name+" accepts "+good.name, func(t *testing.T) {
				if err := setter.set(good.value); err != nil {
					t.Errorf("%s(%q) failed: %v - the charset accepts A-Z a-z 0-9 . _ : / - and is meant to accept every format anyone actually emits", setter.name, good.value, err)
				}
			})
		}
	}
}

// The bound is stated as "one to MaxIDLength characters": exactly MaxIDLength is
// in, one past it is out.
func TestIdentifierLengthBoundHoldsAtTheLimitAndOnePastIt(t *testing.T) {
	base := newProvenance(t)
	atLimit := strings.Repeat("a", provenance.MaxIDLength)
	pastLimit := strings.Repeat("a", provenance.MaxIDLength+1)

	setters := []struct {
		name string
		set  func(string) error
	}{
		{"User", func(s string) error { _, err := provenance.User(s); return err }},
		{"Service", func(s string) error { _, err := provenance.Service(s); return err }},
		{"System", func(s string) error { _, err := provenance.System(s); return err }},
		{"WithTenant", func(s string) error { _, err := base.WithTenant(s); return err }},
	}
	for _, setter := range setters {
		t.Run(setter.name+" accepts an identifier of exactly MaxIDLength characters", func(t *testing.T) {
			if err := setter.set(atLimit); err != nil {
				t.Errorf("%s rejected a %d character identifier: %v - MaxIDLength is the limit, not one past it", setter.name, provenance.MaxIDLength, err)
			}
		})
		t.Run(setter.name+" rejects an identifier one character past MaxIDLength", func(t *testing.T) {
			err := setter.set(pastLimit)
			assertInternal(t, err, setter.name+" given a "+"MaxIDLength+1 character identifier")
		})
	}
}

// INFERENCE: "one to MaxIDLength characters" makes the empty string too short.
// For WithTenant this is the weaker of the two readings - an implementation
// could plausibly treat "" as clearing the tenant - so the case is kept separate
// from the charset table above.
func TestEmptyIdentifiersAreRejected(t *testing.T) {
	base := newProvenance(t)
	setters := []struct {
		name string
		set  func(string) error
	}{
		{"User", func(s string) error { _, err := provenance.User(s); return err }},
		{"Service", func(s string) error { _, err := provenance.Service(s); return err }},
		{"System", func(s string) error { _, err := provenance.System(s); return err }},
		{"WithTenant", func(s string) error { _, err := base.WithTenant(s); return err }},
	}
	for _, setter := range setters {
		t.Run(setter.name+" rejects the empty identifier", func(t *testing.T) {
			err := setter.set("")
			assertInternal(t, err, setter.name+` given ""`)
		})
	}
}

// ---------------------------------------------------------------------------
// WithActor / WithTenant / WithOnBehalfOf: carriage, immutability, isolation
// ---------------------------------------------------------------------------

func TestWithActorCarriesTheActorAndLeavesTheOriginalUntouched(t *testing.T) {
	p := newProvenance(t)
	before := p.Actor()
	a := mustUser(t, "u-1234")

	q := p.WithActor(a)

	if q.Actor().Kind() != a.Kind() || q.Actor().ID() != a.ID() {
		t.Errorf("Actor() after WithActor is kind %v id %q, want kind %v id %q", q.Actor().Kind(), q.Actor().ID(), a.Kind(), a.ID())
	}
	if p.Actor().Kind() != before.Kind() || p.Actor().ID() != before.ID() {
		t.Errorf("WithActor changed the receiver: it now reports kind %v id %q, previously kind %v id %q - a provenance is immutable, and a mutating setter lets a child's actor rewrite its parent's record",
			p.Actor().Kind(), p.Actor().ID(), before.Kind(), before.ID())
	}
}

func TestWithActorIsIdempotent(t *testing.T) {
	p := newProvenance(t)
	a := mustService(t, "collector")

	once := p.WithActor(a)
	twice := once.WithActor(a)

	if once.Actor().Kind() != twice.Actor().Kind() || once.Actor().ID() != twice.Actor().ID() {
		t.Errorf("applying the same actor twice changed it: kind %v id %q then kind %v id %q", once.Actor().Kind(), once.Actor().ID(), twice.Actor().Kind(), twice.Actor().ID())
	}
	first := attrMap(t, once.Attrs())
	second := attrMap(t, twice.Attrs())
	if len(first) != len(second) {
		t.Fatalf("applying the same actor twice changed the rendered field set: %v then %v", first, second)
	}
	for k, v := range first {
		if second[k] != v {
			t.Errorf("field %q is %q after one WithActor and %q after two; setting the same value twice must be indistinguishable from setting it once", k, v, second[k])
		}
	}
}

func TestWithActorLeavesTenantAndTraceparentAlone(t *testing.T) {
	p := mustTenant(t, newProvenance(t), "acme")
	p = p.Adopt(provenance.Adopted{Traceparent: validTraceparent})

	q := p.WithActor(mustUser(t, "u-1"))

	if q.Tenant() != "acme" {
		t.Errorf("Tenant() = %q after WithActor, want %q - setting one field must not clear another", q.Tenant(), "acme")
	}
	if q.Traceparent() != validTraceparent {
		t.Errorf("Traceparent() = %q after WithActor, want %q - setting one field must not clear another", q.Traceparent(), validTraceparent)
	}
}

func TestWithTenantCarriesTheTenantAndLeavesTheOriginalUntouched(t *testing.T) {
	p := newProvenance(t)
	if p.Tenant() != "" {
		t.Fatalf("a fresh provenance reports tenant %q; nothing has set one", p.Tenant())
	}

	q := mustTenant(t, p, "acme-corp")

	if q.Tenant() != "acme-corp" {
		t.Errorf("Tenant() = %q, want %q - the tenant is what says whose data this touched", q.Tenant(), "acme-corp")
	}
	if p.Tenant() != "" {
		t.Errorf("WithTenant changed the receiver, which now reports tenant %q; a provenance is immutable", p.Tenant())
	}
	if q.Actor().Kind() != p.Actor().Kind() || q.Actor().ID() != p.Actor().ID() {
		t.Errorf("WithTenant changed the actor; setting one field must not disturb another")
	}
}

func TestWithTenantIsIdempotent(t *testing.T) {
	p := newProvenance(t)
	once := mustTenant(t, p, "acme")
	twice := mustTenant(t, once, "acme")
	if once.Tenant() != twice.Tenant() {
		t.Errorf("setting the same tenant twice gave %q then %q; it must be indistinguishable from setting it once", once.Tenant(), twice.Tenant())
	}
}

func TestActorAndTenantAreOrderIndependent(t *testing.T) {
	p := newProvenance(t)
	a := mustUser(t, "u-1")

	actorFirst := mustTenant(t, p.WithActor(a), "acme")
	tenantFirst := mustTenant(t, p, "acme").WithActor(a)

	if actorFirst.Actor().ID() != tenantFirst.Actor().ID() || actorFirst.Actor().Kind() != tenantFirst.Actor().Kind() {
		t.Errorf("the actor differs by application order: %q/%v vs %q/%v", actorFirst.Actor().ID(), actorFirst.Actor().Kind(), tenantFirst.Actor().ID(), tenantFirst.Actor().Kind())
	}
	if actorFirst.Tenant() != tenantFirst.Tenant() {
		t.Errorf("the tenant differs by application order: %q vs %q", actorFirst.Tenant(), tenantFirst.Tenant())
	}
	first := attrMap(t, actorFirst.Attrs())
	second := attrMap(t, tenantFirst.Attrs())
	if len(first) != len(second) {
		t.Fatalf("the rendered field set differs by application order: %v vs %v - these are independent fields and the order they were set in is not information", first, second)
	}
	for k, v := range first {
		if second[k] != v {
			t.Errorf("field %q is %q when the actor was set first and %q when the tenant was, for independent fields", k, v, second[k])
		}
	}
}

func TestAnOriginWithNoOnBehalfOfIsNotDelegated(t *testing.T) {
	p := newProvenance(t).WithActor(mustUser(t, "u-1"))

	if p.Delegated() {
		t.Errorf("Delegated() is true with nothing set on behalf of anyone; delegation is the case where the acting identity and the affected account differ, and reporting it by default makes every record look delegated")
	}
	if !p.OnBehalfOf().IsZero() {
		t.Errorf("OnBehalfOf() is non-zero when nothing set it: kind %v id %q", p.OnBehalfOf().Kind(), p.OnBehalfOf().ID())
	}
	if _, present := attrMap(t, p.Attrs())[provenance.FieldOnBehalfOf]; present {
		t.Errorf("Attrs emitted %q with nothing set on behalf of anyone; an absent field is omitted, never rendered", provenance.FieldOnBehalfOf)
	}
}

func TestAnActorActingForAnotherAccountIsDelegated(t *testing.T) {
	actor := mustUser(t, "u-operator")
	subject := mustUser(t, "u-customer")

	base := newProvenance(t).WithActor(actor)
	q := mustOnBehalfOf(t, base, subject)

	if !q.Delegated() {
		t.Errorf("Delegated() is false when %q acted on behalf of %q; that difference is the whole reason the field exists", actor.ID(), subject.ID())
	}
	if q.OnBehalfOf().ID() != subject.ID() || q.OnBehalfOf().Kind() != subject.Kind() {
		t.Errorf("OnBehalfOf() is kind %v id %q, want kind %v id %q", q.OnBehalfOf().Kind(), q.OnBehalfOf().ID(), subject.Kind(), subject.ID())
	}
	if q.Actor().ID() != actor.ID() {
		t.Errorf("Actor() is %q after WithOnBehalfOf, want %q - the acting identity must survive; overwriting it loses who actually did the thing", q.Actor().ID(), actor.ID())
	}
	if base.Delegated() {
		t.Errorf("WithOnBehalfOf changed the receiver, which now reports itself delegated; a provenance is immutable")
	}
	if got := attrMap(t, q.Attrs())[provenance.FieldOnBehalfOf]; got == "" {
		t.Errorf("Attrs did not emit %q for a delegated provenance; whose account it affects is the point of recording it", provenance.FieldOnBehalfOf)
	}
}

// The specification does not say what makes WithOnBehalfOf fail, so this asserts
// only the two things it does say: failures are Internal, and a value that was
// accepted comes back out unchanged.
func TestWithOnBehalfOfEitherCarriesTheActorOrFailsInternal(t *testing.T) {
	base := newProvenance(t).WithActor(mustUser(t, "u-operator"))
	cases := []struct {
		name  string
		actor provenance.Actor
	}{
		{"a different user", mustUser(t, "u-customer")},
		{"a service", mustService(t, "collector")},
		{"a system path", mustSystem(t, "extract/llm")},
		{"an anonymous actor", provenance.Anonymous()},
		{"the same actor that is already acting", mustUser(t, "u-operator")},
	}
	for _, tc := range cases {
		t.Run("on behalf of "+tc.name, func(t *testing.T) {
			q, err := base.WithOnBehalfOf(tc.actor)
			if err != nil {
				assertInternal(t, err, "WithOnBehalfOf("+tc.name+")")
				return
			}
			if q.OnBehalfOf().Kind() != tc.actor.Kind() || q.OnBehalfOf().ID() != tc.actor.ID() {
				t.Errorf("OnBehalfOf() is kind %v id %q, want kind %v id %q - an accepted actor must come back out exactly as it went in",
					q.OnBehalfOf().Kind(), q.OnBehalfOf().ID(), tc.actor.Kind(), tc.actor.ID())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Context carriage
// ---------------------------------------------------------------------------

func TestCurrentReturnsTheProvenancePutOnTheContext(t *testing.T) {
	p := mustTenant(t, newProvenance(t).WithActor(mustUser(t, "u-1")), "acme")
	ctx := provenance.NewContext(context.Background(), p)

	got, ok := provenance.Current(ctx)
	if !ok {
		t.Fatalf("Current reported no provenance on a context NewContext had just put one on; provenance travels ambiently and this is the only way a call site gets it")
	}
	if got.Tenant() != p.Tenant() {
		t.Errorf("tenant off the context is %q, want %q", got.Tenant(), p.Tenant())
	}
	if got.Actor().ID() != p.Actor().ID() || got.Actor().Kind() != p.Actor().Kind() {
		t.Errorf("actor off the context is kind %v id %q, want kind %v id %q", got.Actor().Kind(), got.Actor().ID(), p.Actor().Kind(), p.Actor().ID())
	}
	want := attrMap(t, p.Attrs())
	have := attrMap(t, got.Attrs())
	if len(want) != len(have) {
		t.Fatalf("the provenance off the context renders %d fields, the one put on it renders %d: %v vs %v", len(have), len(want), have, want)
	}
	for k, v := range want {
		if have[k] != v {
			t.Errorf("field %q is %q off the context and %q on the value put there; carriage must not alter what it carries", k, have[k], v)
		}
	}
}

func TestCurrentFailsClosedOnAContextWithNoProvenance(t *testing.T) {
	got, ok := provenance.Current(context.Background())
	if ok {
		t.Fatalf("Current reported a provenance on a bare context; the boolean is the only way a caller can tell a scope was never opened")
	}
	if !got.IsZero() {
		t.Errorf("Current returned a non-zero Provenance alongside ok == false; an absent scope must land on the zero value, not on something that reads as a real record")
	}
}

// INFERENCE: the specification does not describe nesting. Standard context
// semantics make the innermost value win, and a subscriber that opens a child
// scope inside a parent's would otherwise read the parent's.
func TestTheInnermostProvenanceOnAContextWins(t *testing.T) {
	outer := mustTenant(t, newProvenance(t), "outer")
	inner := mustTenant(t, newProvenance(t), "inner")

	ctx := provenance.NewContext(provenance.NewContext(context.Background(), outer), inner)

	got, ok := provenance.Current(ctx)
	if !ok {
		t.Fatalf("Current found no provenance on a doubly-wrapped context")
	}
	if got.Tenant() != "inner" {
		t.Errorf("Current returned the tenant %q, want %q - the nearer scope is the one this unit of work is running in", got.Tenant(), "inner")
	}
}

func TestRequireFailsInternalWhenNoScopeWasOpened(t *testing.T) {
	_, err := provenance.Require(context.Background())
	assertInternal(t, err, "Require on a context with no provenance")
}

func TestRequireReturnsTheCarriedProvenance(t *testing.T) {
	p := mustTenant(t, newProvenance(t), "acme")
	ctx := provenance.NewContext(context.Background(), p)

	got, err := provenance.Require(ctx)
	if err != nil {
		t.Fatalf("Require failed on a context carrying a provenance: %v", err)
	}
	if got.Tenant() != p.Tenant() {
		t.Errorf("Require returned tenant %q, want %q", got.Tenant(), p.Tenant())
	}
}

func TestPackageAttrsRendersTheProvenanceOnTheContext(t *testing.T) {
	p := mustTenant(t, newProvenance(t).WithActor(mustUser(t, "u-1")), "acme")
	ctx := provenance.NewContext(context.Background(), p)

	fromCtx := attrMap(t, provenance.Attrs(ctx))
	fromValue := attrMap(t, p.Attrs())

	if len(fromCtx) != len(fromValue) {
		t.Fatalf("Attrs(ctx) rendered %d fields and Provenance.Attrs rendered %d: %v vs %v - a log line must not depend on which of the two the writer reached for", len(fromCtx), len(fromValue), fromCtx, fromValue)
	}
	for k, v := range fromValue {
		if fromCtx[k] != v {
			t.Errorf("field %q is %q from the context and %q from the value; these must be the same record", k, fromCtx[k], v)
		}
	}
}

// INFERENCE: the specification does not say what Attrs(ctx) returns when no
// scope was opened. An absent field is omitted, so an absent provenance has no
// fields to emit; emitting placeholder fields would claim a chain that does not
// exist.
func TestPackageAttrsEmitsNothingWhenNoScopeWasOpened(t *testing.T) {
	got := provenance.Attrs(context.Background())
	if len(got) != 0 {
		t.Errorf("Attrs on a bare context emitted %d fields (%v); there is no request, no correlation and no actor to report, and emitting placeholders claims a chain that never existed", len(got), got)
	}
}

// ---------------------------------------------------------------------------
// Attrs: flat, and absent means omitted
// ---------------------------------------------------------------------------

func TestAttrsAreFlatAndNotNestedUnderAGroup(t *testing.T) {
	p := mustTenant(t, newProvenance(t).WithActor(mustUser(t, "u-1")), "acme")
	attrs := p.Attrs()

	if len(attrs) == 0 {
		t.Fatalf("Attrs emitted nothing for a provenance carrying a request, an actor and a tenant")
	}
	for _, a := range attrs {
		if a.Value.Kind() == slog.KindGroup {
			t.Errorf("Attrs emitted the key %q as a group; the fields are flat because that is the shape a query wants, and a nested group makes every existing query for request_id miss", a.Key)
		}
		if a.Key == "provenance" {
			t.Errorf(`Attrs emitted a "provenance" key; request_id belongs at the top level of the record, not under a wrapper`)
		}
	}
	m := attrMap(t, attrs)
	if _, present := m[provenance.FieldRequest]; !present {
		t.Errorf("Attrs did not emit %q at the top level; every provenance has a request minted for it and it is the field a query reaches for first. Got keys %v", provenance.FieldRequest, keySet(m))
	}
	if got := m[provenance.FieldTenant]; got != "acme" {
		t.Errorf("Attrs emitted %q = %q at the top level, want %q", provenance.FieldTenant, got, "acme")
	}
}

func TestAttrsOmitsAbsentFieldsRatherThanEmittingEmptyOnes(t *testing.T) {
	p := newProvenance(t) // no tenant, no on-behalf-of, no traceparent
	attrs := p.Attrs()
	m := attrMap(t, attrs)

	absent := []string{provenance.FieldTenant, provenance.FieldOnBehalfOf, provenance.FieldTraceparent}
	for _, key := range absent {
		if _, present := m[key]; present {
			t.Errorf("Attrs emitted %q = %q when nothing set it; an absent field is omitted, because %s=\"\" is a claim that the field existed and had no value, which is not a state this package can produce", key, m[key], key)
		}
	}
	for _, a := range attrs {
		if a.Value.String() == "" {
			t.Errorf("Attrs emitted %q with an empty value; absent, empty and defaulted are three different states here", a.Key)
		}
		if a.Value.Equal(slog.AnyValue(nil)) {
			t.Errorf("Attrs emitted %q as nil; an absent field is omitted, never nulled", a.Key)
		}
	}
}

// Monotonic: adding a layer of information must never remove any, and must add
// exactly the field that was set.
func TestAttrsGainExactlyOneFieldWhenATenantIsAdded(t *testing.T) {
	p := newProvenance(t)
	before := attrMap(t, p.Attrs())
	after := attrMap(t, mustTenant(t, p, "acme").Attrs())

	for k, v := range before {
		got, present := after[k]
		if !present {
			t.Errorf("adding a tenant removed the field %q; adding information must never take any away", k)
			continue
		}
		if got != v {
			t.Errorf("adding a tenant changed the field %q from %q to %q; the fields are independent", k, v, got)
		}
	}
	if len(after) != len(before)+1 {
		t.Fatalf("a provenance renders %d fields without a tenant and %d with one, want exactly one more: %v vs %v", len(before), len(after), keySet(before), keySet(after))
	}
	if after[provenance.FieldTenant] != "acme" {
		t.Errorf("the added field %q is %q, want %q", provenance.FieldTenant, after[provenance.FieldTenant], "acme")
	}
}

func TestAttrsCarryTenantAndTraceparentVerbatim(t *testing.T) {
	tenant := "acme-corp_1:eu/west"
	p := mustTenant(t, newProvenance(t), tenant).Adopt(provenance.Adopted{Traceparent: validTraceparent})
	m := attrMap(t, p.Attrs())

	if m[provenance.FieldTenant] != tenant {
		t.Errorf("%q rendered as %q, want %q - a value that is rewritten on the way into the record cannot be matched against the record it came from", provenance.FieldTenant, m[provenance.FieldTenant], tenant)
	}
	if m[provenance.FieldTraceparent] != validTraceparent {
		t.Errorf("%q rendered as %q, want %q - traceparent is carried verbatim precisely because nothing here reads it", provenance.FieldTraceparent, m[provenance.FieldTraceparent], validTraceparent)
	}
}

// INFERENCE: the specification does not state how an Actor renders into the
// actor field. Actor.String is the only textual form this package defines for
// one, and the field has to be readable back into an Actor for an audit row to
// mean anything.
func TestTheActorFieldRendersAsActorString(t *testing.T) {
	a := mustService(t, "collector")
	p := newProvenance(t).WithActor(a)
	m := attrMap(t, p.Attrs())

	if got := m[provenance.FieldActor]; got != a.String() {
		t.Errorf("%q rendered as %q, want %q (Actor.String) - the only textual form this package defines for an actor is the one ParseActor reads back", provenance.FieldActor, got, a.String())
	}
}

// ---------------------------------------------------------------------------
// LogValue
// ---------------------------------------------------------------------------

// INFERENCE: the specification says LogValue exists so that a provenance logged
// as a value does not render as nothing, and that the field names are a
// contract. It does not literally say LogValue is a group of the same fields
// Attrs returns; that is the only reading under which the two agree.
func TestLogValueCarriesTheSameFieldsAsAttrs(t *testing.T) {
	p := mustTenant(t, newProvenance(t).WithActor(mustUser(t, "u-1")), "acme")

	v := p.LogValue()
	if v.Kind() != slog.KindGroup {
		t.Fatalf("LogValue has kind %v, want a group - a provenance logged as a value has to render as the same set of named fields it renders as attributes, or the two spellings of the same record disagree", v.Kind())
	}
	fromValue := attrMap(t, v.Group())
	fromAttrs := attrMap(t, p.Attrs())

	if len(fromValue) != len(fromAttrs) {
		t.Fatalf("LogValue carries %d fields and Attrs carries %d: %v vs %v", len(fromValue), len(fromAttrs), keySet(fromValue), keySet(fromAttrs))
	}
	for k, want := range fromAttrs {
		if fromValue[k] != want {
			t.Errorf("field %q is %q via LogValue and %q via Attrs; the same record must not depend on how it was logged", k, fromValue[k], want)
		}
	}
}

func TestLogValueRendersSetFieldsAndOmitsAbsentOnes(t *testing.T) {
	withTenant := mustTenant(t, newProvenance(t), "acmecorp")
	without := newProvenance(t)

	set := withTenant.LogValue().String()
	if !strings.Contains(set, "acmecorp") {
		t.Errorf("LogValue rendered as %q and does not contain the tenant %q; LogValue exists exactly because unexported fields would otherwise render as nothing", set, "acmecorp")
	}
	unset := without.LogValue().String()
	if strings.Contains(unset, provenance.FieldTenant) {
		t.Errorf("LogValue rendered as %q and names the %q field when nothing set one; an absent field is omitted, never emitted empty", unset, provenance.FieldTenant)
	}
}

// ---------------------------------------------------------------------------
// String
// ---------------------------------------------------------------------------

func TestStringRendersSetFieldsAndOmitsAbsentOnes(t *testing.T) {
	without := newProvenance(t)
	with := mustTenant(t, without, "acmecorp")

	set := with.String()
	if !strings.Contains(set, "acmecorp") {
		t.Errorf("String rendered as %q and does not contain the tenant %q that was set on it", set, "acmecorp")
	}

	unset := without.String()
	// INFERENCE: that String names its fields at all. If it does, an absent one
	// must not be named.
	if strings.Contains(unset, provenance.FieldTenant) {
		t.Errorf("String rendered as %q and names the %q field when nothing set one; tenant=\"\" is a claim that a tenant existed and had no identity", unset, provenance.FieldTenant)
	}
	if strings.Contains(unset, `""`) {
		t.Errorf(`String rendered as %q and contains an empty quoted value; absent, empty and defaulted are three different states here`, unset)
	}
	if len(set) <= len(unset) {
		t.Errorf("String is %d characters with a tenant and %d without; a record carrying more information must not render shorter or identically, or the two states are indistinguishable in a log", len(set), len(unset))
	}
}

// ---------------------------------------------------------------------------
// JSON
// ---------------------------------------------------------------------------

// AMENDED after triage — added, replacing the zero-value case removed above.
func TestMarshalJSONRefusesTheZeroProvenance(t *testing.T) {
	var zero provenance.Provenance
	b, err := json.Marshal(zero)
	if err == nil {
		t.Fatalf("marshalling the zero Provenance produced %s: a record that contradicts an invariant is worse than a refusal, because it is the refusal's evidence written down as fact", b)
	}
	if !errors.IsKind(err, errors.Internal) {
		t.Errorf("marshalling the zero Provenance reported %v; every failure in this package is Internal, because nobody can fix it by sending different input", errors.KindOf(err))
	}
}

func TestMarshalJSONOmitsAbsentFieldsRatherThanNullingThem(t *testing.T) {
	cases := []struct {
		name string
		p    provenance.Provenance
	}{
		{"a root provenance with no tenant, delegation or traceparent", newProvenance(t)},
		// AMENDED after triage: the zero Provenance case was removed. Marshalling
		// it is an ERROR, not a record with omitted fields — the bytes would claim
		// a request whose identifier is nil and whose attempt is 0, and attempt is
		// documented never to be 0. See TestMarshalJSONRefusesTheZeroProvenance.
	}
	for _, tc := range cases {
		t.Run(tc.name+" emits no null and no empty string", func(t *testing.T) {
			m := jsonKeys(t, tc.p, tc.name)
			for k, v := range m {
				if v == nil {
					t.Errorf("key %q marshalled as null; an absent field is omitted, never nulled - a null is a claim that the field exists and has no value", k)
				}
				if s, isString := v.(string); isString && s == "" {
					t.Errorf("key %q marshalled as an empty string; %s=\"\" is a claim that the field existed and had no identity, which is not a state this package can produce", k, k)
				}
			}
		})
	}

	t.Run("a provenance with no tenant does not carry a tenant key", func(t *testing.T) {
		m := jsonKeys(t, newProvenance(t), "a root provenance")
		if _, present := m[provenance.FieldTenant]; present {
			t.Errorf("marshalled JSON carries the key %q with nothing set on it", provenance.FieldTenant)
		}
		if _, present := m[provenance.FieldOnBehalfOf]; present {
			t.Errorf("marshalled JSON carries the key %q with nothing set on it", provenance.FieldOnBehalfOf)
		}
		if _, present := m[provenance.FieldTraceparent]; present {
			t.Errorf("marshalled JSON carries the key %q with nothing adopted into it", provenance.FieldTraceparent)
		}
	})
}

func TestMarshalJSONGainsExactlyOneKeyWhenATenantIsAdded(t *testing.T) {
	p := newProvenance(t)
	before := jsonKeys(t, p, "a root provenance")
	after := jsonKeys(t, mustTenant(t, p, "acme"), "the same provenance with a tenant")

	for k, v := range before {
		got, present := after[k]
		if !present {
			t.Errorf("adding a tenant removed the JSON key %q; adding information must never take any away", k)
			continue
		}
		if got != v {
			t.Errorf("adding a tenant changed the JSON key %q from %v to %v", k, v, got)
		}
	}
	if len(after) != len(before)+1 {
		t.Fatalf("marshalled JSON has %d keys without a tenant and %d with one, want exactly one more", len(before), len(after))
	}
	if after[provenance.FieldTenant] != "acme" {
		t.Errorf("the added JSON key %q is %v, want %q", provenance.FieldTenant, after[provenance.FieldTenant], "acme")
	}
}

func TestJSONRoundTripPreservesIdentityAndCarriage(t *testing.T) {
	actor := mustUser(t, "u-operator")
	subject := mustService(t, "billing")

	base := newProvenance(t).WithActor(actor)
	base = mustOnBehalfOf(t, base, subject)
	base = mustTenant(t, base, "acme-corp")
	base = base.Adopt(provenance.Adopted{Traceparent: validTraceparent})

	b, err := base.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON failed on a value this package constructed: %v", err)
	}

	var got provenance.Provenance
	if err := got.UnmarshalJSON(b); err != nil {
		t.Fatalf("UnmarshalJSON failed on bytes MarshalJSON produced: %v (bytes %q) - a stored record is our own bytes and must read back", err, b)
	}

	if got.Actor().Kind() != actor.Kind() || got.Actor().ID() != actor.ID() {
		t.Errorf("actor after a JSON round trip is kind %v id %q, want kind %v id %q - a lineage record that cannot say who acted is half an answer",
			got.Actor().Kind(), got.Actor().ID(), actor.Kind(), actor.ID())
	}
	if got.OnBehalfOf().Kind() != subject.Kind() || got.OnBehalfOf().ID() != subject.ID() {
		t.Errorf("on_behalf_of after a JSON round trip is kind %v id %q, want kind %v id %q",
			got.OnBehalfOf().Kind(), got.OnBehalfOf().ID(), subject.Kind(), subject.ID())
	}
	if got.Delegated() != base.Delegated() {
		t.Errorf("Delegated() is %v after a JSON round trip and %v before it", got.Delegated(), base.Delegated())
	}
	if got.Tenant() != base.Tenant() {
		t.Errorf("tenant after a JSON round trip is %q, want %q - the tenant says whose data was touched and it must survive storage", got.Tenant(), base.Tenant())
	}
	if got.Traceparent() != validTraceparent {
		t.Errorf("traceparent after a JSON round trip is %q, want %q - it is carried verbatim", got.Traceparent(), validTraceparent)
	}
	if got.IsZero() {
		t.Errorf("a provenance read back out of JSON reports itself as the zero value")
	}

	want := attrMap(t, base.Attrs())
	have := attrMap(t, got.Attrs())
	if len(want) != len(have) {
		t.Fatalf("a provenance renders %d fields before a JSON round trip and %d after: %v vs %v - what a record can say about itself must not change by being stored", len(want), len(have), keySet(want), keySet(have))
	}
	for k, v := range want {
		if have[k] != v {
			t.Errorf("field %q is %q before a JSON round trip and %q after", k, v, have[k])
		}
	}
}

func TestUnmarshalJSONFailsInternalOnBytesItCannotRead(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"truncated JSON", "{"},
		{"a JSON array where an object belongs", "[1,2,3]"},
		{"a bare string", `"request_id"`},
		{"empty input", ""},
	}
	for _, tc := range cases {
		t.Run("UnmarshalJSON rejects "+tc.name, func(t *testing.T) {
			var p provenance.Provenance
			err := p.UnmarshalJSON([]byte(tc.input))
			assertInternal(t, err, "UnmarshalJSON("+tc.input+")")
		})
	}
}

// ---------------------------------------------------------------------------
// Adopt: the string rules. Adopt returns no error - it takes each field
// independently and drops what it cannot use, field by field.
// ---------------------------------------------------------------------------

func TestAdoptKeepsAWellFormedTraceparentVerbatim(t *testing.T) {
	p := newProvenance(t)
	if p.Traceparent() != "" {
		t.Fatalf("a fresh provenance already carries the traceparent %q; traceparent is adopted and never minted", p.Traceparent())
	}

	q := p.Adopt(provenance.Adopted{Traceparent: validTraceparent})

	if q.Traceparent() != validTraceparent {
		t.Errorf("Traceparent() = %q, want %q byte for byte - cross-vendor correlation is what traceparent is for, and it is carried verbatim precisely because nothing here reads it", q.Traceparent(), validTraceparent)
	}
	if p.Traceparent() != "" {
		t.Errorf("Adopt changed the receiver, which now carries the traceparent %q; a provenance is immutable", p.Traceparent())
	}
}

func TestAdoptDropsAMalformedTraceparent(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"a value that is not a traceparent at all", "not-a-traceparent"},
		{"a traceparent missing its flags field", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7"},
		{"a trace id one hex digit short", "00-4bf92f3577b34da6a3ce929d0e0e473-00f067aa0ba902b7-01"},
		{"a span id one hex digit short", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b-01"},
		{"a non-hex character in the trace id", "00-4bf92f3577b34da6a3ce929d0e0e473g-00f067aa0ba902b7-01"},
		{"a non-hex version field", "zz-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
		{"a well-formed traceparent with a newline appended", validTraceparent + "\nlevel=error msg=forged"},
		{"a value longer than MaxIDLength", strings.Repeat("0", provenance.MaxIDLength+1)},
	}
	for _, tc := range cases {
		t.Run("Adopt drops "+tc.name, func(t *testing.T) {
			q := newProvenance(t).Adopt(provenance.Adopted{Traceparent: tc.input})
			if q.Traceparent() != "" {
				t.Errorf("Traceparent() = %q after adopting %q; traceparent gets its own shape check because the general charset rule would accept a malformed one and a tracing backend then discards the span with nothing to point at",
					q.Traceparent(), tc.input)
			}
		})
	}
}

func TestAdoptDropsAnIdentifierThatIsNotOurOwnFormat(t *testing.T) {
	ids := mintedIDStrings(t, 1)
	cases := []struct {
		name  string
		input string
	}{
		{"a value that does not parse as one of our identifiers", "not-an-id"},
		{"the empty string, which is below the one character minimum", ""},
		{"a value carrying a newline, the log-forging character", ids[0] + "\nlevel=error"},
		{"a value carrying an angle bracket", "<" + ids[0] + ">"},
		{"a value carrying a double quote", `"` + ids[0] + `"`},
		{"a value carrying an ANSI escape", ids[0] + "\x1b[31m"},
		{"a value carrying whitespace", ids[0] + " "},
		{"a value one character past MaxIDLength", strings.Repeat("a", provenance.MaxIDLength+1)},
	}
	for _, tc := range cases {
		t.Run("Adopt drops a correlation that is "+tc.name, func(t *testing.T) {
			p := newProvenance(t)
			q := p.Adopt(provenance.Adopted{Correlation: tc.input})
			if q.Correlation() != p.Correlation() {
				t.Errorf("adopting the correlation %q changed the correlation this process minted; a foreign system that does not speak our format cannot join our chain, because adopting a stranger's correlation makes the grouping key of our audit trail caller-controlled", tc.input)
			}
		})
		t.Run("Adopt drops a causation that is "+tc.name, func(t *testing.T) {
			p := newProvenance(t)
			q := p.Adopt(provenance.Adopted{Causation: tc.input})
			if q.Causation() != p.Causation() {
				t.Errorf("adopting the causation %q changed the causation; an adopted value that does not parse is dropped, not used", tc.input)
			}
		})
	}
}

func TestAdoptTakesEachFieldIndependently(t *testing.T) {
	ids := mintedIDStrings(t, 2)

	t.Run("a bad correlation does not discard a good traceparent", func(t *testing.T) {
		p := newProvenance(t)
		q := p.Adopt(provenance.Adopted{
			Correlation: "not-an-id",
			Traceparent: validTraceparent,
		})
		if q.Traceparent() != validTraceparent {
			t.Errorf("Traceparent() = %q, want %q - Adopt takes each field independently and drops only what it cannot use, so one unusable field must not throw away a usable one", q.Traceparent(), validTraceparent)
		}
		if q.Correlation() != p.Correlation() {
			t.Errorf("the unparseable correlation was not dropped; the minted correlation must survive it")
		}
	})

	t.Run("a bad traceparent does not discard a good correlation", func(t *testing.T) {
		q := newProvenance(t).Adopt(provenance.Adopted{
			Correlation: ids[0],
			Traceparent: "not-a-traceparent",
		})
		if q.Correlation().String() != ids[0] {
			t.Errorf("Correlation() = %q, want %q - a malformed traceparent must not cost us the chain identifier that did parse", q.Correlation().String(), ids[0])
		}
		if q.Traceparent() != "" {
			t.Errorf("Traceparent() = %q; the malformed value must be dropped", q.Traceparent())
		}
	})

	t.Run("a bad causation does not discard a good correlation or traceparent", func(t *testing.T) {
		q := newProvenance(t).Adopt(provenance.Adopted{
			Correlation: ids[0],
			Causation:   "acme\x00corp",
			Traceparent: validTraceparent,
		})
		if q.Correlation().String() != ids[0] {
			t.Errorf("Correlation() = %q, want %q - each field is taken independently", q.Correlation().String(), ids[0])
		}
		if q.Traceparent() != validTraceparent {
			t.Errorf("Traceparent() = %q, want %q - each field is taken independently", q.Traceparent(), validTraceparent)
		}
	})

	t.Run("adopting nothing changes nothing", func(t *testing.T) {
		p := newProvenance(t)
		q := p.Adopt(provenance.Adopted{})
		if q.Traceparent() != "" {
			t.Errorf("Traceparent() = %q after adopting an empty Adopted; there was nothing to adopt", q.Traceparent())
		}
		if q.Correlation() != p.Correlation() || q.Causation() != p.Causation() {
			t.Errorf("adopting an empty Adopted changed the identifiers this process minted")
		}
		before := attrMap(t, p.Attrs())
		after := attrMap(t, q.Attrs())
		if len(before) != len(after) {
			t.Errorf("adopting an empty Adopted changed the rendered field set from %v to %v", keySet(before), keySet(after))
		}
	})

	t.Run("a dropped field leaves no empty field behind", func(t *testing.T) {
		q := newProvenance(t).Adopt(provenance.Adopted{
			Correlation: "not-an-id",
			Causation:   "not-an-id",
			Traceparent: "not-a-traceparent",
		})
		m := attrMap(t, q.Attrs())
		if _, present := m[provenance.FieldTraceparent]; present {
			t.Errorf("a dropped traceparent still rendered as %q = %q; an absent field is omitted, never emitted empty", provenance.FieldTraceparent, m[provenance.FieldTraceparent])
		}
		for k, v := range m {
			if v == "" {
				t.Errorf("field %q rendered empty after a failed adoption", k)
			}
		}
	})
}

func TestAdoptedIdentifiersAreAcceptedAtTheirOwnFormat(t *testing.T) {
	ids := mintedIDStrings(t, 2)
	p := newProvenance(t)

	q := p.Adopt(provenance.Adopted{Correlation: ids[0], Causation: ids[1]})

	if q.Correlation().String() != ids[0] {
		t.Errorf("Correlation() = %q, want %q - a value that parses as one of our own identifiers is exactly what Adopt is for", q.Correlation().String(), ids[0])
	}
	if q.Causation().String() != ids[1] {
		t.Errorf("Causation() = %q, want %q", q.Causation().String(), ids[1])
	}
}

// ---------------------------------------------------------------------------
// Provenance.IsZero
// ---------------------------------------------------------------------------

func TestTheZeroProvenanceIsZeroAndAConstructedOneIsNot(t *testing.T) {
	var zero provenance.Provenance
	if !zero.IsZero() {
		t.Errorf("the zero Provenance reports IsZero() == false; a caller cannot then tell a record that never had a scope opened from one that did")
	}
	if newProvenance(t).IsZero() {
		t.Errorf("a provenance returned by New reports IsZero() == true; it carries a minted request and a stated origin")
	}
}

// INFERENCE: the specification does not describe the zero Provenance's
// rendering. It does say request_id="" is not a state this package can produce,
// and a Nil request identifier rendered as a field is the same claim in a
// different spelling: that a request existed.
func TestTheZeroProvenanceClaimsNothingItCannotSupport(t *testing.T) {
	var zero provenance.Provenance
	attrs := zero.Attrs()
	m := attrMap(t, attrs)

	if _, present := m[provenance.FieldRequest]; present {
		t.Errorf("the zero Provenance emitted %q = %q; no request was ever minted, and a field here claims one existed", provenance.FieldRequest, m[provenance.FieldRequest])
	}
	if _, present := m[provenance.FieldCorrelation]; present {
		t.Errorf("the zero Provenance emitted %q = %q; there is no chain to belong to", provenance.FieldCorrelation, m[provenance.FieldCorrelation])
	}
	for _, a := range attrs {
		if a.Value.String() == "" {
			t.Errorf("the zero Provenance emitted %q with an empty value; absent is not empty", a.Key)
		}
	}
}
