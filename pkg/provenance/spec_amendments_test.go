package provenance_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// amFixedTime seeds the deterministic sequence minter used throughout this
// file. Fixed rather than time.Now() so a failing case is reproducible.
var amFixedTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// amMinter is the Minter used wherever a test needs "some valid Provenance"
// and does not care which identifiers it holds.
var amMinter provenance.Minter = id.NewSequence(amFixedTime)

// amValidTraceparent is the well-formed example given verbatim by the
// specification's own ASCII-art shape description, not invented here.
const amValidTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

// amConstMinter always returns the same identifier. Used to build a family of
// Provenance values that are forced to agree on request/correlation/causation
// so that a JSON diff between two of them isolates the depth field alone,
// without assuming anything about the wire's key names.
type amConstMinter struct{ value id.ID }

func (m amConstMinter) NewID() id.ID { return m.value }

func amNewProvenance(o provenance.Origin) provenance.Provenance {
	return provenance.New(o, amMinter)
}

func amMutate(s string, idx int, b byte) string {
	bs := []byte(s)
	bs[idx] = b
	return string(bs)
}

// amFindSoleDifferingKey compares two decoded JSON documents that are known,
// by construction, to differ in exactly one field, and returns that field's
// key. It assumes nothing about key names; if the construction did not in
// fact isolate a single differing field, it fails loudly rather than guessing
// which key is the depth key.
func amFindSoleDifferingKey(t *testing.T, a, b map[string]json.RawMessage) string {
	t.Helper()
	seen := map[string]struct{}{}
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	var differing []string
	for k := range seen {
		va, aok := a[k]
		vb, bok := b[k]
		if aok != bok || string(va) != string(vb) {
			differing = append(differing, k)
		}
	}
	if len(differing) != 1 {
		t.Fatalf("could not identify the depth key in the provenance JSON wire format: two provenance values constructed to differ only in depth produced %d differing key(s) (%v), not exactly one; the wire shape could not be identified by this technique", len(differing), differing)
	}
	return differing[0]
}

// amDepthOverflowValue rewrites a raw JSON scalar to a new integer value,
// preserving whether the original was a quoted string or a bare number, since
// the specification does not state which encoding the depth field uses on
// the wire.
func amDepthOverflowValue(raw json.RawMessage, depth int) json.RawMessage {
	s := string(raw)
	if len(s) > 0 && s[0] == '"' {
		return json.RawMessage(strconv.Quote(strconv.Itoa(depth)))
	}
	return json.RawMessage(strconv.Itoa(depth))
}

// ---------------------------------------------------------------------------
// 1. traceparent's shape
// ---------------------------------------------------------------------------

func TestAmendTraceparentShapeIsPreciselyEnforced(t *testing.T) {
	base := amValidTraceparent

	cases := []struct {
		name         string
		value        string
		wantAccepted bool
	}{
		{"a well-formed traceparent is accepted", base, true},
		{"total length shorter than 55 characters is rejected", base[:54], false},
		{"total length longer than 55 characters is rejected", base + "0", false},
		{"a non-hyphen byte at index 2 is rejected", amMutate(base, 2, '0'), false},
		{"a non-hyphen byte at index 35 is rejected", amMutate(base, 35, '0'), false},
		{"a non-hyphen byte at index 52 is rejected", amMutate(base, 52, '0'), false},
		{"uppercase hex (A-F) among the hex bytes is rejected", amMutate(base, 3, 'A'), false},
		{"version ff is rejected, since it is reserved by the standard", amMutate(amMutate(base, 0, 'f'), 1, 'f'), false},
		{"an all-zero trace id is rejected", base[:3] + strings.Repeat("0", 32) + base[35:], false},
		{"an all-zero parent id is rejected", base[:36] + strings.Repeat("0", 16) + base[52:], false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := amNewProvenance(provenance.OriginRequest)
			adopted := p.Adopt(provenance.Adopted{Traceparent: c.value})
			got := adopted.Traceparent()

			if c.wantAccepted {
				if got != c.value {
					t.Errorf("a well-formed traceparent must be adopted verbatim (it is carried, never re-minted); got %q, want %q", got, c.value)
				}
				return
			}

			if got != "" {
				t.Errorf("an adopted traceparent that does not validate must be dropped, not accepted (a broken trace link is the cheaper failure than refusing the request); input %q was accepted as %q", c.value, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 2. WithOnBehalfOf's three refusals
// ---------------------------------------------------------------------------

func TestAmendWithOnBehalfOfRefusesThreeDistinctShapes(t *testing.T) {
	alice, err := provenance.User("alice")
	if err != nil {
		t.Fatalf("fixture: provenance.User(%q) must succeed to build this test's actors: %v", "alice", err)
	}
	bob, err := provenance.User("bob")
	if err != nil {
		t.Fatalf("fixture: provenance.User(%q) must succeed to build this test's actors: %v", "bob", err)
	}

	base := amNewProvenance(provenance.OriginRequest)

	t.Run("the delegate is anonymous: acting for nobody is not a delegation", func(t *testing.T) {
		p := base.WithActor(alice)
		if _, err := p.WithOnBehalfOf(provenance.Anonymous()); err == nil {
			t.Error("WithOnBehalfOf(Anonymous()) must be refused, but it was accepted")
		}
	})

	t.Run("the actor is anonymous: nobody cannot act for somebody", func(t *testing.T) {
		// base's actor was never set with WithActor, so it carries the zero
		// (anonymous) actor.
		if _, err := base.WithOnBehalfOf(bob); err == nil {
			t.Error("WithOnBehalfOf must be refused when the acting actor is anonymous, but it was accepted")
		}
	})

	t.Run("actor and delegate are the same actor: acting for yourself is not a delegation", func(t *testing.T) {
		p := base.WithActor(alice)
		if _, err := p.WithOnBehalfOf(alice); err == nil {
			t.Error("WithOnBehalfOf must be refused when the delegate is the acting actor itself, but it was accepted")
		}
	})

	t.Run("a genuine delegation between two distinct named actors is accepted", func(t *testing.T) {
		p := base.WithActor(alice)
		got, err := p.WithOnBehalfOf(bob)
		if err != nil {
			t.Fatalf("a delegation between two distinct, non-anonymous actors must be accepted, not refused: %v", err)
		}
		if !got.Delegated() {
			t.Error("a Provenance that accepted WithOnBehalfOf must report Delegated() == true")
		}
		if got.OnBehalfOf() != bob {
			t.Error("OnBehalfOf() must return the delegate that was set")
		}
	})
}

// ---------------------------------------------------------------------------
// 3. User, Service and System refuse to build an actor of kind KindAnonymous
// ---------------------------------------------------------------------------

// INFERENCE: none of User, Service or System take a Kind argument, so the
// specification's claim that they "refuse to build an actor of kind
// KindAnonymous" cannot be exercised by requesting that kind directly. The
// only actor whose Kind() is the zero value KindAnonymous is the zero Actor,
// and the only input available to these constructors that could degenerate
// into that shape is an empty identifier. This test assumes an empty string
// is the trigger the specification is describing. That assumption is not
// stated in the specification and could be wrong; if it is, this test
// exercises the wrong input rather than exercising nothing, and should not be
// read as confirming or refuting the documented rule.
func TestAmendNamedConstructorsRefuseAnEmptyIdentifierRatherThanYieldAnonymousKind(t *testing.T) {
	cases := []struct {
		name  string
		build func() (provenance.Actor, error)
	}{
		{"User refuses an empty identifier", func() (provenance.Actor, error) { return provenance.User("") }},
		{"Service refuses an empty identifier", func() (provenance.Actor, error) { return provenance.Service("") }},
		{"System refuses an empty identifier", func() (provenance.Actor, error) { return provenance.System("") }},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, err := c.build()
			if err == nil {
				t.Errorf("expected a refusal: an empty identifier would otherwise yield an actor indistinguishable from Anonymous (a named anonymous actor would make that identity ambiguous), got Actor{Kind=%v, ID=%q} with a nil error", a.Kind(), a.ID())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 4. Retry saturates rather than wrapping
// ---------------------------------------------------------------------------

func TestAmendRetrySaturatesAtUint16MaxRatherThanWrapping(t *testing.T) {
	const max = ^uint16(0)

	p := amNewProvenance(provenance.OriginRequest)
	for i := 0; i < 70000; i++ {
		p = p.Retry()
	}

	if got := p.Attempt(); got != max {
		t.Fatalf("after far exceeding the uint16 range via repeated Retry, Attempt() must saturate at %d rather than wrapping; got %d", max, got)
	}

	after := p.Retry()
	if got := after.Attempt(); got != max {
		t.Errorf("Retry() at the maximum attempt must remain at %d, not wrap to a smaller value (wrapping would report a runaway redelivery as a first attempt, the most misleading value the field can hold); got %d", max, got)
	}
}

// ---------------------------------------------------------------------------
// 5. Root is depth == 0 AND correlation == request
// ---------------------------------------------------------------------------

func TestAmendRootRequiresBothZeroDepthAndUnadoptedCorrelation(t *testing.T) {
	freshRoot := amNewProvenance(provenance.OriginRequest)

	if freshRoot.Depth() != 0 {
		t.Fatalf("fixture assumption failed: a freshly minted Provenance must be at depth 0, got %d", freshRoot.Depth())
	}
	if freshRoot.Correlation() != freshRoot.Request() {
		t.Fatalf("fixture assumption failed: at an origin, correlation must equal request (the chain's root and the chain are the same thing named twice); got correlation=%s request=%s", freshRoot.Correlation(), freshRoot.Request())
	}
	if !freshRoot.Root() {
		t.Error("a depth-0 value whose correlation equals its request must report Root() == true")
	}

	upstreamCorrelation := amMinter.NewID().String()
	adopted := freshRoot.Adopt(provenance.Adopted{Correlation: upstreamCorrelation})

	if adopted.Depth() != 0 {
		t.Fatalf("fixture assumption failed: Adopt must not change depth, got %d", adopted.Depth())
	}
	if adopted.Correlation().String() != upstreamCorrelation {
		t.Fatalf("fixture assumption failed: the upstream correlation %q was not adopted (got %s); cannot exercise Root() with an adopted correlation", upstreamCorrelation, adopted.Correlation())
	}

	if adopted.Root() {
		t.Error("a depth-0 value that has adopted an upstream correlation must not report Root(): it opened this process's work, it did not open the chain")
	}
}

// ---------------------------------------------------------------------------
// 6. UnmarshalJSON refuses a depth past MaxDepth
// ---------------------------------------------------------------------------

func TestAmendUnmarshalJSONRefusesDepthPastMaxDepth(t *testing.T) {
	fixed := amMinter.NewID()
	minter := amConstMinter{value: fixed}

	grandparent := provenance.New(provenance.OriginRequest, minter)
	p1, err := grandparent.Derive(minter)
	if err != nil {
		t.Fatalf("fixture: Derive must succeed well under MaxDepth: %v", err)
	}
	p2, err := p1.Derive(minter)
	if err != nil {
		t.Fatalf("fixture: Derive must succeed well under MaxDepth: %v", err)
	}

	b1, err := json.Marshal(p1)
	if err != nil {
		t.Fatalf("fixture: json.Marshal(p1) must succeed: %v", err)
	}
	b2, err := json.Marshal(p2)
	if err != nil {
		t.Fatalf("fixture: json.Marshal(p2) must succeed: %v", err)
	}

	var m1, m2 map[string]json.RawMessage
	if err := json.Unmarshal(b1, &m1); err != nil {
		t.Fatalf("fixture: decoding p1's own JSON into a map must succeed: %v", err)
	}
	if err := json.Unmarshal(b2, &m2); err != nil {
		t.Fatalf("fixture: decoding p2's own JSON into a map must succeed: %v", err)
	}

	depthKey := amFindSoleDifferingKey(t, m1, m2)
	m2[depthKey] = amDepthOverflowValue(m2[depthKey], provenance.MaxDepth+1)

	corrupted, err := json.Marshal(m2)
	if err != nil {
		t.Fatalf("fixture: re-marshaling the modified wire map must succeed: %v", err)
	}

	var out provenance.Provenance
	err = out.UnmarshalJSON(corrupted)
	if err == nil {
		t.Fatal("UnmarshalJSON must refuse a document whose depth exceeds MaxDepth: a chain that deep is a cycle in every case anyone has produced, and accepting it launders a broken record into a trusted one")
	}
	if !errors.IsKind(err, errors.Internal) {
		t.Errorf("a stored record refused by UnmarshalJSON must report errors.Internal, since these are our own bytes and a violation is corruption rather than input; got kind %v", errors.KindOf(err))
	}
}
