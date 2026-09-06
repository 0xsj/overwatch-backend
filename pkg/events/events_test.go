package events_test

import (
	"strings"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

func fixtures(t *testing.T) (*id.Sequence, clock.System, provenance.Provenance) {
	t.Helper()
	m := id.NewSequence(time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC))
	return m, clock.System{}, provenance.New(provenance.OriginRequest, m)
}

func TestANameIsNamespacedOrItIsNotAName(t *testing.T) {
	good := []string{"identity.account.created", "audit.entry_written", "a.b", "x1._9"}
	bad := []string{
		"", "identity", "Identity.Account.Created", "identity..created",
		".created", "identity.", "identity.account-created", "identity.account created",
	}
	for _, s := range good {
		if !events.ValidName(s) {
			t.Errorf("%q was refused; lowercase dot-separated segments of [a-z0-9_] are the rule", s)
		}
	}
	for _, s := range bad {
		if events.ValidName(s) {
			t.Errorf("%q was accepted; a misspelled name is indistinguishable from a new kind of event", s)
		}
	}
	// AMENDED 2026-09-06, custody 0010 M01-M03: this read
	// `make([]byte, events.MaxNameLength+1)`, so the input moved when the
	// constant moved and the test could never observe the constant moving.
	// Literals, both sides of the boundary, and one line that notices a change.
	if !events.ValidName("a." + strings.Repeat("b", 126)) {
		t.Error("128 characters is the documented limit and must be accepted")
	}
	if events.ValidName("a." + strings.Repeat("b", 127)) {
		t.Error("129 characters is one past the documented limit and must be refused")
	}
	if events.MaxNameLength != 128 {
		t.Errorf("MaxNameLength is %d; the cases above are written against 128 deliberately, so this line is where a change is noticed rather than absorbed", events.MaxNameLength)
	}
}

func TestNewRefusesWhatASubscriberCouldNotActOn(t *testing.T) {
	m, c, p := fixtures(t)
	var zero provenance.Provenance

	if _, err := events.New(m, c, "nonamespace", p, nil); !errors.IsKind(err, errors.Internal) {
		t.Errorf("a name with one segment was accepted: %v", err)
	}
	if _, err := events.New(m, c, "identity.account.created", zero, nil); !errors.IsKind(err, errors.Internal) {
		t.Errorf("an event with no provenance was accepted: %v — nothing can reconstruct what caused it later", err)
	}
	if _, err := events.New(m, c, "identity.account.created", p, make(chan int)); err == nil {
		t.Error("a payload that cannot encode was accepted")
	}
}

func TestAnEventCarriesTheWholeChainAndNotACopyOfSomeFields(t *testing.T) {
	m, c, _ := fixtures(t)
	root := provenance.New(provenance.OriginRequest, m)
	child, err := root.Derive(m)
	if err != nil {
		t.Fatal(err)
	}

	e, err := events.New(m, c, "identity.account.created", child, map[string]string{"email": "a@b.c"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Provenance != child {
		t.Fatal("the provenance was not carried verbatim")
	}
	if e.Provenance.Causation() != root.Request() {
		t.Error("causation was lost; a subscriber months later cannot answer what caused this")
	}
	if e.Provenance.Correlation() != root.Correlation() {
		t.Error("correlation was lost")
	}
	if e.ID.IsZero() || e.OccurredAt.IsZero() {
		t.Error("an event with no id cannot be deduplicated, and delivery is at-least-once")
	}
}

func TestTheNamespaceIsTheFirstSegment(t *testing.T) {
	m, c, p := fixtures(t)
	e, _ := events.New(m, c, "identity.account.created", p, nil)
	if e.Namespace() != "identity" {
		t.Errorf("namespace = %q; it is the facet a client groups by", e.Namespace())
	}
}

func TestAPayloadRoundTrips(t *testing.T) {
	m, c, p := fixtures(t)
	type account struct {
		Email string `json:"email"`
		Org   string `json:"org"`
	}
	want := account{Email: "sj@example.com", Org: "org_1"}
	e, err := events.New(m, c, "identity.account.created", p, want)
	if err != nil {
		t.Fatal(err)
	}
	var got account
	if err := e.Into(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	empty, _ := events.New(m, c, "identity.account.created", p, nil)
	if err := empty.Into(&got); !errors.IsKind(err, errors.Internal) {
		t.Errorf("decoding an absent payload gave %v; the publisher is in this repository, so it is ours", err)
	}
}

func TestNilDependenciesPanicWhereTheyAreWired(t *testing.T) {
	_, c, p := fixtures(t)
	m, _, _ := fixtures(t)
	for _, tc := range []struct {
		name string
		want string
		call func()
	}{
		{"a nil Minter", "nil Minter", func() { events.New(nil, c, "a.b", p, nil) }},
		{"a nil Clock", "nil Clock", func() { events.New(m, nil, "a.b", p, nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) { wantPanic(t, tc.want, tc.call) })
	}
}

// wantPanic asserts not merely that something panicked, but that it panicked
// with the guard being tested. `recover() != nil` cannot tell a deliberate guard
// from a nil dereference two statements later, so it passes when the guard is
// deleted — measured on custody 0010 (M27/M28) and 0014.
func wantPanic(t *testing.T, contains string, call func()) {
	t.Helper()
	defer func() {
		v := recover()
		if v == nil {
			t.Errorf("did not panic; wanted the guard mentioning %q", contains)
			return
		}
		s, ok := v.(string)
		if !ok || !strings.Contains(s, contains) {
			t.Errorf("panicked with %v (%T); wanted the guard mentioning %q — a deref two statements later passes a bare recover() check", v, v, contains)
		}
	}()
	call()
}
