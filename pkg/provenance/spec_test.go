// Package provenance_test, causal half.
//
// These tests are written from the package documentation alone. No part of the
// implementation was read while writing them. Where a test encodes an
// interpretation rather than a stated rule, the site says so in a comment
// beginning "INFERENCE:" and names the alternative that was rejected.
//
// Naming: every test is TestCausalXxx, every other package-level identifier is
// causalXxx, because a second file in this same external test package is being
// written concurrently.
package provenance_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

var (
	causalEpoch    = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	causalAltEpoch = time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
)

// causalMinter is a Minter that counts calls and can be primed with exact
// identifiers, so a test can assert which value ended up in which field.
type causalMinter struct {
	seq    *id.Sequence
	canned []id.ID
	calls  int
}

func causalNewMinter() *causalMinter {
	return &causalMinter{seq: id.NewSequence(causalEpoch)}
}

func causalCannedMinter(ids ...id.ID) *causalMinter {
	return &causalMinter{seq: id.NewSequence(causalEpoch), canned: ids}
}

func (m *causalMinter) NewID() id.ID {
	m.calls++
	if len(m.canned) > 0 {
		v := m.canned[0]
		m.canned = m.canned[1:]
		return v
	}
	return m.seq.NewID()
}

// Both the real deterministic generator and the local double must satisfy
// Minter structurally; these lines fail at compile time if the interface drifts.
var (
	_ provenance.Minter = (*causalMinter)(nil)
	_ provenance.Minter = (*id.Sequence)(nil)
)

// causalMintN returns n distinct identifiers seeded from a given instant.
func causalMintN(at time.Time, n int) []id.ID {
	s := id.NewSequence(at)
	out := make([]id.ID, n)
	for i := range out {
		out[i] = s.NewID()
	}
	return out
}

func causalMustNew(t *testing.T, o provenance.Origin) (provenance.Provenance, *causalMinter) {
	t.Helper()
	m := causalNewMinter()
	p := provenance.New(o, m)
	if p.IsZero() {
		t.Fatalf("New(%v, minter) returned a zero Provenance: a listed origin must open a usable chain", o)
	}
	return p, m
}

func causalMustDerive(t *testing.T, p provenance.Provenance, m provenance.Minter) provenance.Provenance {
	t.Helper()
	c, err := p.Derive(m)
	if err != nil {
		t.Fatalf("Derive from depth %d returned %v: a derive that stays within MaxDepth (%d) must succeed", p.Depth(), err, provenance.MaxDepth)
	}
	return c
}

func causalMustDeriveFrom(t *testing.T, p provenance.Provenance, m provenance.Minter, cause id.ID) provenance.Provenance {
	t.Helper()
	c, err := p.DeriveFrom(m, cause)
	if err != nil {
		t.Fatalf("DeriveFrom from depth %d returned %v: a derive that stays within MaxDepth (%d) must succeed", p.Depth(), err, provenance.MaxDepth)
	}
	return c
}

// causalSnapshot captures every causal accessor so a test can say "only this
// one field was allowed to move".
type causalSnapshot struct {
	request     id.ID
	correlation id.ID
	causation   id.ID
	origin      provenance.Origin
	depth       uint8
	attempt     uint16
	traceparent string
	root        bool
	zero        bool
}

func causalSnap(p provenance.Provenance) causalSnapshot {
	return causalSnapshot{
		request:     p.Request(),
		correlation: p.Correlation(),
		causation:   p.Causation(),
		origin:      p.Origin(),
		depth:       p.Depth(),
		attempt:     p.Attempt(),
		traceparent: p.Traceparent(),
		root:        p.Root(),
		zero:        p.IsZero(),
	}
}

// causalWellFormedTraceparent is the canonical example from the W3C trace
// context recommendation. It is used only as a value that is unarguably
// well-formed; the shape rule itself is another writer's territory.
const causalWellFormedTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

// causalUnparseable are strings that cannot be an id.ID. They are chosen to be
// obviously not identifiers rather than to probe the charset rule.
var causalUnparseable = []string{
	"not-an-identifier",
	"1234",
	"0189-0c9a",
	"00000000000000000000000000000000000000",
}

// ---------------------------------------------------------------------------
// New: minting at an origin
// ---------------------------------------------------------------------------

func TestCausalNewMintsARootAtDepthZeroAttemptOne(t *testing.T) {
	p, _ := causalMustNew(t, provenance.OriginRequest)

	if p.Request().IsZero() {
		t.Errorf("New returned a Provenance with no request identifier: the request is minted here and is the identity of this unit of work")
	}
	if p.Request().Version() != 7 {
		t.Errorf("New minted a request with UUID version %d, want 7: minted identity is documented as UUIDv7 so that ID.Time recovers when the scope opened", p.Request().Version())
	}
	if p.Depth() != 0 {
		t.Errorf("New produced depth %d, want 0: the root of a chain is depth 0 and every derive counts up from there", p.Depth())
	}
	if !p.Root() {
		t.Errorf("New produced a value whose Root() is false: a value minted at an origin is the root of its chain")
	}
	if p.IsZero() {
		t.Errorf("New produced a value that reports IsZero(): a constructed provenance must be distinguishable from the unset one")
	}
}

func TestCausalAttemptStartsAtOneNotZero(t *testing.T) {
	p, _ := causalMustNew(t, provenance.OriginSchedule)

	if p.Attempt() != 1 {
		t.Errorf("New produced attempt %d, want 1: attempt is 1 at the origin, not 0, because \"attempt 1\" is what an operator reading a dashboard means by the first try", p.Attempt())
	}
}

func TestCausalNewCorrelationIsItsOwnRequestAtTheRoot(t *testing.T) {
	p, _ := causalMustNew(t, provenance.OriginRequest)

	// INFERENCE: the doc calls correlation "the chain's root" and never
	// mentions a second minted value, so the root's correlation is read here as
	// its own request. The alternative — a separately minted correlation
	// identifier that names no record — was rejected because then the
	// correlation of a chain would not point at anything.
	if p.Correlation() != p.Request() {
		t.Errorf("root correlation %s != root request %s: correlation is the chain's root, so at the origin it is the request that opened the chain", p.Correlation(), p.Request())
	}
	if p.Correlation().IsZero() {
		t.Errorf("New produced a zero correlation: every record must carry the identifier of the chain it belongs to")
	}
}

func TestCausalNewHasNoCausationAtTheRoot(t *testing.T) {
	p, _ := causalMustNew(t, provenance.OriginRequest)

	// INFERENCE: causation is "the immediate parent" and a root has no parent,
	// so it is read here as unset. The alternative is the CQRS convention where
	// the first message's causation is its own id; that was rejected because a
	// self-edge at the root is exactly the self-loop the doc warns reads as a
	// valid record.
	if !p.Causation().IsZero() {
		t.Errorf("root causation is %s, want the zero identifier: causation is the immediate parent edge and a root has no parent", p.Causation())
	}
}

func TestCausalNewNeverMintsATraceparent(t *testing.T) {
	p, _ := causalMustNew(t, provenance.OriginRequest)

	if p.Traceparent() != "" {
		t.Errorf("New produced traceparent %q, want empty: traceparent is W3C, adopted and never minted here", p.Traceparent())
	}
}

func TestCausalNewMintsExactlyOneIdentifier(t *testing.T) {
	m := causalNewMinter()
	p := provenance.New(provenance.OriginReplay, m)

	if p.IsZero() {
		t.Fatalf("New(OriginReplay) returned a zero Provenance: a listed origin must open a usable chain")
	}
	// INFERENCE: follows from the root's correlation being its request. If a
	// second identifier is minted here, this and
	// TestCausalNewCorrelationIsItsOwnRequestAtTheRoot fail together and the
	// pair should be read as one finding.
	if m.calls != 1 {
		t.Errorf("New called Minter.NewID %d times, want 1: a root has exactly one identifier to mint — its request, which is also its correlation", m.calls)
	}
}

func TestCausalNewRecordsTheOriginItWasGiven(t *testing.T) {
	for _, o := range provenance.Origins {
		t.Run(o.String(), func(t *testing.T) {
			p, _ := causalMustNew(t, o)
			if p.Origin() != o {
				t.Errorf("New(%v) recorded origin %v: origin answers why the chain exists, and a coverage claim that cannot say why is a claim nobody can act on", o, p.Origin())
			}
		})
	}
}

// AMENDED after triage. The INFERENCE this test carried was decided the other
// way: "New refuses it" means New panics, not that it fails closed to a zero
// value. doc.go now says so, and says why — returning an unusable value moves
// the crash to NewContext, one frame further from the mistake.
func TestCausalNewRefusesOriginUnknown(t *testing.T) {
	m := causalNewMinter()

	defer func() {
		if recover() == nil {
			t.Fatal("New(OriginUnknown) returned instead of panicking: an origin is a literal at a call site, so a chain with no reason is a programming error and must fail at construction")
		}
	}()
	p := provenance.New(provenance.OriginUnknown, m)
	t.Fatalf("New(OriginUnknown) returned a Provenance (request %s, origin %v)", p.Request(), p.Origin())
}

// AMENDED after triage. The INFERENCE was upheld — Origins IS the closed set and
// a value outside it names no reason — but the refusal is a panic, and the
// implementation was tightened to check membership rather than only the zero.
func TestCausalNewRefusesAnOriginOutsideTheClosedSet(t *testing.T) {
	m := causalNewMinter()

	defer func() {
		if recover() == nil {
			t.Fatal("New(Origin(200)) returned instead of panicking: Origins is the closed set of reasons a chain exists, and a value outside it names no reason at all")
		}
	}()
	p := provenance.New(provenance.Origin(200), m)
	t.Fatalf("New(Origin(200)) returned a usable Provenance with origin %v", p.Origin())
}

func TestCausalMinterSuppliesEveryMintedIdentifier(t *testing.T) {
	want := causalMintN(causalAltEpoch, 2)
	m := causalCannedMinter(want...)

	p := provenance.New(provenance.OriginRequest, m)
	if p.IsZero() {
		t.Fatalf("New(OriginRequest) returned a zero Provenance: a listed origin must open a usable chain")
	}
	if p.Request() != want[0] {
		t.Errorf("New's request is %s, want %s: identity comes from the injected Minter and from nowhere else, which is what makes a chain reproducible in a test", p.Request(), want[0])
	}

	c := causalMustDerive(t, p, m)
	if c.Request() != want[1] {
		t.Errorf("Derive's request is %s, want %s: a derive mints its request from the Minter it was passed", c.Request(), want[1])
	}
}

// ---------------------------------------------------------------------------
// Derive and DeriveFrom: correlation is the tree, causation is the edge
// ---------------------------------------------------------------------------

func TestCausalDeriveKeepsCorrelationAndTakesCausationFromTheParentRequest(t *testing.T) {
	p, m := causalMustNew(t, provenance.OriginRequest)
	c := causalMustDerive(t, p, m)

	if c.Correlation() != p.Correlation() {
		t.Errorf("child correlation %s != parent correlation %s: minting a new correlation at a hop breaks the chain silently, and the break is invisible until somebody asks a question that crosses it", c.Correlation(), p.Correlation())
	}
	if c.Causation() != p.Request() {
		t.Errorf("child causation %s != parent request %s: causation is the immediate parent edge, and without that edge you have a bag of records sharing an identifier and no parentage", c.Causation(), p.Request())
	}
}

func TestCausalDeriveMintsAFreshRequestAndNeverSelfLoops(t *testing.T) {
	p, m := causalMustNew(t, provenance.OriginRequest)
	c := causalMustDerive(t, p, m)

	if c.Request().IsZero() {
		t.Errorf("Derive produced a child with no request identifier: every unit of work is minted its own identity")
	}
	if c.Request() == p.Request() {
		t.Errorf("child request %s equals parent request %s: a parent's request becoming its child's is the collapse into a self-loop that reads as a valid record", c.Request(), p.Request())
	}
	if c.Request() == c.Causation() {
		t.Errorf("child request %s equals its own causation: a record cannot be its own cause", c.Request())
	}
}

func TestCausalDeriveAddsExactlyOneToDepth(t *testing.T) {
	p, m := causalMustNew(t, provenance.OriginRequest)

	cur := p
	for hop := 1; hop <= 4; hop++ {
		next := causalMustDerive(t, cur, m)
		if int(next.Depth()) != int(cur.Depth())+1 {
			t.Fatalf("hop %d: depth went %d -> %d, want +1: depth is hops from the root, and it is the only thing that detects a cycle whose records all share one correlation by design", hop, cur.Depth(), next.Depth())
		}
		if next.Root() {
			t.Errorf("hop %d: a derived value reports Root(): only depth 0 is the root", hop)
		}
		cur = next
	}
}

func TestCausalDeriveResetsAttemptToOne(t *testing.T) {
	// INFERENCE: attempt is "which delivery of THIS unit", and a derive creates
	// a different unit, so it starts on its own first delivery. The alternative
	// — a child inheriting its parent's attempt — was rejected because it would
	// make a child of a redelivered message indistinguishable from a retried
	// child.
	p, m := causalMustNew(t, provenance.OriginRequest)
	redelivered := p.Retry().Retry()
	if redelivered.Attempt() != 3 {
		t.Fatalf("two Retry calls produced attempt %d, want 3: this test needs a parent whose attempt is not 1", redelivered.Attempt())
	}

	c := causalMustDerive(t, redelivered, m)
	if c.Attempt() != 1 {
		t.Errorf("child of an attempt-3 parent has attempt %d, want 1: a derived child is a new unit of work on its first delivery, and attempt is what separates a poison message from a fan-out", c.Attempt())
	}
}

func TestCausalDeriveInheritsOrigin(t *testing.T) {
	// INFERENCE: origin is why the CHAIN exists, so every node of one chain
	// carries the same origin. The alternative — a per-hop origin — has no
	// candidate value, since New refuses OriginUnknown and nothing else could
	// be supplied at a derive.
	for _, o := range provenance.Origins {
		t.Run(o.String(), func(t *testing.T) {
			p, m := causalMustNew(t, o)
			c := causalMustDerive(t, p, m)
			g := causalMustDerive(t, c, m)

			if c.Origin() != o || g.Origin() != o {
				t.Errorf("origins down the chain were %v, %v, %v: origin says why the chain exists, so it is a property of the chain and not of the hop", o, c.Origin(), g.Origin())
			}
		})
	}
}

func TestCausalDeriveFromTakesCausationFromTheCauseNotTheParent(t *testing.T) {
	p, m := causalMustNew(t, provenance.OriginSchedule)
	cause := causalMintN(causalAltEpoch, 1)[0]

	c := causalMustDeriveFrom(t, p, m, cause)

	if c.Causation() != cause {
		t.Errorf("DeriveFrom set causation to %s, want the supplied cause %s: a subscriber takes causation from the EVENT's identifier, which is the whole reason this constructor exists", c.Causation(), cause)
	}
	if c.Causation() == p.Request() {
		t.Errorf("DeriveFrom used the parent's request %s as causation instead of the supplied cause: the parent is not the edge here, the cause is", p.Request())
	}
	if c.Request() == cause {
		t.Errorf("DeriveFrom minted the cause %s as the child's own request: the child is a new unit of work, not the thing that caused it", cause)
	}
}

func TestCausalDeriveFromKeepsTheParentCorrelation(t *testing.T) {
	p, m := causalMustNew(t, provenance.OriginSchedule)
	cause := causalMintN(causalAltEpoch, 1)[0]

	c := causalMustDeriveFrom(t, p, m, cause)

	if c.Correlation() != p.Correlation() {
		t.Errorf("DeriveFrom set correlation to %s, want the parent's %s: correlation comes from the envelope and causation from the event, and minting a new correlation here breaks the chain at that hop", c.Correlation(), p.Correlation())
	}
	if c.Correlation() == cause {
		t.Errorf("DeriveFrom adopted the cause %s as the correlation: the cause names an edge, not a tree", cause)
	}
	if int(c.Depth()) != int(p.Depth())+1 {
		t.Errorf("DeriveFrom produced depth %d from a parent at depth %d, want +1: a derive from an event is still a hop and the bound still applies", c.Depth(), p.Depth())
	}
}

func TestCausalDeriveFromMatchesDeriveWhenTheCauseIsTheParentRequest(t *testing.T) {
	p, _ := causalMustNew(t, provenance.OriginRequest)
	childID := causalMintN(causalAltEpoch, 1)[0]

	a := causalMustDerive(t, p, causalCannedMinter(childID))
	b := causalMustDeriveFrom(t, p, causalCannedMinter(childID), p.Request())

	if a != b {
		t.Errorf("Derive and DeriveFrom(parent.Request()) produced different values (%+v vs %+v): Derive is exactly DeriveFrom with the parent as the cause, and any other difference is a field one of them forgot", causalSnap(a), causalSnap(b))
	}
}

func TestCausalDeriveFromWithZeroCauseDoesNotProduceAZeroCausation(t *testing.T) {
	p, m := causalMustNew(t, provenance.OriginRequest)

	c, err := p.DeriveFrom(m, id.Nil)
	if err != nil {
		// Errors here are ours, never the caller's.
		if !errors.IsKind(err, errors.Internal) {
			t.Errorf("DeriveFrom(Nil) failed with kind %v, want Internal: a cause is an identifier this system minted, so nobody can fix it by sending different input and reporting Invalid would put a 400 on a fault that is ours", errors.KindOf(err))
		}
		if !c.IsZero() {
			t.Errorf("DeriveFrom(Nil) returned an error AND a non-zero Provenance: a failed derive must not hand back a usable value")
		}
		return
	}
	if c.Causation().IsZero() {
		t.Errorf("DeriveFrom(Nil) succeeded with a zero causation: a record at depth %d claiming an empty parent edge is a record that says it has a parent and cannot name it", c.Depth())
	}
}

// ---------------------------------------------------------------------------
// chain-wide properties
// ---------------------------------------------------------------------------

func TestCausalCorrelationIsInvariantAcrossAWholeChain(t *testing.T) {
	p, m := causalMustNew(t, provenance.OriginBackfill)
	want := p.Correlation()
	causes := causalMintN(causalAltEpoch, 4)

	type causalStep struct {
		name string
		fn   func(provenance.Provenance) provenance.Provenance
	}
	steps := []causalStep{
		{"derive", func(x provenance.Provenance) provenance.Provenance { return causalMustDerive(t, x, m) }},
		{"derive from an event", func(x provenance.Provenance) provenance.Provenance {
			return causalMustDeriveFrom(t, x, m, causes[0])
		}},
		{"retry", func(x provenance.Provenance) provenance.Provenance { return x.Retry() }},
		{"adopt nothing usable", func(x provenance.Provenance) provenance.Provenance {
			return x.Adopt(provenance.Adopted{Correlation: causalUnparseable[0]})
		}},
		{"derive again", func(x provenance.Provenance) provenance.Provenance { return causalMustDerive(t, x, m) }},
		{"derive from another event", func(x provenance.Provenance) provenance.Provenance {
			return causalMustDeriveFrom(t, x, m, causes[1])
		}},
	}

	cur := p
	for _, s := range steps {
		cur = s.fn(cur)
		if cur.Correlation() != want {
			t.Fatalf("after %q the correlation is %s, want %s: everything descending from one origin shares one correlation, and a hop that changes it severs the chain invisibly", s.name, cur.Correlation(), want)
		}
	}
}

func TestCausalEveryRequestInAChainIsDistinct(t *testing.T) {
	p, m := causalMustNew(t, provenance.OriginRequest)
	causes := causalMintN(causalAltEpoch, 3)

	seen := map[id.ID]string{p.Request(): "root"}
	cur := p
	labels := []string{"child", "grandchild", "great-grandchild"}
	for i, label := range labels {
		if i%2 == 0 {
			cur = causalMustDerive(t, cur, m)
		} else {
			cur = causalMustDeriveFrom(t, cur, m, causes[i])
		}
		if prior, dup := seen[cur.Request()]; dup {
			t.Fatalf("%s reused the request identifier of %s (%s): every unit of work is minted its own identity, or two different units become one record in a query", label, prior, cur.Request())
		}
		seen[cur.Request()] = label
	}
	if len(seen) != len(labels)+1 {
		t.Errorf("a chain of %d units produced %d distinct request identifiers: identity per unit of work is what makes a causal edge point at exactly one thing", len(labels)+1, len(seen))
	}
}

// ---------------------------------------------------------------------------
// depth is a bound, not a statistic
// ---------------------------------------------------------------------------

func TestCausalDeriveFailsPastMaxDepth(t *testing.T) {
	p, m := causalMustNew(t, provenance.OriginRequest)

	cur := p
	for hop := 1; hop <= provenance.MaxDepth; hop++ {
		next, err := cur.Derive(m)
		if err != nil {
			t.Fatalf("Derive to depth %d failed with %v: depth %d is not past MaxDepth (%d), so the bound must not have been reached yet", hop, err, hop, provenance.MaxDepth)
		}
		if int(next.Depth()) != hop {
			t.Fatalf("hop %d produced depth %d: depth counts hops from the root exactly", hop, next.Depth())
		}
		cur = next
	}
	if int(cur.Depth()) != provenance.MaxDepth {
		t.Fatalf("after %d derives the depth is %d: the boundary case cannot be exercised unless depth reaches MaxDepth exactly", provenance.MaxDepth, cur.Depth())
	}

	over, err := cur.Derive(m)
	if err == nil {
		t.Fatalf("Derive from depth %d (MaxDepth) succeeded and produced depth %d: past MaxDepth a derive must fail, because a chain that deep is a cycle in every case anyone has produced", provenance.MaxDepth, over.Depth())
	}
	if !errors.Is(err, provenance.ErrDepthExceeded) {
		t.Errorf("Derive past MaxDepth failed with %v, want ErrDepthExceeded: the runaway has to be identifiable by the caller that has to stop it", err)
	}
	if !over.IsZero() {
		t.Errorf("Derive past MaxDepth returned an error AND a usable Provenance (depth %d): returning a value there makes the runaway cheaper to continue than to stop", over.Depth())
	}
}

func TestCausalDeriveFromFailsPastMaxDepth(t *testing.T) {
	p, m := causalMustNew(t, provenance.OriginSchedule)
	cause := causalMintN(causalAltEpoch, 1)[0]

	cur := p
	for hop := 1; hop <= provenance.MaxDepth; hop++ {
		next, err := cur.DeriveFrom(m, cause)
		if err != nil {
			t.Fatalf("DeriveFrom to depth %d failed with %v: depth %d is within MaxDepth (%d)", hop, err, hop, provenance.MaxDepth)
		}
		cur = next
	}

	over, err := cur.DeriveFrom(m, cause)
	if err == nil {
		t.Fatalf("DeriveFrom from depth %d (MaxDepth) succeeded: the event-driven path is precisely the one that forms cycles, so the bound must apply to it too", provenance.MaxDepth)
	}
	if !errors.Is(err, provenance.ErrDepthExceeded) {
		t.Errorf("DeriveFrom past MaxDepth failed with %v, want ErrDepthExceeded: both derives are bounded by the same rule and must report it the same way", err)
	}
	if !over.IsZero() {
		t.Errorf("DeriveFrom past MaxDepth returned an error AND a usable Provenance: a failed derive must not hand back something a subscriber can keep publishing with")
	}
}

func TestCausalDepthNeverExceedsMaxDepthOnAnySuccessfulDerive(t *testing.T) {
	p, m := causalMustNew(t, provenance.OriginRequest)

	cur := p
	for hop := 1; hop <= provenance.MaxDepth+8; hop++ {
		next, err := cur.Derive(m)
		if err != nil {
			break
		}
		if int(next.Depth()) > provenance.MaxDepth {
			t.Fatalf("a successful Derive produced depth %d, above MaxDepth (%d): depth is a bound, and a bound that can be exceeded by continuing to call is not a bound", next.Depth(), provenance.MaxDepth)
		}
		cur = next
	}
}

func TestCausalErrDepthExceededIsInternalNotInvalid(t *testing.T) {
	if provenance.ErrDepthExceeded == nil {
		t.Fatalf("ErrDepthExceeded is nil: callers match on this sentinel to tell a runaway from any other failure")
	}
	if !errors.IsKind(provenance.ErrDepthExceeded, errors.Internal) {
		t.Errorf("ErrDepthExceeded has kind %v, want Internal: nobody can fix a runaway chain by sending different input, and reporting Invalid would put a 400 on a fault that is ours", errors.KindOf(provenance.ErrDepthExceeded))
	}
	if errors.IsKind(provenance.ErrDepthExceeded, errors.Invalid) {
		t.Errorf("ErrDepthExceeded reports kind Invalid: every failure in this package is Internal")
	}
	if provenance.ErrDepthExceeded.Error() == "" {
		t.Errorf("ErrDepthExceeded has an empty message: the operator who sees it needs to be told which bound was hit")
	}
}

// ---------------------------------------------------------------------------
// Retry: the same unit again
// ---------------------------------------------------------------------------

func TestCausalRetryMovesOnlyAttempt(t *testing.T) {
	p, m := causalMustNew(t, provenance.OriginRequest)
	parent := causalMustDerive(t, p, m)

	before := causalSnap(parent)
	r := parent.Retry()
	after := causalSnap(r)

	if after.attempt != before.attempt+1 {
		t.Errorf("Retry moved attempt %d -> %d, want +1: attempt is what separates a message redelivered nine times from a message that fanned out to nine children", before.attempt, after.attempt)
	}
	if after.request != before.request {
		t.Errorf("Retry minted a new request (%s -> %s): a redelivery is the same unit of work arriving again, so its identity does not change", before.request, after.request)
	}
	if after.correlation != before.correlation {
		t.Errorf("Retry changed correlation %s -> %s: a redelivery continues a chain, it does not open one", before.correlation, after.correlation)
	}
	if after.causation != before.causation {
		t.Errorf("Retry changed causation %s -> %s: the same unit arriving again has the same parent edge", before.causation, after.causation)
	}
	if after.depth != before.depth {
		t.Errorf("Retry changed depth %d -> %d: Retry is not a derive, and deriving instead would make a poison message look like a widening tree", before.depth, after.depth)
	}
	if after.origin != before.origin {
		t.Errorf("Retry changed origin %v -> %v: a redelivery is deliberately not an origin — it continues a chain rather than opening one", before.origin, after.origin)
	}
	if after.traceparent != before.traceparent {
		t.Errorf("Retry changed traceparent %q -> %q: only attempt moves", before.traceparent, after.traceparent)
	}
	if after.root != before.root {
		t.Errorf("Retry changed Root() %v -> %v: the unit's place in the tree is unchanged by a redelivery", before.root, after.root)
	}
	if r == parent {
		t.Errorf("Retry returned a value equal to its receiver: the redelivery must be distinguishable from the first attempt in a log")
	}
}

func TestCausalRetryIncrementsMonotonicallyAcrossRedeliveries(t *testing.T) {
	p, _ := causalMustNew(t, provenance.OriginRequest)

	cur := p
	for want := uint16(2); want <= 6; want++ {
		cur = cur.Retry()
		if cur.Attempt() != want {
			t.Fatalf("redelivery %d reports attempt %d: each redelivery of the same unit adds exactly one, or the count an operator reads during an incident is wrong", want-1, cur.Attempt())
		}
		if cur.Request() != p.Request() || cur.Depth() != p.Depth() {
			t.Fatalf("redelivery %d changed request or depth (%s/%d vs %s/%d): nine redeliveries must not read as a widening tree", want-1, cur.Request(), cur.Depth(), p.Request(), p.Depth())
		}
	}
}

func TestCausalRetryStillWorksAtMaxDepth(t *testing.T) {
	p, m := causalMustNew(t, provenance.OriginRequest)

	cur := p
	for hop := 1; hop <= provenance.MaxDepth; hop++ {
		next, err := cur.Derive(m)
		if err != nil {
			t.Fatalf("Derive to depth %d failed with %v: this test needs a value sitting exactly at MaxDepth", hop, err)
		}
		cur = next
	}

	r := cur.Retry()
	if r.Attempt() != cur.Attempt()+1 {
		t.Errorf("Retry at MaxDepth did not move attempt (%d -> %d): Retry is not a derive, so the depth bound does not apply to it — a poison message at the bottom of a chain still has to be countable", cur.Attempt(), r.Attempt())
	}
	if int(r.Depth()) != provenance.MaxDepth {
		t.Errorf("Retry at MaxDepth produced depth %d: a redelivery does not add a hop", r.Depth())
	}
}

// ---------------------------------------------------------------------------
// Adopt: what a caller may and may not move
// ---------------------------------------------------------------------------

func TestCausalAdoptedHasNoFieldForRequestActorTenantDepthOrAttempt(t *testing.T) {
	typ := reflect.TypeOf(provenance.Adopted{})
	if typ.Kind() != reflect.Struct {
		t.Fatalf("Adopted is a %v, want a struct", typ.Kind())
	}

	want := map[string]bool{"Correlation": false, "Causation": false, "Traceparent": false}
	forbidden := []string{"request", "actor", "tenant", "depth", "attempt", "behalf", "origin"}

	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		lower := strings.ToLower(f.Name)
		if _, ok := want[f.Name]; ok {
			want[f.Name] = true
			if f.Type.Kind() != reflect.String {
				t.Errorf("Adopted.%s is a %v, want string: adopted values are a caller's bytes, validated and untrusted, and carry no minted identity", f.Name, f.Type.Kind())
			}
			continue
		}
		flagged := false
		for _, bad := range forbidden {
			if strings.Contains(lower, bad) {
				t.Errorf("Adopted has a field %q: adopt what can only correlate, never what confers identity, authority or a bound — and the absence of the field is what enforces that, instead of a comment asking", f.Name)
				flagged = true
			}
		}
		if !flagged {
			t.Errorf("Adopted has an unexpected field %q: the type is the enforcement here, so any field added to it widens what a caller controls", f.Name)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("Adopted has no field %q: correlation, causation and traceparent are the three things an inbound boundary may contribute", name)
		}
	}
	if typ.NumField() != len(want) {
		t.Errorf("Adopted has %d fields, want %d: Depth and Attempt exist to be branched on, and being the exception is exactly why neither is adoptable", typ.NumField(), len(want))
	}
}

func TestCausalAdoptCannotMoveRequestDepthAttemptOrOrigin(t *testing.T) {
	p, m := causalMustNew(t, provenance.OriginSchedule)
	child := causalMustDerive(t, p, m)
	deep := child.Retry() // depth 1, attempt 2

	ids := causalMintN(causalAltEpoch, 2)
	before := causalSnap(deep)

	got := deep.Adopt(provenance.Adopted{
		Correlation: ids[0].String(),
		Causation:   ids[1].String(),
		Traceparent: causalWellFormedTraceparent,
	})
	after := causalSnap(got)

	if after.request != before.request {
		t.Errorf("Adopt changed the request %s -> %s: the request is minted here and never adopted, because it is the value that confers identity", before.request, after.request)
	}
	if after.depth != before.depth {
		t.Errorf("Adopt changed depth %d -> %d: a caller must not be able to reset a bound it is subject to", before.depth, after.depth)
	}
	if after.attempt != before.attempt {
		t.Errorf("Adopt changed attempt %d -> %d: attempt is branched on, and Adopted has no field for it precisely so that it cannot be supplied", before.attempt, after.attempt)
	}
	if after.origin != before.origin {
		t.Errorf("Adopt changed origin %v -> %v: origin says why this chain exists and is decided at the mint, not by the caller", before.origin, after.origin)
	}
	if after.root != before.root {
		t.Errorf("Adopt changed Root() %v -> %v: Root follows depth, which adoption cannot touch", before.root, after.root)
	}
}

func TestCausalAdoptOfAnEmptyAdoptedChangesNothing(t *testing.T) {
	p, m := causalMustNew(t, provenance.OriginRequest)
	child := causalMustDerive(t, p, m)

	if got := child.Adopt(provenance.Adopted{}); got != child {
		t.Errorf("Adopt(Adopted{}) returned a different value (%+v vs %+v): an absent field is absent, not an instruction to clear what is already known", causalSnap(got), causalSnap(child))
	}
}

func TestCausalAdoptDropsWhatItCannotParse(t *testing.T) {
	for _, bad := range causalUnparseable {
		t.Run("an unparseable value "+bad+" is dropped", func(t *testing.T) {
			p, _ := causalMustNew(t, provenance.OriginRequest)
			got := p.Adopt(provenance.Adopted{Correlation: bad, Causation: bad})

			if got != p {
				t.Errorf("Adopt of the unparseable correlation/causation %q changed the value (%+v vs %+v): an adopted value that does not parse is dropped, not stored — a foreign identifier must never become the grouping key of our audit trail", bad, causalSnap(got), causalSnap(p))
			}
			if got.Correlation() != p.Correlation() {
				t.Errorf("Adopt of %q replaced the correlation with %s: our identifiers are our own format, so a value that is not one of ours cannot join our chain", bad, got.Correlation())
			}
		})
	}
}

func TestCausalAdoptDoesNotErrorAndAlwaysReturnsAUsableValue(t *testing.T) {
	p, _ := causalMustNew(t, provenance.OriginRequest)

	// The signature returning no error is compile-enforced; what is testable is
	// that the value survives the worst input.
	got := p.Adopt(provenance.Adopted{
		Correlation: causalUnparseable[0],
		Causation:   "",
		Traceparent: causalUnparseable[1],
	})

	if got.IsZero() {
		t.Errorf("Adopt of unusable input returned a zero Provenance: Adopt takes each field independently and drops what it cannot use — refusing outright would take something away for no gain")
	}
	if got.Request() != p.Request() {
		t.Errorf("Adopt of unusable input lost the request %s: dropping a bad correlation must not disturb the identity that was minted here", p.Request())
	}
}

func TestCausalAdoptTakesAWellFormedCorrelation(t *testing.T) {
	p, _ := causalMustNew(t, provenance.OriginRequest)
	inbound := causalMintN(causalAltEpoch, 1)[0]

	got := p.Adopt(provenance.Adopted{Correlation: inbound.String()})

	if got.Correlation() != inbound {
		t.Errorf("Adopt left the correlation at %s, want the adopted %s: an inbound boundary is where a caller's chain is joined, and a value that parses as one of our identifiers is exactly what may be adopted", got.Correlation(), inbound)
	}
	if got.Request() != p.Request() {
		t.Errorf("adopting a correlation changed the request %s -> %s: request is minted here, never adopted", p.Request(), got.Request())
	}
}

func TestCausalAdoptTakesEachFieldIndependently(t *testing.T) {
	ids := causalMintN(causalAltEpoch, 2)

	t.Run("a bad correlation does not veto a good causation", func(t *testing.T) {
		p, _ := causalMustNew(t, provenance.OriginRequest)
		got := p.Adopt(provenance.Adopted{Correlation: causalUnparseable[0], Causation: ids[1].String()})

		if got.Causation() != ids[1] {
			t.Errorf("causation is %s, want the adopted %s: Adopt takes each field independently and drops only what it cannot use", got.Causation(), ids[1])
		}
		if got.Correlation() != p.Correlation() {
			t.Errorf("correlation moved to %s despite being unparseable: a dropped field keeps the value that was already there", got.Correlation())
		}
	})

	t.Run("a bad causation does not veto a good correlation", func(t *testing.T) {
		p, _ := causalMustNew(t, provenance.OriginRequest)
		got := p.Adopt(provenance.Adopted{Correlation: ids[0].String(), Causation: causalUnparseable[1]})

		if got.Correlation() != ids[0] {
			t.Errorf("correlation is %s, want the adopted %s: one unusable field must not cost the caller the field that was usable", got.Correlation(), ids[0])
		}
		if got.Causation() != p.Causation() {
			t.Errorf("causation moved to %s despite being unparseable: a dropped field keeps the value that was already there", got.Causation())
		}
	})
}

func TestCausalAdoptCarriesATraceparentVerbatim(t *testing.T) {
	p, _ := causalMustNew(t, provenance.OriginRequest)

	got := p.Adopt(provenance.Adopted{Traceparent: causalWellFormedTraceparent})

	if got.Traceparent() != causalWellFormedTraceparent {
		t.Errorf("traceparent is %q, want %q byte for byte: it is carried verbatim precisely because nothing here reads it, and cross-vendor correlation is the only thing it is for", got.Traceparent(), causalWellFormedTraceparent)
	}
	if got.Correlation() != p.Correlation() || got.Request() != p.Request() {
		t.Errorf("adopting a traceparent moved a minted identifier (request %s, correlation %s): traceparent is observability only and never joins our chain", got.Request(), got.Correlation())
	}
}

func TestCausalAdoptIsIdempotent(t *testing.T) {
	p, _ := causalMustNew(t, provenance.OriginRequest)
	ids := causalMintN(causalAltEpoch, 2)
	a := provenance.Adopted{
		Correlation: ids[0].String(),
		Causation:   ids[1].String(),
		Traceparent: causalWellFormedTraceparent,
	}

	once := p.Adopt(a)
	twice := once.Adopt(a)

	if once != twice {
		t.Errorf("adopting the same values twice produced different results (%+v vs %+v): Adopt sets fields from a caller's bytes, so applying it again with the same bytes has nothing left to change", causalSnap(once), causalSnap(twice))
	}
}

// ---------------------------------------------------------------------------
// Root, IsZero, immutability, comparability
// ---------------------------------------------------------------------------

func TestCausalRootIsDepthZero(t *testing.T) {
	p, m := causalMustNew(t, provenance.OriginStartup)

	// INFERENCE: "Root is depth 0" is read as the definition of Root(). The
	// alternative reading — Root() meaning correlation == request — differs
	// only for a root that has adopted a foreign correlation, which is not
	// exercised here.
	cur := p
	for hop := 0; hop <= 3; hop++ {
		wantRoot := cur.Depth() == 0
		if cur.Root() != wantRoot {
			t.Fatalf("at depth %d Root() is %v, want %v: root is depth 0, and nothing else is", cur.Depth(), cur.Root(), wantRoot)
		}
		if hop == 0 && !cur.Root() {
			t.Fatalf("a value straight from New is not Root(): New mints at an origin, which is the root of the chain by definition")
		}
		if hop > 0 && cur.Root() {
			t.Fatalf("a value %d hops down still reports Root(): a derived unit has a parent", hop)
		}
		cur = causalMustDerive(t, cur, m)
	}
}

func TestCausalIsZeroDistinguishesTheZeroValueFromAMintedRoot(t *testing.T) {
	var zero provenance.Provenance
	p, _ := causalMustNew(t, provenance.OriginRequest)

	if !zero.IsZero() {
		t.Errorf("the zero Provenance does not report IsZero(): absent, empty and defaulted are three states here, and the unset one has to be recognisable")
	}
	if p.IsZero() {
		t.Errorf("a Provenance from New reports IsZero(): a constructed chain must never be mistaken for an unset field")
	}
	if zero == p {
		t.Errorf("a minted root compares equal to the zero value: the zero value carries no origin, no identity and no attempt, and cannot be allowed to read as a valid record")
	}
	if zero.Attempt() == 1 {
		t.Errorf("the zero Provenance reports attempt 1: attempt 1 is a claim that a first delivery happened, and the unset value has made no such claim")
	}
	if !zero.Request().IsZero() {
		t.Errorf("the zero Provenance carries request %s: nothing but a Minter can produce an identity here", zero.Request())
	}
}

func TestCausalProvenanceIsComparableAndUsableAsAMapKey(t *testing.T) {
	// This does not compile at all if Provenance stops being comparable, which
	// is the promise: "== means what it looks like".
	p, m := causalMustNew(t, provenance.OriginRequest)
	child := causalMustDerive(t, p, m)

	index := map[provenance.Provenance]string{p: "root", child: "child"}
	if len(index) != 2 {
		t.Fatalf("a root and its child collapsed to %d map entries: two different units of work must not be one key", len(index))
	}

	copyOfRoot := p
	if index[copyOfRoot] != "root" {
		t.Errorf("a copy of a Provenance did not find its own map entry: a lineage record stores these values, so they must be storable and comparable in the domain and not merely loggable")
	}
	if index[child] != "child" {
		t.Errorf("a derived Provenance did not find its own map entry: equality has to distinguish nodes of the same chain")
	}
}

func TestCausalTwoIdenticallyMintedRootsAreEqual(t *testing.T) {
	p1 := provenance.New(provenance.OriginRequest, id.NewSequence(causalEpoch))
	p2 := provenance.New(provenance.OriginRequest, id.NewSequence(causalEpoch))

	if p1.IsZero() || p2.IsZero() {
		t.Fatalf("New(OriginRequest) returned a zero Provenance: a listed origin must open a usable chain")
	}
	if p1 != p2 {
		t.Errorf("two roots minted with identical deterministic minters and the same origin are not == (%+v vs %+v): equality must mean equality of the ledger, which it cannot if some hidden field carries a clock reading or a pointer", causalSnap(p1), causalSnap(p2))
	}
}

func TestCausalProvenanceHasNoExportedFieldsAndNoSliceOrMapField(t *testing.T) {
	typ := reflect.TypeOf(provenance.Provenance{})
	if typ.Kind() != reflect.Struct {
		t.Fatalf("Provenance is a %v, want a struct", typ.Kind())
	}
	if typ.NumField() == 0 {
		t.Fatalf("Provenance has no fields at all: it is supposed to carry the ledger")
	}
	causalCheckFields(t, typ, "Provenance", 0)
}

func causalCheckFields(t *testing.T, typ reflect.Type, path string, depth int) {
	t.Helper()
	if depth > 4 {
		return
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		name := path + "." + f.Name
		if depth == 0 && f.IsExported() {
			t.Errorf("%s is exported: every field is unexported precisely so there is no struct literal, because hand-building is how a parent's request becomes its child's", name)
		}
		switch f.Type.Kind() {
		case reflect.Slice, reflect.Map, reflect.Func:
			t.Errorf("%s is a %v: Provenance is promised comparable and usable as a map key, so no slice, map or func field, ever, whatever arrives later — a field that needs one belongs on the record", name, f.Type.Kind())
		case reflect.Struct:
			causalCheckFields(t, f.Type, name, depth+1)
		}
	}
}

func TestCausalConstructorsDoNotMutateTheirReceiver(t *testing.T) {
	p, m := causalMustNew(t, provenance.OriginRequest)
	parent := causalMustDerive(t, p, m)
	cause := causalMintN(causalAltEpoch, 1)[0]
	inbound := causalMintN(causalAltEpoch.Add(time.Hour), 1)[0]

	before := parent
	beforeSnap := causalSnap(parent)

	_ = causalMustDerive(t, parent, m)
	_ = causalMustDeriveFrom(t, parent, m, cause)
	_ = parent.Retry()
	_ = parent.Adopt(provenance.Adopted{Correlation: inbound.String(), Traceparent: causalWellFormedTraceparent})

	if parent != before {
		t.Errorf("the receiver changed after Derive/DeriveFrom/Retry/Adopt (%+v, was %+v): a Provenance is immutable, and a constructor that edits its receiver would rewrite the history of records already written", causalSnap(parent), beforeSnap)
	}
	if causalSnap(parent) != beforeSnap {
		t.Errorf("an accessor on the receiver changed after deriving: immutability is what makes a stamped record final")
	}
}

// ---------------------------------------------------------------------------
// Origin and ParseOrigin
// ---------------------------------------------------------------------------

func TestCausalOriginRoundTripsThroughParseOrigin(t *testing.T) {
	for _, o := range provenance.Origins {
		t.Run(o.String(), func(t *testing.T) {
			s := o.String()
			if s == "" {
				t.Fatalf("Origin(%d) renders as the empty string: an origin that cannot be written down cannot be stored on a coverage claim", uint8(o))
			}
			got, ok := provenance.ParseOrigin(s)
			if !ok {
				t.Fatalf("ParseOrigin(%q) reported not-ok for a string this package itself produced: a value must survive the round trip through storage", s)
			}
			if got != o {
				t.Errorf("ParseOrigin(%q) = %v, want %v: parse must invert String, or a chain read back from the database claims a different reason than the one recorded", s, got, o)
			}
		})
	}
}

func TestCausalParseOriginFailsClosedOnAnUnknownString(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"the empty string", ""},
		{"a word that is not an origin", "not-an-origin"},
		{"a redelivery, which is deliberately not an origin", "redelivery"},
		{"a retry, which continues a chain rather than opening one", "retry"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := provenance.ParseOrigin(tc.in)
			if ok {
				t.Errorf("ParseOrigin(%q) reported ok and returned %v: only the five listed reasons open a chain", tc.in, got)
			}
			if got != provenance.OriginUnknown {
				t.Errorf("ParseOrigin(%q) failed but returned %v: the zero value is where an unrecognised origin has to land, because a chain whose reason is unrecorded must not read as a real reason", tc.in, got)
			}
		})
	}
}

func TestCausalOriginsIsTheClosedSetAndExcludesUnknown(t *testing.T) {
	want := []provenance.Origin{
		provenance.OriginRequest,
		provenance.OriginSchedule,
		provenance.OriginReplay,
		provenance.OriginBackfill,
		provenance.OriginStartup,
	}

	if len(provenance.Origins) != len(want) {
		t.Fatalf("Origins has %d entries, want %d: it is the enumeration every caller ranges over, so a reason missing from it is a reason nothing can produce or validate", len(provenance.Origins), len(want))
	}

	seen := map[provenance.Origin]int{}
	for _, o := range provenance.Origins {
		seen[o]++
		if o == provenance.OriginUnknown {
			t.Errorf("Origins contains OriginUnknown: the zero value is the fail-closed state, not a reason a chain exists")
		}
	}
	for _, o := range want {
		if seen[o] != 1 {
			t.Errorf("Origins contains %v %d times, want exactly 1: the set is closed and each reason appears once", o, seen[o])
		}
	}

	strs := map[string]provenance.Origin{}
	for _, o := range provenance.Origins {
		s := o.String()
		if prior, dup := strs[s]; dup {
			t.Errorf("%v and %v both render as %q: two reasons that render the same cannot be told apart by anyone reading a coverage claim", prior, o, s)
		}
		strs[s] = o
		if s == provenance.OriginUnknown.String() {
			t.Errorf("%v renders the same as OriginUnknown (%q): the unrecorded reason must never be confusable with a recorded one", o, s)
		}
	}
}

func TestCausalRedeliveryIsNotAnOrigin(t *testing.T) {
	for _, o := range provenance.Origins {
		switch strings.ToLower(o.String()) {
		case "retry", "redelivery", "redeliver":
			t.Errorf("Origins contains %q: a redelivery does not open a chain, it continues one with Retry, and modelling it as an origin would make every poison message look like new work", o.String())
		}
	}
}

func TestCausalOriginUnknownIsTheZeroValue(t *testing.T) {
	var zero provenance.Origin
	if zero != provenance.OriginUnknown {
		t.Errorf("the zero Origin is %v, not OriginUnknown: the whole fail-closed argument rests on the unset value being the unrecorded one", zero)
	}
}
