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

	if _, err := events.New(m, c, "nonamespace", "account:a1", p, nil); !errors.IsKind(err, errors.Internal) {
		t.Errorf("a name with one segment was accepted: %v", err)
	}
	if _, err := events.New(m, c, "identity.account.created", "account:a1", zero, nil); !errors.IsKind(err, errors.Internal) {
		t.Errorf("an event with no provenance was accepted: %v — nothing can reconstruct what caused it later", err)
	}
	if _, err := events.New(m, c, "identity.account.created", "account:a1", p, make(chan int)); err == nil {
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

	e, err := events.New(m, c, "identity.account.created", "account:a1", child, map[string]string{"email": "a@b.c"})
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
	e, _ := events.New(m, c, "identity.account.created", "account:a1", p, nil)
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
	e, err := events.New(m, c, "identity.account.created", "account:a1", p, want)
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
	empty, _ := events.New(m, c, "identity.account.created", "account:a1", p, nil)
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
		{"a nil Minter", "nil Minter", func() { events.New(nil, c, "a.b", "account:a1", p, nil) }},
		{"a nil Clock", "nil Clock", func() { events.New(m, nil, "a.b", "account:a1", p, nil) }},
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

func TestASubjectIsRefusedUnlessItNamesAThing(t *testing.T) {
	m, c, p := fixtures(t)
	for _, tc := range []struct {
		name    string
		subject string
		want    bool
	}{
		{"kind and id", "account:0198f3c1", true},
		{"an underscore in the kind", "api_key:k1", true},
		{"digits in the kind", "s3:bucket", true},
		{"a uuid id", "workspace:0198f3c1-1c9e-7a3f-8000-8f2b1c4d5e6f", true},
		{"a colon inside the id", "target:host:example.com", true},
		{"empty", "", false},
		{"no colon", "account", false},
		{"no kind", ":0198f3c1", false},
		{"no id", "account:", false},
		{"an uppercase kind", "Account:a1", false},
		{"a hyphen in the kind", "api-key:k1", false},
		{"a space in the id", "account:a 1", false},
		{"too long", "account:" + strings.Repeat("x", events.MaxSubjectLength), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := events.ValidSubject(tc.subject); got != tc.want {
				t.Errorf("ValidSubject(%q) = %v, want %v", tc.subject, got, tc.want)
			}
			_, err := events.New(m, c, "identity.account.created", tc.subject, p, nil)
			if minted := err == nil; minted != tc.want {
				t.Errorf("New with subject %q: err = %v, want minted = %v", tc.subject, err, tc.want)
			}
		})
	}
}

func TestSubjectKindAndIDSplitWithoutTheCallerParsing(t *testing.T) {
	m, c, p := fixtures(t)
	e, err := events.New(m, c, "identity.session.started", "account:a1", p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if e.SubjectKind() != "account" || e.SubjectID() != "a1" {
		t.Errorf("split %q into %q / %q", e.Subject, e.SubjectKind(), e.SubjectID())
	}
	// Only the FIRST colon separates. An id that contains one is not truncated,
	// which is what lets a subject name a host or a URL.
	deep, err := events.New(m, c, "target.host.seen", "target:host:example.com", p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if deep.SubjectKind() != "target" || deep.SubjectID() != "host:example.com" {
		t.Errorf("split %q into %q / %q", deep.Subject, deep.SubjectKind(), deep.SubjectID())
	}
}

func TestWorkAndADecisionAreTheSameEnvelopeWithADifferentClaim(t *testing.T) {
	m, c, p := fixtures(t)

	work, err := events.New(m, c, "runner.run.started", "run:r119", p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if work.Decision {
		t.Error("events.New minted a decision")
	}

	// A person starting a run is WORK, not audit — the shortcut this guards
	// against is "the actor is a person, therefore audit".
	decision, err := events.NewDecision(m, c, "finding.judgement.set", "finding:f7", p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Decision {
		t.Error("events.NewDecision did not mint a decision")
	}

	// Everything else about the envelope is identical: a decision is refused for
	// the same reasons work is, and gains no exemption.
	if _, err := events.NewDecision(m, c, "not-a-name", "finding:f7", p, nil); err == nil {
		t.Error("NewDecision accepted a name New would refuse")
	}
	if _, err := events.NewDecision(m, c, "finding.judgement.set", "nosubject", p, nil); err == nil {
		t.Error("NewDecision accepted a subject New would refuse")
	}
}
