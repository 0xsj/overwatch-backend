package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	journalapp "github.com/0xsj/overwatch-backend/internal/journal/app"
	journalpg "github.com/0xsj/overwatch-backend/internal/journal/infra/postgres"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

var at = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return at }

func store(t *testing.T) *journalpg.Store {
	t.Helper()
	dsn := os.Getenv("OVERWATCH_TEST_DSN")
	if dsn == "" {
		t.Skip("OVERWATCH_TEST_DSN is unset — run `make test-db`")
	}
	ctx := context.Background()
	p, err := postgres.Open(ctx, postgres.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(p.Close)
	if _, err := p.DB(ctx).Exec(ctx, "drop schema if exists journal cascade"); err != nil {
		t.Fatalf("drop schema: %v", err)
	}
	if _, err := postgres.Migrate(ctx, p, journalpg.Migrations, postgres.InSchema(journalpg.Schema)); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return journalpg.NewStore(p)
}

func ids() *id.Sequence { return id.NewSequence(at) }

func user(t *testing.T, m *id.Sequence, who string) provenance.Provenance {
	t.Helper()
	a, err := provenance.User(who)
	if err != nil {
		t.Fatal(err)
	}
	return provenance.New(provenance.OriginRequest, m).WithActor(a)
}

func service(t *testing.T, m *id.Sequence, name string) provenance.Provenance {
	t.Helper()
	a, err := provenance.Service(name)
	if err != nil {
		t.Fatal(err)
	}
	return provenance.New(provenance.OriginSchedule, m).WithActor(a)
}

// The difference from audit's, in one test: the journal has no filter. Work and
// decisions both land, and the `decision` column is what lets a reader join the
// two tables rather than choose between them.
func TestEveryUnitOfWorkIsALineWhetherOrNotItIsADecision(t *testing.T) {
	s := store(t)
	ctx := context.Background()
	m := ids()
	sub := journalapp.NewSubscriber(s, m, clock.System{})

	work, err := events.New(m, fixedClock{}, "runner.invocation.finished", "invocation:i1", service(t, m, "runner"), nil)
	if err != nil {
		t.Fatal(err)
	}
	ruling, err := events.NewDecision(m, fixedClock{}, "finding.judgement.set", "finding:f7", user(t, m, "acct_sj"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []events.Event{work, ruling} {
		if err := sub.Handle(ctx, e); err != nil {
			t.Fatalf("%s: %v", e.Name, err)
		}
	}

	lines, err := s.Recent(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("the journal holds %d lines, want both", len(lines))
	}
	var sawWork, sawDecision bool
	for _, l := range lines {
		switch l.Action {
		case "runner.invocation.finished":
			sawWork = true
			if l.Decision {
				t.Error("machine work was recorded as a decision")
			}
			if l.Origin != "schedule" || l.Actor != "service:runner" {
				t.Errorf("origin=%q actor=%q", l.Origin, l.Actor)
			}
		case "finding.judgement.set":
			sawDecision = true
			if !l.Decision {
				t.Error("a decision lost its flag in the table")
			}
		}
	}
	if !sawWork || !sawDecision {
		t.Error("one of the two did not land")
	}
}

// The Chain toggle: everything one request caused, in order. This is the
// property that makes "what caused this" answerable at all.
func TestACorrelationGathersTheWholeChainInOrder(t *testing.T) {
	s := store(t)
	ctx := context.Background()
	m := ids()
	sub := journalapp.NewSubscriber(s, m, clock.System{})

	root := service(t, m, "sched/nightly")
	child, err := root.Derive(m)
	if err != nil {
		t.Fatal(err)
	}
	grandchild, err := child.Derive(m)
	if err != nil {
		t.Fatal(err)
	}

	for i, p := range []provenance.Provenance{root, child, grandchild} {
		name := []string{"runner.run.started", "runner.invocation.finished", "extract.observation.created"}[i]
		e, err := events.New(m, fixedClock{}, name, "run:r119", p, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := sub.Handle(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	chain, err := s.ForCorrelation(ctx, root.Correlation())
	if err != nil {
		t.Fatal(err)
	}
	if len(chain) != 3 {
		t.Fatalf("the chain has %d lines, want 3 — a derived provenance must keep the correlation", len(chain))
	}
	for i, want := range []int{0, 1, 2} {
		if chain[i].Depth != want {
			t.Errorf("line %d is at depth %d, want %d", i, chain[i].Depth, want)
		}
	}
}

func TestARedeliveredEventWritesOneLine(t *testing.T) {
	s := store(t)
	ctx := context.Background()
	m := ids()
	sub := journalapp.NewSubscriber(s, m, clock.System{})
	e, err := events.New(m, fixedClock{}, "ingest.artifact.stored", "artifact:sha256:4f2b9c", service(t, m, "ingest"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := sub.Handle(ctx, e); err != nil {
			t.Fatalf("delivery %d: %v", i+1, err)
		}
	}
	lines, err := s.Recent(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Errorf("three deliveries wrote %d lines", len(lines))
	}
}

// decisions/0014: the journal is expirable and audit is not. This is that claim
// made real — and the sweep must never take a decision, because those are the
// rows the ledger's own copy is joined to.
func TestTheSweepTakesOldWorkAndNeverADecision(t *testing.T) {
	s := store(t)
	ctx := context.Background()
	m := ids()
	sub := journalapp.NewSubscriber(s, m, clock.System{})

	old := &backdated{at: at.Add(-90 * 24 * time.Hour)}
	fresh := &backdated{at: at}

	oldWork, err := events.New(m, old, "ingest.artifact.stored", "artifact:a1", service(t, m, "ingest"), nil)
	if err != nil {
		t.Fatal(err)
	}
	oldDecision, err := events.NewDecision(m, old, "report.generated", "report:r1", user(t, m, "acct_sj"), nil)
	if err != nil {
		t.Fatal(err)
	}
	freshWork, err := events.New(m, fresh, "ingest.artifact.stored", "artifact:a2", service(t, m, "ingest"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []events.Event{oldWork, oldDecision, freshWork} {
		if err := sub.Handle(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	n, err := s.ExpireBefore(ctx, at.Add(-30*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("the sweep took %d lines, want 1 — the old decision must survive it", n)
	}

	left, err := s.Recent(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 2 {
		t.Fatalf("%d lines left, want the old decision and the fresh work", len(left))
	}
	var keptDecision bool
	for _, l := range left {
		if l.Action == "report.generated" {
			keptDecision = true
		}
	}
	if !keptDecision {
		t.Error("the sweep deleted a decision")
	}
}

type backdated struct{ at time.Time }

func (b *backdated) Now() time.Time { return b.at }
