package events_test

// This file tests github.com/0xsj/overwatch-backend/pkg/events against its
// documentation only. Every top-level identifier is prefixed "Spec"/"spec" to
// avoid collision with the sibling test file already present in this package.

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// ---- fixtures -------------------------------------------------------------

type specFakeClock struct {
	at time.Time
}

func (c specFakeClock) Now() time.Time { return c.at }

type specPayload struct {
	Foo   string `json:"foo"`
	Count int    `json:"count"`
}

func specNewProvenance(m *id.Sequence) provenance.Provenance {
	return provenance.New(provenance.OriginRequest, m)
}

type specFakePublisher struct {
	got []events.Event
}

func (p *specFakePublisher) Publish(ctx context.Context, evs ...events.Event) error {
	p.got = append(p.got, evs...)
	return nil
}

func specHandlerFunc(ctx context.Context, e events.Event) error {
	return nil
}

// ---- MaxNameLength ----------------------------------------------------------

func TestSpecMaxNameLengthIsOneHundredTwentyEight(t *testing.T) {
	if events.MaxNameLength != 128 {
		t.Fatalf("MaxNameLength must be 128 per the document; got %d", events.MaxNameLength)
	}
}

// ---- ValidName: every clause, both directions -----------------------------

func TestSpecValidName(t *testing.T) {
	name128 := strings.Repeat("a", 126) + ".a" // len == 128
	name129 := strings.Repeat("a", 127) + ".a" // len == 129

	tests := []struct {
		claim string
		in    string
		want  bool
	}{
		{"three lowercase dot-separated segments is accepted", "identity.account.created", true},
		{"exactly two segments is accepted", "a.b", true},
		{"digits and underscores are allowed within a segment", "a1.b_2.c3", true},
		{"more than three segments is accepted", "a.b.c.d.e", true},
		{"a name at exactly MaxNameLength is accepted (inferred: inclusive boundary)", name128, true},

		{"the empty string has no segment and is refused", "", false},
		{"a single segment with no dot is refused", "identity", false},
		{"an uppercase letter in the first segment is refused", "Identity.account.created", false},
		{"an uppercase letter in a later segment is refused", "identity.Account.created", false},
		{"a hyphen is outside the allowed alphabet", "identity.account-created", false},
		{"an empty segment between two dots is refused", "identity..created", false},
		{"a leading dot produces an empty first segment and is refused", ".identity.account", false},
		{"a trailing dot produces an empty last segment and is refused", "identity.account.", false},
		{"a space is outside the allowed alphabet", "identity. account.created", false},
		{"a non-ASCII letter is outside the allowed alphabet", "idéntity.account.created", false},
		{"one byte past MaxNameLength is refused (inferred: exclusive boundary)", name129, false},
	}

	for _, tt := range tests {
		t.Run(tt.claim, func(t *testing.T) {
			if got := events.ValidName(tt.in); got != tt.want {
				t.Fatalf("ValidName(%q) = %v, want %v because: %s", tt.in, got, tt.want, tt.claim)
			}
		})
	}
}

// ---- Namespace is the first segment ----------------------------------------

func TestSpecNamespaceReturnsTheFirstSegment(t *testing.T) {
	tests := []struct {
		claim string
		name  string
		want  string
	}{
		{"the domain is the segment before the first dot", "identity.account.created", "identity"},
		{"two segments still yields the first as the namespace", "a.b", "a"},
		{"a name with no dot returns the whole name as its own namespace", "standalone", "standalone"},
		{"an empty name yields an empty namespace", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.claim, func(t *testing.T) {
			ev := events.Event{Name: tt.name}
			if got := ev.Namespace(); got != tt.want {
				t.Fatalf("Namespace() on Name %q must return the first dot-separated segment: got %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// ---- IsZero -----------------------------------------------------------------

func TestSpecEventIsZeroForTheZeroValueAndNotForAMintedEvent(t *testing.T) {
	var zero events.Event
	if !zero.IsZero() {
		t.Fatalf("the zero-value Event must report IsZero() true")
	}

	m := id.NewSequence(time.Unix(1_700_001_000, 0))
	c := specFakeClock{at: time.Unix(1_700_001_001, 0)}
	p := specNewProvenance(m)

	ev, err := events.New(m, c, "identity.account.created", p, specPayload{Foo: "a"})
	if err != nil {
		t.Fatalf("setup: New with a valid name, provenance and payload must succeed; got error %v", err)
	}
	if ev.IsZero() {
		t.Fatalf("a successfully minted Event must not report IsZero() true")
	}

	refused, err := events.New(m, c, "not-a-name", p, specPayload{Foo: "a"})
	if err == nil {
		t.Fatalf("setup: New with an invalid name must fail")
	}
	if !refused.IsZero() {
		t.Fatalf("the Event returned when New refuses its input must report IsZero() true")
	}
}

// ---- New refuses what a subscriber could not act on ------------------------

func TestSpecNewRefusesAnUnnamedOrMisnamedEvent(t *testing.T) {
	m := id.NewSequence(time.Unix(1_700_000_700, 0))
	c := specFakeClock{at: time.Unix(1_700_000_701, 0)}
	p := specNewProvenance(m)

	ev, err := events.New(m, c, "not-a-namespaced-name", p, specPayload{Foo: "a"})

	t.Run("returns a non-nil error", func(t *testing.T) {
		if err == nil {
			t.Fatalf("New must refuse a name that is not a name, because a misspelled name is indistinguishable from a new kind of event; got nil error")
		}
	})
	t.Run("returns the zero Event", func(t *testing.T) {
		if !reflect.DeepEqual(ev, events.Event{}) {
			t.Fatalf("New must not mint anything when it refuses the name; got %+v, want the zero Event", ev)
		}
	})
}

func TestSpecNewRefusesAProvenanceThatSaysNothingCausedThis(t *testing.T) {
	m := id.NewSequence(time.Unix(1_700_000_800, 0))
	c := specFakeClock{at: time.Unix(1_700_000_801, 0)}
	var zeroProv provenance.Provenance

	ev, err := events.New(m, c, "identity.account.created", zeroProv, specPayload{Foo: "a"})

	t.Run("returns a non-nil error", func(t *testing.T) {
		if err == nil {
			t.Fatalf("New must refuse the zero Provenance, since it says nothing caused this event; got nil error")
		}
	})
	t.Run("returns the zero Event", func(t *testing.T) {
		if !reflect.DeepEqual(ev, events.Event{}) {
			t.Fatalf("New must not mint anything when it refuses the provenance; got %+v, want the zero Event", ev)
		}
	})
}

func TestSpecNewRefusesAPayloadThatWillNotEncode(t *testing.T) {
	m := id.NewSequence(time.Unix(1_700_000_900, 0))
	c := specFakeClock{at: time.Unix(1_700_000_901, 0)}
	p := specNewProvenance(m)

	ev, err := events.New(m, c, "identity.account.created", p, make(chan int))

	t.Run("returns a non-nil error", func(t *testing.T) {
		if err == nil {
			t.Fatalf("New must refuse a payload that will not encode to JSON; got nil error")
		}
	})
	t.Run("returns the zero Event", func(t *testing.T) {
		if !reflect.DeepEqual(ev, events.Event{}) {
			t.Fatalf("New must not mint anything when the payload will not encode; got %+v, want the zero Event", ev)
		}
	})
}

func TestSpecNewAcceptsANilPayload(t *testing.T) {
	// INFERENCE: the document does not explicitly say a nil payload is
	// accepted; it says New refuses "a payload that will not encode". Since
	// json.Marshal(nil) succeeds (JSON null), a nil payload is, by that
	// reading, encodable and should be accepted. Flagged as an inference.
	m := id.NewSequence(time.Unix(1_700_001_100, 0))
	c := specFakeClock{at: time.Unix(1_700_001_101, 0)}
	p := specNewProvenance(m)

	ev, err := events.New(m, c, "identity.account.created", p, nil)
	if err != nil {
		t.Fatalf("New with a nil payload is expected (inferred) to succeed because nil encodes to JSON null without error; got error %v", err)
	}
	if ev.IsZero() {
		t.Fatalf("a successful New must not return the zero Event")
	}
}

// ---- New's contract with its two dependencies -------------------------------

func TestSpecNewStampsOccurredAtFromTheClock(t *testing.T) {
	m := id.NewSequence(time.Unix(1_700_000_600, 0))
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	c := specFakeClock{at: fixed}
	p := specNewProvenance(m)

	ev, err := events.New(m, c, "identity.account.created", p, specPayload{Foo: "a"})
	if err != nil {
		t.Fatalf("setup: New must succeed; got error %v", err)
	}
	if !ev.OccurredAt.Equal(fixed) {
		t.Fatalf("Event.OccurredAt must come from the injected Clock, not from wall-clock time: got %v, want %v", ev.OccurredAt, fixed)
	}
}

func TestSpecNewMintsANonZeroDeduplicationKey(t *testing.T) {
	m := id.NewSequence(time.Unix(1_700_000_500, 0))
	c := specFakeClock{at: time.Unix(1_700_000_501, 0)}
	p := specNewProvenance(m)

	ev, err := events.New(m, c, "identity.account.created", p, specPayload{Foo: "a"})
	if err != nil {
		t.Fatalf("setup: New must succeed; got error %v", err)
	}
	if ev.ID.IsZero() {
		t.Fatalf("a successfully minted event must carry a real deduplication key, not the zero id (fail-closed violation)")
	}
}

func TestSpecTwoMintedEventsHaveDistinctDeduplicationKeys(t *testing.T) {
	m := id.NewSequence(time.Unix(1_700_000_400, 0))
	c := specFakeClock{at: time.Unix(1_700_000_401, 0)}
	p := specNewProvenance(m)

	e1, err := events.New(m, c, "identity.account.created", p, specPayload{Foo: "a"})
	if err != nil {
		t.Fatalf("setup: first New must succeed; got error %v", err)
	}
	e2, err := events.New(m, c, "identity.account.created", p, specPayload{Foo: "a"})
	if err != nil {
		t.Fatalf("setup: second New must succeed; got error %v", err)
	}

	if e1.ID == e2.ID {
		t.Fatalf("two separately minted events must not share a deduplication key even with identical name, provenance and payload (a shared id would make two distinct occurrences look like one): both had id %v", e1.ID)
	}
}

// ---- Provenance is carried as the value itself ------------------------------

func TestSpecEventCarriesTheProvenanceValueItselfNotACopyOfFields(t *testing.T) {
	m := id.NewSequence(time.Unix(1_700_000_300, 0))
	c := specFakeClock{at: time.Unix(1_700_000_301, 0)}

	actor, err := provenance.User("user-1")
	if err != nil {
		t.Skipf("could not construct a User actor to attach to provenance; actor construction rules are not a claim of pkg/events: %v", err)
	}
	p := specNewProvenance(m).WithActor(actor)

	ev, err := events.New(m, c, "identity.account.created", p, specPayload{Foo: "x"})
	if err != nil {
		t.Fatalf("setup: New with a valid name, non-zero provenance and encodable payload must succeed; got error %v", err)
	}

	if !reflect.DeepEqual(ev.Provenance, p) {
		t.Fatalf("Event.Provenance must be the same provenance.Provenance value given to New, not a copy of some of its fields: got %+v, want %+v", ev.Provenance, p)
	}

	// Cross-check via the accessors the document names explicitly: correlation,
	// causation, origin, depth, attempt and the actor.
	if ev.Provenance.Correlation() != p.Correlation() {
		t.Fatalf("Event.Provenance.Correlation() must match the given provenance's correlation id: got %v, want %v", ev.Provenance.Correlation(), p.Correlation())
	}
	if ev.Provenance.Causation() != p.Causation() {
		t.Fatalf("Event.Provenance.Causation() must match the given provenance's causation id: got %v, want %v", ev.Provenance.Causation(), p.Causation())
	}
	if ev.Provenance.Origin() != p.Origin() {
		t.Fatalf("Event.Provenance.Origin() must match the given provenance's origin: got %v, want %v", ev.Provenance.Origin(), p.Origin())
	}
	if ev.Provenance.Depth() != p.Depth() {
		t.Fatalf("Event.Provenance.Depth() must match the given provenance's depth: got %v, want %v", ev.Provenance.Depth(), p.Depth())
	}
	if ev.Provenance.Attempt() != p.Attempt() {
		t.Fatalf("Event.Provenance.Attempt() must match the given provenance's attempt count: got %v, want %v", ev.Provenance.Attempt(), p.Attempt())
	}
	if ev.Provenance.Actor().String() != p.Actor().String() {
		t.Fatalf("Event.Provenance.Actor() must match the given provenance's actor: got %v, want %v", ev.Provenance.Actor().String(), p.Actor().String())
	}
}

// ---- Into ---------------------------------------------------------------

func TestSpecIntoRoundTripsThePayload(t *testing.T) {
	m := id.NewSequence(time.Unix(1_700_000_100, 0))
	c := specFakeClock{at: time.Unix(1_700_000_101, 0)}
	p := specNewProvenance(m)
	want := specPayload{Foo: "bar", Count: 7}

	ev, err := events.New(m, c, "identity.account.created", p, want)
	if err != nil {
		t.Fatalf("setup: New with a valid name, provenance and encodable payload must succeed; got error %v", err)
	}

	var got specPayload
	if err := ev.Into(&got); err != nil {
		t.Fatalf("Into must decode a payload that New successfully encoded; got error %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a payload must round-trip through New and Into unchanged: got %+v, want %+v", got, want)
	}
}

func TestSpecIntoIsIdempotentAcrossCalls(t *testing.T) {
	m := id.NewSequence(time.Unix(1_700_000_200, 0))
	c := specFakeClock{at: time.Unix(1_700_000_201, 0)}
	p := specNewProvenance(m)
	want := specPayload{Foo: "baz", Count: 3}

	ev, err := events.New(m, c, "identity.account.created", p, want)
	if err != nil {
		t.Fatalf("setup: New must succeed; got error %v", err)
	}

	var first, second specPayload
	if err := ev.Into(&first); err != nil {
		t.Fatalf("first Into call failed: %v", err)
	}
	if err := ev.Into(&second); err != nil {
		t.Fatalf("second Into call failed: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Into must be idempotent: repeated decodes of the same immutable payload produced different results: first=%+v second=%+v", first, second)
	}
}

func TestSpecIntoOnAnEventWithNoPayloadAtAllReturnsAnError(t *testing.T) {
	// INFERENCE: the document flags "what Into does when there is no payload
	// at all" as worth attention but does not state the answer. We take "no
	// payload at all" to mean an Event whose Payload was never set (the zero
	// json.RawMessage), and assert the fail-closed reading: decoding nothing
	// is an error rather than a silent no-op that leaves the target
	// unchanged. This is an inference and may not match the implementation.
	var e events.Event
	var target specPayload
	if err := e.Into(&target); err == nil {
		t.Fatalf("Into on an Event with no payload at all is expected (inferred, fail-closed) to return an error; got nil")
	}
}

// ---- The two ports ------------------------------------------------------

func TestSpecHandlerMatchesTheDeclaredFunctionSignature(t *testing.T) {
	var h events.Handler = specHandlerFunc
	if err := h(context.Background(), events.Event{}); err != nil {
		t.Fatalf("a Handler wrapping a function with the documented signature (ctx, Event) error must be callable; got unexpected error %v", err)
	}
}

func TestSpecPublisherAcceptsAVariadicListOfEvents(t *testing.T) {
	fp := &specFakePublisher{}
	var pub events.Publisher = fp

	if err := pub.Publish(context.Background()); err != nil {
		t.Fatalf("Publish with zero events must be callable without error; got %v", err)
	}
	if len(fp.got) != 0 {
		t.Fatalf("Publish with zero events must not record any event; got %d recorded", len(fp.got))
	}

	e1 := events.Event{Name: "identity.account.created"}
	e2 := events.Event{Name: "identity.account.deleted"}
	if err := pub.Publish(context.Background(), e1, e2); err != nil {
		t.Fatalf("Publish must accept a variadic list of events without error; got %v", err)
	}
	if len(fp.got) != 2 {
		t.Fatalf("a variadic Publish call with two events must deliver exactly two events to the publisher: got %d, want 2", len(fp.got))
	}
}

// ---- Totality over closed sets from the widening appendix -----------------

func TestSpecNewIsTotalAcrossEveryProvenanceOrigin(t *testing.T) {
	// Order and membership as documented:
	// provenance.Origins = []Origin{OriginRequest, OriginSchedule, OriginReplay, OriginBackfill, OriginStartup}
	labels := []string{"OriginRequest", "OriginSchedule", "OriginReplay", "OriginBackfill", "OriginStartup"}
	if len(labels) != len(provenance.Origins) {
		t.Fatalf("the documented Origins set has %d members but this test names %d; the enumeration may have changed since this test was written", len(provenance.Origins), len(labels))
	}

	for i, origin := range provenance.Origins {
		t.Run(labels[i], func(t *testing.T) {
			m := id.NewSequence(time.Unix(int64(1_700_002_000+i), 0))
			c := specFakeClock{at: time.Unix(int64(1_700_002_001+i), 0)}
			p := provenance.New(origin, m)

			if _, err := events.New(m, c, "identity.account.created", p, specPayload{Foo: "a"}); err != nil {
				t.Fatalf("New must accept a provenance built from every documented Origin; origin %s produced error %v", labels[i], err)
			}
		})
	}
}

func TestSpecNewIsTotalAcrossEveryActorKind(t *testing.T) {
	type actorCase struct {
		claim string
		build func() (provenance.Actor, error)
	}
	cases := []actorCase{
		{"an Anonymous actor", func() (provenance.Actor, error) { return provenance.Anonymous(), nil }},
		{"a User actor", func() (provenance.Actor, error) { return provenance.User("user-1") }},
		{"a Service actor", func() (provenance.Actor, error) { return provenance.Service("svc-1") }},
		{"a System actor", func() (provenance.Actor, error) { return provenance.System("sys-1") }},
	}

	for i, tc := range cases {
		t.Run(tc.claim, func(t *testing.T) {
			actor, err := tc.build()
			if err != nil {
				t.Skipf("could not construct %s to attach to provenance; actor construction rules are not a claim of pkg/events: %v", tc.claim, err)
			}

			m := id.NewSequence(time.Unix(int64(1_700_003_000+i), 0))
			c := specFakeClock{at: time.Unix(int64(1_700_003_001+i), 0)}
			p := provenance.New(provenance.OriginRequest, m).WithActor(actor)

			if _, err := events.New(m, c, "identity.account.created", p, specPayload{Foo: "a"}); err != nil {
				t.Fatalf("New must accept a provenance carrying %s; got error %v", tc.claim, err)
			}
		})
	}
}
