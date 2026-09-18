// Author-written, from decisions/0039's Verification block — which was written
// before this code, and is the checklist below.
//
// It is HERE and not in root/ because the executor is where a downstream step is
// resolved, and root's end-to-end harness deliberately never ticks it: those
// tests exercise the request that PLANS a run, and 0033 §1's whole point is that
// planning spawns nothing.
package command_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/run/app/command"
	"github.com/0xsj/overwatch-backend/internal/run/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/execx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var at = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

func nonZero(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}

// The chain every test here runs: subfinder feeds httpx. Two steps, one flow,
// which is the smallest graph that can show a step being fed.
var (
	source = nonZero(0x11)
	sink   = nonZero(0x12)
	tool1  = nonZero(0x21)
	tool2  = nonZero(0x22)
	space  = nonZero(0x31)
	aim    = nonZero(0x32)
	quiz   = nonZero(0x33)
	rule   = nonZero(0x41)
	block  = nonZero(0x42)
)

// ---------------------------------------------------------------- the fakes

// store is an in-memory Repository. It keeps candidates in insertion order,
// because the argv's order is a claim these tests make.
type store struct {
	runs        map[id.ID]domain.Run
	invocations []domain.Invocation
	candidates  []domain.Candidate
	claimed     bool
}

func newStore() *store { return &store{runs: map[id.ID]domain.Run{}} }

func (s *store) Create(_ context.Context, r domain.Run) error {
	s.runs[r.ID] = r
	return nil
}

func (s *store) ByID(_ context.Context, _, want id.ID) (domain.Run, error) {
	return s.runs[want], nil
}

func (s *store) Finish(_ context.Context, r domain.Run) error {
	s.runs[r.ID] = r
	return nil
}

// Claim answers once, so a test's tick does not loop forever on its own run.
func (s *store) Claim(_ context.Context, _ int) ([]domain.Run, error) {
	if s.claimed {
		return nil, nil
	}
	s.claimed = true
	out := make([]domain.Run, 0, len(s.runs))
	for _, r := range s.runs {
		if r.State == domain.StateRunning {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *store) PlanInvocation(_ context.Context, i domain.Invocation) error {
	s.invocations = append(s.invocations, i)
	return nil
}

func (s *store) SaveInvocation(_ context.Context, i domain.Invocation) error {
	for n := range s.invocations {
		if s.invocations[n].ID == i.ID {
			s.invocations[n] = i
			return nil
		}
	}
	s.invocations = append(s.invocations, i)
	return nil
}

func (s *store) Invocations(_ context.Context, run id.ID) ([]domain.Invocation, error) {
	out := make([]domain.Invocation, 0, len(s.invocations))
	for _, i := range s.invocations {
		if i.RunID == run {
			out = append(out, i)
		}
	}
	return out, nil
}

// AddCandidate is idempotent on (invocation, kind, value), matching the unique
// index. A fake that let a duplicate through would hide the thing the index is
// there to stop.
func (s *store) AddCandidate(_ context.Context, c domain.Candidate) error {
	for _, held := range s.candidates {
		if held.InvocationID == c.InvocationID && held.Kind == c.Kind && held.Value == c.Value {
			return nil
		}
	}
	s.candidates = append(s.candidates, c)
	return nil
}

func (s *store) AddArtifact(_ context.Context, _ domain.Artifact) error { return nil }

func (s *store) of(invocation id.ID) []domain.Candidate {
	out := make([]domain.Candidate, 0, 2)
	for _, c := range s.candidates {
		if c.InvocationID == invocation {
			out = append(out, c)
		}
	}
	return out
}

func (s *store) step(t *testing.T, step id.ID) domain.Invocation {
	t.Helper()
	for _, i := range s.invocations {
		if i.StepID == step {
			return i
		}
	}
	t.Fatalf("no invocation for step %s", step)
	return domain.Invocation{}
}

type chainOf struct{ steps []domain.Step }

func (c chainOf) Steps(_ context.Context, _, _ id.ID) ([]domain.Step, bool, error) {
	return c.steps, false, nil
}

type namedTarget struct{}

func (namedTarget) Name(_ context.Context, _, _ id.ID) (string, error) { return "acme.test", nil }

// gate answers per VALUE, which is the whole point of 0039 §2: each host is
// permitted or not on its own.
type gate struct {
	excluded map[string]bool
	nothing  bool
	asked    []string
}

func (g *gate) MaySpawn(_ context.Context, _, _ id.ID, _, value, _ string) (domain.Gate, error) {
	g.asked = append(g.asked, value)
	switch {
	case g.nothing:
		return domain.Gate{Reason: "nothing in this target's scope permits " + value}, nil
	case g.excluded[value]:
		return domain.Gate{Rule: block, Reason: "rule excludes " + value}, nil
	default:
		return domain.Gate{Permitted: true, Rule: rule}, nil
	}
}

type toolbox struct{}

func (toolbox) Succeeded(_ context.Context, _, _ id.ID, exit int) (bool, error) {
	return exit == 0, nil
}
func (toolbox) MediaType(_ context.Context, _, _ id.ID) (string, error) { return "text/plain", nil }

type orgOf struct{}

func (orgOf) OrgOf(_ context.Context, _ id.ID) (id.ID, error) { return nonZero(0x51), nil }

// spawner records every argv it was handed, which is how these tests read what
// actually ran.
type spawner struct{ argv [][]string }

func (s *spawner) Spawn(_ context.Context, argv []string, _ execx.Policy) (execx.Result, error) {
	s.argv = append(s.argv, argv)
	return execx.Result{Argv: argv, Binary: "/usr/bin/" + argv[0], Outcome: execx.Ran,
		ExitCode: 0, Stdout: []byte("out")}, nil
}

type blobs struct{}

// Put answers a CONTENT ADDRESS, because an artifact refuses to exist without
// one and a fake that returned "" would fail every test for a reason unrelated
// to what it asserts.
func (blobs) Put(_ context.Context, r io.Reader) (string, int64, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:]), int64(len(body)), nil
}

type extracts struct{}

func (extracts) Extract(_ context.Context, _ command.Extraction) (int, error) { return 0, nil }

// observed is what the feeders saw, keyed by invocation. `found` is consulted
// per invocation so a test can give two feeders overlapping answers and watch
// them union.
type observed struct {
	found map[id.ID][]string
	kinds map[id.ID]string
	asked [][]id.ID
}

func (o *observed) Subjects(_ context.Context, _ id.ID, invocations []id.ID, kind string) ([]string, error) {
	o.asked = append(o.asked, invocations)
	out := []string{}
	for _, i := range invocations {
		if o.kinds[i] != "" && o.kinds[i] != kind {
			// A tool declaring `consumes: url` fed by a step producing hosts
			// gets nothing. 0039 names this as the claim that fails QUIETLY.
			continue
		}
		out = append(out, o.found[i]...)
	}
	return out, nil
}

type direct struct{}

func (direct) InTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

type transactionProbe struct{ active bool }

func (p *transactionProbe) InTx(ctx context.Context, fn func(context.Context) error) error {
	p.active = true
	defer func() { p.active = false }()
	return fn(ctx)
}

type transactionProbeSpawner struct {
	active    *bool
	sawActive bool
}

func (s *transactionProbeSpawner) Spawn(_ context.Context, argv []string, _ execx.Policy) (execx.Result, error) {
	if *s.active {
		s.sawActive = true
	}
	return execx.Result{Argv: argv, Binary: "/usr/bin/" + argv[0], Outcome: execx.Ran,
		ExitCode: 0, Stdout: []byte("out")}, nil
}

type minter struct{ n byte }

func (m *minter) NewID() id.ID {
	m.n++
	return nonZero(0x80 + m.n)
}

type frozen struct{}

func (frozen) Now() time.Time { return at }

type quiet struct{}

func (quiet) Publish(_ context.Context, _ ...events.Event) error { return nil }

type noise struct{ errs []string }

func (n *noise) Error(msg string, _ ...any) { n.errs = append(n.errs, msg) }
func (n *noise) Info(string, ...any)        {}

// ---------------------------------------------------------------- the rig

type rig struct {
	store    *store
	gate     *gate
	observed *observed
	spawner  *spawner
	log      *noise
	runs     *command.Runs
	executor *command.Executor
}

// build wires a two-step chain: `subfinder -d {{target}}` producing hosts, into
// `httpx -u {{host}}` consuming them.
func build(t *testing.T, seen map[id.ID][]string, kinds map[id.ID]string, g *gate) *rig {
	t.Helper()
	repo := newStore()
	obs := &observed{found: seen, kinds: kinds}
	spawn := &spawner{}
	log := &noise{}
	ids := &minter{}
	chain := chainOf{steps: []domain.Step{
		{StepID: source, ToolID: tool1, Template: "subfinder -d {{target}}",
			Source: true, Kind: "host", Intensity: "passive"},
		{StepID: sink, ToolID: tool2, Template: "httpx -u {{host}}",
			Kind: "host", Intensity: "passive", Upstream: []id.ID{source}},
	}}
	runs := command.NewRuns(repo, chain, namedTarget{}, g, direct{}, quiet{}, ids, frozen{})
	return &rig{
		store: repo, gate: g, observed: obs, spawner: spawn, log: log, runs: runs,
		executor: command.NewExecutor(repo, runs, toolbox{}, orgOf{}, spawn,
			blobs{}, extracts{}, obs, direct{}, quiet{}, ids, frozen{},
			execx.Policy{}, 4, time.Second, log),
	}
}

// ---------------------------------------------------------------- the claims

// 0039 §2 and §3, and the shape every other test here rests on: one invocation
// per step planned up front, a source step with exactly one candidate, and a
// downstream step left pending with its template unresolved.
func TestAPlanIsOneInvocationPerStepAndOneCandidateAtTheSource(t *testing.T) {
	r := build(t, nil, nil, &gate{})
	planned, err := r.runs.Start(context.Background(), space, aim, quiz, nonZero(9))
	if err != nil {
		t.Fatal(err)
	}
	if len(planned.Invocations) != 2 {
		t.Fatalf("0033 §1: every step gets a row, got %d", len(planned.Invocations))
	}
	if len(planned.Candidates) != 1 {
		t.Fatalf("only the source step is aimed at anything yet, got %d", len(planned.Candidates))
	}
	only := planned.Candidates[0]
	if only.Kind != "host" || only.Value != "acme.test" || !only.Permitted {
		t.Fatalf("the source candidate is the permitted target, got %+v", only)
	}
	downstream := r.store.step(t, sink)
	if downstream.Phase != domain.PhasePending {
		t.Fatalf("a downstream step waits rather than being skipped, got %s", downstream.Phase)
	}
	if got := strings.Join(downstream.Argv, " "); got != "httpx -u {{host}}" {
		t.Fatalf("0039 §4: a planned argv holds the template, got %q", got)
	}
	if len(downstream.Upstream) != 1 || downstream.Upstream[0] != source {
		t.Fatalf("what feeds it is recorded at plan time, got %v", downstream.Upstream)
	}
}

func TestTickDoesNotHoldClaimTransactionDuringExternalWork(t *testing.T) {
	r := build(t, nil, nil, &gate{})
	if _, err := r.runs.Start(context.Background(), space, aim, quiz, nonZero(9)); err != nil {
		t.Fatal(err)
	}
	probe := &transactionProbe{}
	spawner := &transactionProbeSpawner{active: &probe.active}
	r.executor = command.NewExecutor(r.store, r.runs, toolbox{}, orgOf{}, spawner,
		blobs{}, extracts{}, r.observed, probe, quiet{}, &minter{}, frozen{},
		execx.Policy{}, 4, time.Second, r.log)
	if err := r.executor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if spawner.sawActive {
		t.Fatal("external tool execution ran while the claim transaction was active")
	}
}

// 0039 §3, the headline: a downstream step's candidates are what its feeders
// observed, and the permitted ones are what the argv carries. This is `0033`
// §5's stated future arriving.
func TestADownstreamStepRunsOnWhatItsFeederFound(t *testing.T) {
	r := build(t, nil, nil, &gate{})
	planned, err := r.runs.Start(context.Background(), space, aim, quiz, nonZero(9))
	if err != nil {
		t.Fatal(err)
	}
	// What step one will have observed by the time step two resolves.
	feeder := r.store.step(t, source).ID
	r.observed.found = map[id.ID][]string{feeder: {"b.acme.test", "a.acme.test", "a.acme.test"}}

	if err := r.executor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	ran := r.store.step(t, sink)
	if ran.Phase != domain.PhaseOK {
		t.Fatalf("want ok, got %s (%s)", ran.Phase, ran.SkippedBecause+ran.RefusalReason)
	}
	if got := strings.Join(ran.Argv, " "); got != "httpx -u a.acme.test b.acme.test" {
		t.Fatalf("0039 §4: a run argv holds what ran, got %q", got)
	}
	if got := r.store.of(ran.ID); len(got) != 2 {
		t.Fatalf("duplicates fold to one candidate each, got %d", len(got))
	}
	if len(r.observed.asked) != 1 || len(r.observed.asked[0]) != 1 ||
		r.observed.asked[0][0] != feeder {
		t.Fatalf("it asked the wrong invocations: %v", r.observed.asked)
	}
	// The run finished, and finished COMPLETE.
	if r.store.runs[planned.Run.ID].State != domain.StateComplete {
		t.Fatalf("want complete, got %s", r.store.runs[planned.Run.ID].State)
	}
}

// 0039 §2: a refused candidate is a ROW citing its rule, and is ABSENT from the
// argv. That absence is the scope proof, and it is the reason this is a table
// rather than an array of what was permitted.
func TestARefusedCandidateIsARowAndIsNotInTheArgv(t *testing.T) {
	g := &gate{excluded: map[string]bool{"b.acme.test": true}}
	r := build(t, nil, nil, g)
	if _, err := r.runs.Start(context.Background(), space, aim, quiz, nonZero(9)); err != nil {
		t.Fatal(err)
	}
	feeder := r.store.step(t, source).ID
	r.observed.found = map[id.ID][]string{feeder: {"a.acme.test", "b.acme.test", "c.acme.test"}}

	if err := r.executor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	ran := r.store.step(t, sink)
	if ran.Phase != domain.PhaseOK {
		t.Fatalf("two of three survived, so it runs: got %s", ran.Phase)
	}
	if got := strings.Join(ran.Argv, " "); got != "httpx -u a.acme.test c.acme.test" {
		t.Fatalf("the refused host must not be in the argv, got %q", got)
	}
	var refused []domain.Candidate
	for _, c := range r.store.of(ran.ID) {
		if !c.Permitted {
			refused = append(refused, c)
		}
	}
	if len(refused) != 1 || refused[0].Value != "b.acme.test" {
		t.Fatalf("want one refused row, got %+v", refused)
	}
	if refused[0].RefusalRule != block {
		t.Fatalf("a refusal cites the rule that caused it, got %v", refused[0].RefusalRule)
	}
	if refused[0].RefusalReason == "" {
		t.Fatal("a refusal always says why")
	}
}

// 0039 §2 and the Verification block: a step whose candidates are ALL refused
// does not spawn, and is not `ok`.
func TestAStepWhoseCandidatesAreAllRefusedDoesNotSpawn(t *testing.T) {
	g := &gate{excluded: map[string]bool{"a.acme.test": true, "b.acme.test": true}}
	r := build(t, nil, nil, g)
	if _, err := r.runs.Start(context.Background(), space, aim, quiz, nonZero(9)); err != nil {
		t.Fatal(err)
	}
	feeder := r.store.step(t, source).ID
	r.observed.found = map[id.ID][]string{feeder: {"a.acme.test", "b.acme.test"}}

	before := len(r.spawner.argv)
	if err := r.executor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	ran := r.store.step(t, sink)
	if ran.Phase != domain.PhaseRefused {
		t.Fatalf("want refused, got %s", ran.Phase)
	}
	if ran.RefusalRule != block {
		t.Fatalf("the invocation cites a rule, got %v", ran.RefusalRule)
	}
	if ran.HasExitCode {
		t.Fatal("nothing ran, so there is no exit code")
	}
	// ONE spawn — step one's. Step two never reached the spawner.
	if len(r.spawner.argv) != before+1 {
		t.Fatalf("the refused step spawned: %v", r.spawner.argv)
	}
	if got := len(r.store.of(ran.ID)); got != 2 {
		t.Fatalf("both refusals are rows, got %d", got)
	}
}

// 0039 §3: a downstream step whose feeders produced nothing is SKIPPED with a
// reason — and the reason stops being a lie. It said the same words before this
// record, because nothing could ever feed it.
func TestADownstreamStepWithNothingUpstreamIsSkipped(t *testing.T) {
	r := build(t, map[id.ID][]string{}, nil, &gate{})
	if _, err := r.runs.Start(context.Background(), space, aim, quiz, nonZero(9)); err != nil {
		t.Fatal(err)
	}
	if err := r.executor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	ran := r.store.step(t, sink)
	if ran.Phase != domain.PhaseSkipped {
		t.Fatalf("want skipped, got %s", ran.Phase)
	}
	if ran.SkippedBecause != domain.SkipNothingUpstream {
		t.Fatalf("a skip names what did not arrive, got %q", ran.SkippedBecause)
	}
	if len(r.store.of(ran.ID)) != 0 {
		t.Fatal("nothing was aimed at, so there are no candidates")
	}
	// AND THE GATE WAS NEVER ASKED. There is no spawn to ask about, and a
	// refusal recorded here would put a rule citation on a row proving nothing.
	if len(r.gate.asked) != 1 {
		t.Fatalf("the gate was asked about a step with nothing to run: %v", r.gate.asked)
	}
}

// 0039 §3: several feeders UNION rather than the second replacing the first, and
// the result is ordered so an argv is a function of what was FOUND.
func TestTwoFeedersUnion(t *testing.T) {
	r := build(t, nil, nil, &gate{})
	// A second source feeding the same sink.
	second := nonZero(0x13)
	r.runs = command.NewRuns(r.store, chainOf{steps: []domain.Step{
		{StepID: source, ToolID: tool1, Template: "subfinder -d {{target}}",
			Source: true, Kind: "host", Intensity: "passive"},
		{StepID: second, ToolID: tool1, Template: "amass -d {{target}}",
			Source: true, Kind: "host", Intensity: "passive"},
		{StepID: sink, ToolID: tool2, Template: "httpx -u {{host}}",
			Kind: "host", Intensity: "passive", Upstream: []id.ID{source, second}},
	}}, namedTarget{}, r.gate, direct{}, quiet{}, &minter{n: 0x10}, frozen{})
	r.executor = command.NewExecutor(r.store, r.runs, toolbox{}, orgOf{}, r.spawner,
		blobs{}, extracts{}, r.observed, direct{}, quiet{}, &minter{n: 0x40}, frozen{},
		execx.Policy{}, 4, time.Second, r.log)

	if _, err := r.runs.Start(context.Background(), space, aim, quiz, nonZero(9)); err != nil {
		t.Fatal(err)
	}
	r.observed.found = map[id.ID][]string{
		r.store.step(t, source).ID: {"b.acme.test", "shared.acme.test"},
		r.store.step(t, second).ID: {"a.acme.test", "shared.acme.test"},
	}
	if err := r.executor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	ran := r.store.step(t, sink)
	want := "httpx -u a.acme.test b.acme.test shared.acme.test"
	if got := strings.Join(ran.Argv, " "); got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

// The claim 0039 says fails QUIETLY: a tool consuming a kind no observation
// carries yields zero candidates every time, and is indistinguishable from a
// step whose feeder found nothing. It is pinned here so the day the chain's
// edge-legality check lands (owed item I), this is the test that changes.
func TestAKindNoObservationCarriesLooksExactlyLikeFindingNothing(t *testing.T) {
	r := build(t, nil, map[id.ID]string{}, &gate{})
	if _, err := r.runs.Start(context.Background(), space, aim, quiz, nonZero(9)); err != nil {
		t.Fatal(err)
	}
	feeder := r.store.step(t, source).ID
	r.observed.found = map[id.ID][]string{feeder: {"https://a.acme.test/"}}
	// The feeder produced URLs; this step consumes hosts.
	r.observed.kinds = map[id.ID]string{feeder: "url"}

	if err := r.executor.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	ran := r.store.step(t, sink)
	if ran.Phase != domain.PhaseSkipped || ran.SkippedBecause != domain.SkipNothingUpstream {
		t.Fatalf("want the indistinguishable skip, got %s / %q", ran.Phase, ran.SkippedBecause)
	}
}
