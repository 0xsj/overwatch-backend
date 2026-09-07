package main

import (
	"crypto/rand"
	"fmt"
	"os"
	"strings"

	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

var ids = id.NewV7(clock.System{}, rand.Reader)

func main() {
	title("1 · a person clicks Run now",
		"An origin is minted, not adopted. At an origin the correlation IS the",
		"root's request id — a chain's root and the chain are the same thing",
		"named twice, so no second identifier exists to drift.")

	sj := actor(provenance.User("acct_sj"))
	root := provenance.New(provenance.OriginRequest, ids).WithActor(sj)
	root = tenanted(root, "ws_ctf1")
	show("run.requested", root)

	run := derive(root)
	show("runner.run.started", run)

	httpx := derive(run)
	show("runner.invocation.finished  httpx", httpx)
	nuclei := derive(run)
	show("runner.invocation.finished  nuclei", nuclei)

	// DeriveFrom is the pipeline case: a subscriber takes correlation from the
	// envelope and causation from the EVENT's id, never from the parent's
	// request. Minting a fresh correlation here is the break that is invisible
	// until somebody asks a question that crosses it.
	eventID := ids.NewID()
	obs := deriveFrom(httpx, eventID)
	show("extract.observation.created", obs)

	note("Two invocations at depth 2 are siblings: same correlation, same",
		"causation, different requests. Correlation alone cannot tell that",
		"from a chain of two.")

	title("2 · the nightly sweep",
		"The same shape, and every identifier is different. What changed is",
		"ORIGIN — why the chain exists — which is a separate question from",
		"the actor, who is now a service rather than a person.")

	sweep := provenance.New(provenance.OriginSchedule, ids).
		WithActor(actor(provenance.Service("sched/nightly")))
	show("runner.run.started", sweep)
	show("runner.invocation.finished  subfinder", derive(sweep))

	note("coverage is one of this product's nouns. \"This target was scanned\"",
		"means something different when the chain was a nightly sweep than when",
		"somebody clicked a button, and a coverage claim that cannot say which",
		"is a claim nobody can act on.")

	title("3 · a redelivery, and a report generated for a client",
		"Retry is not a derive. A redelivery is the SAME unit of work arriving",
		"again, so only attempt moves. Deriving instead would make a poison",
		"message look like a widening tree.")

	once := derive(sweep)
	show("ingest.artifact.parsed", once)
	show("ingest.artifact.parsed  (redelivered)", once.Retry())
	show("ingest.artifact.parsed  (again)", once.Retry().Retry())

	// on_behalf_of is the pair CLAUDE.md refuses to collapse: who acted, and
	// whose account it affects.
	delegated, err := provenance.New(provenance.OriginRequest, ids).
		WithActor(sj).
		WithOnBehalfOf(actor(provenance.User("acct_client")))
	if err != nil {
		fail(err)
	}
	show("report.generated", tenanted(delegated, "ws_acme_q3"))

	title("4 · an inbound boundary, and why depth 0 is not always a root",
		"Adopt takes an upstream correlation once, at an edge. After it, this",
		"value is at depth 0 and is NOT a root: it opened this process's work,",
		"it did not open the chain.")

	upstream := ids.NewID().String()
	edge := provenance.New(provenance.OriginRequest, ids).Adopt(provenance.Adopted{
		Correlation: upstream,
		Traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	})
	show("api.request.received", edge)
	fmt.Printf("        Root() == %v      depth 0, and correlation != request\n", edge.Root())

	note("Adopted carries no depth and no attempt, deliberately. They are the",
		"two fields something branches on, so a caller cannot reset a bound it",
		"is subject to — the compiler enforcing the rule rather than a comment",
		"asking for it.")

	title("5 · depth is a bound, not a statistic",
		"An event pipeline can form a cycle — a judgement raises an event, the",
		"event re-enters attribution, attribution raises a judgement. Every",
		"record in that cycle shares one correlation BY DESIGN, so no amount of",
		"inspecting identifiers finds it. The hop count does.")

	p := provenance.New(provenance.OriginRequest, ids)
	for hop := 0; ; hop++ {
		next, err := p.Derive(ids)
		if err != nil {
			fmt.Printf("  depth %-3d %v\n", p.Depth(), err)
			fmt.Printf("        refused rather than returned, because a chain this deep is a\n")
			fmt.Printf("        cycle in every case anyone has produced, and a usable value\n")
			fmt.Printf("        there makes the runaway cheaper to continue than to stop.\n")
			break
		}
		p = next
	}
	fmt.Printf("        MaxDepth = %d\n\n", provenance.MaxDepth)
}

// ── the plumbing, kept out of the way ──────────────────────────────────

func show(what string, p provenance.Provenance) {
	pad := strings.Repeat("   ", int(p.Depth()))
	fmt.Printf("  %sd%d %-34s req %s\n", pad, p.Depth(), what, short(p.Request()))
	fmt.Printf("  %s      corr %s   caus %s\n", pad, short(p.Correlation()), short(p.Causation()))
	line := fmt.Sprintf("  %s      origin %s  actor %s  attempt %d",
		pad, p.Origin(), p.Actor(), p.Attempt())
	if p.Delegated() {
		line += "  on_behalf_of " + p.OnBehalfOf().String()
	}
	if p.Tenant() != "" {
		line += "  tenant " + p.Tenant()
	}
	fmt.Println(line)
	fmt.Println()
}

func short(i id.ID) string {
	if i.IsZero() {
		return "—                   "
	}
	return i.String()[:20]
}

func derive(p provenance.Provenance) provenance.Provenance {
	next, err := p.Derive(ids)
	if err != nil {
		fail(err)
	}
	return next
}

func deriveFrom(p provenance.Provenance, cause id.ID) provenance.Provenance {
	next, err := p.DeriveFrom(ids, cause)
	if err != nil {
		fail(err)
	}
	return next
}

func tenanted(p provenance.Provenance, t string) provenance.Provenance {
	next, err := p.WithTenant(t)
	if err != nil {
		fail(err)
	}
	return next
}

func actor(a provenance.Actor, err error) provenance.Actor {
	if err != nil {
		fail(err)
	}
	return a
}

func title(heading string, lines ...string) {
	fmt.Printf("\n\033[1m%s\033[0m\n", heading)
	for _, l := range lines {
		fmt.Printf("  %s\n", l)
	}
	fmt.Println()
}

func note(lines ...string) {
	for i, l := range lines {
		if i == 0 {
			fmt.Printf("     ─ %s\n", l)
			continue
		}
		fmt.Printf("       %s\n", l)
	}
	fmt.Println()
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", errors.KindOf(err), err)
	os.Exit(1)
}
