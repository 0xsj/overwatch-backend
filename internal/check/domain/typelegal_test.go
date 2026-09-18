// Author-written. decisions/0032 deferred this rule with its reasons; 0039 and
// 0041 each turned it from tidiness into a silent wrong answer, which is what
// made it worth building.
package domain_test

import (
	"testing"

	"github.com/0xsj/overwatch-backend/internal/check/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// subfinder -> httpx -> nuclei, the chain the whole product is shaped around.
func realistic() (domain.Chain, map[id.ID]domain.Feeds) {
	chain := domain.Chain{
		Steps: []domain.Step{{ID: nonZero(1)}, {ID: nonZero(2)}, {ID: nonZero(3)}},
		Flows: []domain.Flow{
			{From: nonZero(1), To: nonZero(2)},
			{From: nonZero(2), To: nonZero(3)},
		},
	}
	return chain, map[id.ID]domain.Feeds{
		nonZero(1): {Produces: "host"},                     // subfinder: a SOURCE
		nonZero(2): {Consumes: "host", Produces: "url"},    // httpx
		nonZero(3): {Consumes: "url", Produces: "finding"}, // nuclei
	}
}

func TestARealChainIsTypeLegal(t *testing.T) {
	chain, feeds := realistic()
	if err := chain.TypeLegal(feeds); err != nil {
		t.Fatalf("subfinder -> httpx -> nuclei must be legal: %v", err)
	}
}

// **THE failure this rule exists for.** A tool consuming `url` fed by a step
// producing `host` yields zero candidates every time (0039) — and the step is
// `skipped` with the reason "nothing upstream produced observations to feed it",
// which is indistinguishable from a feeder that genuinely found nothing.
func TestAnEdgeThatCarriesNothingIsRefused(t *testing.T) {
	chain, feeds := realistic()
	feeds[nonZero(2)] = domain.Feeds{Consumes: "url", Produces: "url"}
	if err := chain.TypeLegal(feeds); !errors.Is(err, domain.ErrEdgeMismatched) {
		t.Fatalf("want ErrEdgeMismatched, got %v", err)
	}
}

// A source tool is seeded from the target and cannot be fed by another. Wiring
// something into subfinder is a mistake a canvas makes easy.
func TestASourceToolCannotBeFed(t *testing.T) {
	chain := domain.Chain{
		Steps: []domain.Step{{ID: nonZero(1)}, {ID: nonZero(2)}},
		Flows: []domain.Flow{{From: nonZero(1), To: nonZero(2)}},
	}
	feeds := map[id.ID]domain.Feeds{
		nonZero(1): {Produces: "host"},
		nonZero(2): {Produces: "host"}, // consumes NOTHING — a second source
	}
	if err := chain.TypeLegal(feeds); !errors.Is(err, domain.ErrEdgeConsumesNothing) {
		t.Fatalf("want ErrEdgeConsumesNothing, got %v", err)
	}
}

// A tool whose output nothing can read cannot feed anything — `nuclei` into
// something else, which is the mirror of the case above and the one a person
// draws when they think a finding flows onward.
func TestAToolThatProducesNothingReadableCannotFeed(t *testing.T) {
	chain := domain.Chain{
		Steps: []domain.Step{{ID: nonZero(1)}, {ID: nonZero(2)}},
		Flows: []domain.Flow{{From: nonZero(1), To: nonZero(2)}},
	}
	feeds := map[id.ID]domain.Feeds{
		nonZero(1): {Consumes: "url"}, // produces NOTHING
		nonZero(2): {Consumes: "url", Produces: "finding"},
	}
	if err := chain.TypeLegal(feeds); !errors.Is(err, domain.ErrEdgeProducesNothing) {
		t.Fatalf("want ErrEdgeProducesNothing, got %v", err)
	}
}

// A chain with no flows is trivially legal — a check that is one tool is the
// common case and must not need feeds at all.
func TestAChainWithNoEdgesIsLegal(t *testing.T) {
	chain := domain.Chain{Steps: []domain.Step{{ID: nonZero(1)}}}
	if err := chain.TypeLegal(map[id.ID]domain.Feeds{nonZero(1): {Produces: "host"}}); err != nil {
		t.Fatalf("one step is no edges: %v", err)
	}
	// And an EMPTY chain — the human check — is legal with no feeds at all.
	if err := (domain.Chain{}).TypeLegal(nil); err != nil {
		t.Fatalf("the human check has no chain: %v", err)
	}
}

// A flow naming a step whose feeds were not resolved is a caller bug, and it
// reports as an unknown step rather than as a type error — reporting the wrong
// problem is what sends somebody to fix the wrong thing.
func TestAnUnresolvedStepIsAnUnknownStep(t *testing.T) {
	chain := domain.Chain{
		Steps: []domain.Step{{ID: nonZero(1)}, {ID: nonZero(2)}},
		Flows: []domain.Flow{{From: nonZero(1), To: nonZero(2)}},
	}
	// BOTH ENDS, separately. A mutation round found the `from` guard unkilled:
	// this test only omitted the `to` step, so the second guard caught it and
	// the first was never exercised.
	for _, tc := range []struct {
		name  string
		feeds map[id.ID]domain.Feeds
	}{
		{"the downstream is unresolved", map[id.ID]domain.Feeds{
			nonZero(1): {Produces: "host"}}},
		{"the upstream is unresolved", map[id.ID]domain.Feeds{
			nonZero(2): {Consumes: "host"}}},
		{"neither is", map[id.ID]domain.Feeds{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := chain.TypeLegal(tc.feeds); !errors.Is(err, domain.ErrStepUnknown) {
				t.Fatalf("want ErrStepUnknown, got %v", err)
			}
		})
	}
}
