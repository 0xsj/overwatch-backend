// Author-written, from decisions/0040 §5. It exists because a mutation round
// found the store's fold unkilled: every other test of the provenance path uses
// an in-memory fake that folds on its own, so the REAL lookup's fold was
// asserted by nothing.
//
// It needs a database and SKIPS without one, which is this package's existing
// rule (`pkg/testx`). A skipped test is a result and is reported as one.
package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/entity/domain"
	entitypg "github.com/0xsj/overwatch-backend/internal/entity/infra/postgres"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/testx"
)

// A derivation resolves its `from` by looking a value up in `entity.fragment`.
// The column is folded and an observation's VALUE deliberately is not — it is
// what the source said — so the fold has to happen in the lookup, and if it does
// not, every tool that echoes a differently-cased host produces an unresolved
// row instead of an edge.
//
// 0037 named the fold mismatch as its own quiet failure. This is the same join
// one domain over.
func TestTheFragmentLookupFoldsWhatItIsGiven(t *testing.T) {
	p := testx.Postgres(t, testx.Schema{Name: entitypg.Schema, Migrations: entitypg.Migrations})
	store := entitypg.NewStore(p)
	ctx := context.Background()

	space := id.ID{}
	space[0] = 0x10
	at := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	fresh, err := domain.NewFragment(newID(0x20), space, "host", "a.acme.test",
		domain.Observed, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Upsert(ctx, fresh); err != nil {
		t.Fatal(err)
	}

	// What `httpx` would echo in `.input`, stored verbatim as an observation
	// value and handed to the lookup exactly as the tool wrote it.
	for _, raw := range []string{"a.acme.test", "A.ACME.Test", "  a.Acme.Test  "} {
		got, ok, err := store.FragmentFor(ctx, space, "host", raw)
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		if !ok {
			t.Fatalf("%q did not resolve — a case difference is the same host", raw)
		}
		if got.Value != "a.acme.test" {
			t.Fatalf("%q resolved to %q", raw, got.Value)
		}
	}

	// AND A GENUINE MISS IS STILL A MISS. A lookup that folded so hard it
	// matched anything would make the unresolved row unreachable, which is the
	// half 0040 §5 exists for.
	if _, ok, err := store.FragmentFor(ctx, space, "host", "b.acme.test"); err != nil || ok {
		t.Fatalf("an absent host must not resolve: ok=%v err=%v", ok, err)
	}
	// The KIND is part of the tuple and is not folded away either.
	if _, ok, err := store.FragmentFor(ctx, space, "url", "a.acme.test"); err != nil || ok {
		t.Fatalf("a fragment is a KIND and a value: ok=%v err=%v", ok, err)
	}
}

func newID(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}
