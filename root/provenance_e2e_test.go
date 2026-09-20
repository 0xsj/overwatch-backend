package root

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cleanuppg "github.com/0xsj/overwatch-backend/internal/artifactcleanup/infra/postgres"
	assistpg "github.com/0xsj/overwatch-backend/internal/assistance/infra/postgres"
	auditapp "github.com/0xsj/overwatch-backend/internal/audit/app"
	auditquery "github.com/0xsj/overwatch-backend/internal/audit/app/query"
	auditpg "github.com/0xsj/overwatch-backend/internal/audit/infra/postgres"
	briefpg "github.com/0xsj/overwatch-backend/internal/brief/infra/postgres"
	checkcmd "github.com/0xsj/overwatch-backend/internal/check/app/command"
	checkquery "github.com/0xsj/overwatch-backend/internal/check/app/query"
	checkpg "github.com/0xsj/overwatch-backend/internal/check/infra/postgres"
	entcmd "github.com/0xsj/overwatch-backend/internal/entity/app/command"
	entquery "github.com/0xsj/overwatch-backend/internal/entity/app/query"
	entpg "github.com/0xsj/overwatch-backend/internal/entity/infra/postgres"
	eventpg "github.com/0xsj/overwatch-backend/internal/event/infra/postgres"
	findingcmd "github.com/0xsj/overwatch-backend/internal/finding/app/command"
	findingquery "github.com/0xsj/overwatch-backend/internal/finding/app/query"
	findingpg "github.com/0xsj/overwatch-backend/internal/finding/infra/postgres"
	healthquery "github.com/0xsj/overwatch-backend/internal/health/app/query"
	identitycmd "github.com/0xsj/overwatch-backend/internal/identity/app/command"
	identityquery "github.com/0xsj/overwatch-backend/internal/identity/app/query"
	identitypg "github.com/0xsj/overwatch-backend/internal/identity/infra/postgres"
	identityhttp "github.com/0xsj/overwatch-backend/internal/identity/transport/http"
	journalapp "github.com/0xsj/overwatch-backend/internal/journal/app"
	journalquery "github.com/0xsj/overwatch-backend/internal/journal/app/query"
	journalpg "github.com/0xsj/overwatch-backend/internal/journal/infra/postgres"
	leadpg "github.com/0xsj/overwatch-backend/internal/lead/infra/postgres"
	notecmd "github.com/0xsj/overwatch-backend/internal/note/app/command"
	notequery "github.com/0xsj/overwatch-backend/internal/note/app/query"
	notepg "github.com/0xsj/overwatch-backend/internal/note/infra/postgres"
	obscmd "github.com/0xsj/overwatch-backend/internal/observation/app/command"
	obsquery "github.com/0xsj/overwatch-backend/internal/observation/app/query"
	obspg "github.com/0xsj/overwatch-backend/internal/observation/infra/postgres"
	orgcmd "github.com/0xsj/overwatch-backend/internal/org/app/command"
	orgquery "github.com/0xsj/overwatch-backend/internal/org/app/query"
	orgpg "github.com/0xsj/overwatch-backend/internal/org/infra/postgres"
	reportcmd "github.com/0xsj/overwatch-backend/internal/report/app/command"
	reportquery "github.com/0xsj/overwatch-backend/internal/report/app/query"
	reportpg "github.com/0xsj/overwatch-backend/internal/report/infra/postgres"
	reviewpg "github.com/0xsj/overwatch-backend/internal/review/infra/postgres"
	runcmd "github.com/0xsj/overwatch-backend/internal/run/app/command"
	runquery "github.com/0xsj/overwatch-backend/internal/run/app/query"
	runpg "github.com/0xsj/overwatch-backend/internal/run/infra/postgres"
	scopecmd "github.com/0xsj/overwatch-backend/internal/scope/app/command"
	scopequery "github.com/0xsj/overwatch-backend/internal/scope/app/query"
	scopepg "github.com/0xsj/overwatch-backend/internal/scope/infra/postgres"
	seencmd "github.com/0xsj/overwatch-backend/internal/seen/app/command"
	seenquery "github.com/0xsj/overwatch-backend/internal/seen/app/query"
	seenpg "github.com/0xsj/overwatch-backend/internal/seen/infra/postgres"
	sourcedomain "github.com/0xsj/overwatch-backend/internal/source/domain"
	extractioncmd "github.com/0xsj/overwatch-backend/internal/source/extraction/app/command"
	extractionpg "github.com/0xsj/overwatch-backend/internal/source/extraction/infra/postgres"
	sourcepg "github.com/0xsj/overwatch-backend/internal/source/infra/postgres"
	targetcmd "github.com/0xsj/overwatch-backend/internal/target/app/command"
	targetquery "github.com/0xsj/overwatch-backend/internal/target/app/query"
	targetpg "github.com/0xsj/overwatch-backend/internal/target/infra/postgres"
	toolcmd "github.com/0xsj/overwatch-backend/internal/tool/app/command"
	toolquery "github.com/0xsj/overwatch-backend/internal/tool/app/query"
	toolpg "github.com/0xsj/overwatch-backend/internal/tool/infra/postgres"
	workspacecmd "github.com/0xsj/overwatch-backend/internal/workspace/app/command"
	workspacequery "github.com/0xsj/overwatch-backend/internal/workspace/app/query"
	workspacepg "github.com/0xsj/overwatch-backend/internal/workspace/infra/postgres"
	"github.com/0xsj/overwatch-backend/pkg/blob"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/crypto"
	"github.com/0xsj/overwatch-backend/pkg/egress"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/execx"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/limit"
	"github.com/0xsj/overwatch-backend/pkg/logger"
	"github.com/0xsj/overwatch-backend/pkg/mail"
	"github.com/0xsj/overwatch-backend/pkg/outbox"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
	"github.com/0xsj/overwatch-backend/pkg/testx"
)

// traced is the whole system minus the listener: the real middleware, the real
// handler, the real chain, the real journal. Nothing here is a fake — the point
// is to assert that what pkg/provenance promises survives an HTTP request, a
// transaction, an outbox row, a dispatcher and four subscribers.
type traced struct {
	handler http.Handler
	pump    *outbox.Dispatcher
	pool    *postgres.Pool
	// executor is driven by hand, one [runcmd.Executor.Tick] at a time. A
	// background loop would make every assertion a race.
	executor *runcmd.Executor
	// sent is the mailbox. A memory Sender is the ONE fake here, because the
	// alternative is a test that needs an SMTP server to assert what a link
	// says.
	sent *mail.Memory
}

// guessBudget lets ONE test give the sign-in limiter a real budget. Every other
// caller gets nil, which permits everything — these suites sign in far more than
// ten times, and a rate limit firing mid-suite would fail a test about something
// else entirely.
var guessBudget *limit.Limiter

func tracedSystem(t *testing.T) traced {
	return tracedSystemWithFetcher(t, egress.New(egress.Config{
		Guard:   egress.NewGuard(egress.Policy{AllowLoopback: true}),
		MaxBody: sourcedomain.MaxBinaryCaptureBytes,
	}))
}

func tracedSystemWithFetcher(t *testing.T, fetcher referenceFetcher) traced {
	return tracedSystemWithFetcherAndOCR(t, fetcher, extractioncmd.UnsupportedImageOCR{})
}

func tracedSystemWithOCR(t *testing.T, ocr extractioncmd.ImageOCR) traced {
	return tracedSystemWithFetcherAndOCR(t, egress.New(egress.Config{
		Guard:   egress.NewGuard(egress.Policy{AllowLoopback: true}),
		MaxBody: sourcedomain.MaxBinaryCaptureBytes,
	}), ocr)
}

func tracedSystemWithFetcherAndOCR(t *testing.T, fetcher referenceFetcher, ocr extractioncmd.ImageOCR) traced {
	t.Helper()
	// Reset between tests: a package-level knob that leaks into the next test is
	// the reason most of them are refused, and this one is justified only
	// because it is cleaned up here.
	t.Cleanup(func() { guessBudget = nil })
	p := testx.Postgres(t,
		testx.Schema{Name: "outbox", Migrations: outbox.Migrations, Unqualified: true},
		testx.Schema{Name: identitypg.Schema, Migrations: identitypg.Migrations},
		testx.Schema{Name: orgpg.Schema, Migrations: orgpg.Migrations},
		testx.Schema{Name: workspacepg.Schema, Migrations: workspacepg.Migrations},
		testx.Schema{Name: seenpg.Schema, Migrations: seenpg.Migrations},
		testx.Schema{Name: journalpg.Schema, Migrations: journalpg.Migrations},
		testx.Schema{Name: auditpg.Schema, Migrations: auditpg.Migrations},
		testx.Schema{Name: targetpg.Schema, Migrations: targetpg.Migrations},
		testx.Schema{Name: scopepg.Schema, Migrations: scopepg.Migrations},
		testx.Schema{Name: toolpg.Schema, Migrations: toolpg.Migrations},
		testx.Schema{Name: checkpg.Schema, Migrations: checkpg.Migrations},
		testx.Schema{Name: runpg.Schema, Migrations: runpg.Migrations},
		testx.Schema{Name: sourcepg.Schema, Migrations: sourcepg.Migrations},
		testx.Schema{Name: extractionpg.Schema, Migrations: extractionpg.Migrations},
		testx.Schema{Name: obspg.Schema, Migrations: obspg.Migrations},
		testx.Schema{Name: eventpg.Schema, Migrations: eventpg.Migrations},
		testx.Schema{Name: entpg.Schema, Migrations: entpg.Migrations},
		testx.Schema{Name: findingpg.Schema, Migrations: findingpg.Migrations},
		testx.Schema{Name: reportpg.Schema, Migrations: reportpg.Migrations},
		testx.Schema{Name: notepg.Schema, Migrations: notepg.Migrations},
		testx.Schema{Name: reviewpg.Schema, Migrations: reviewpg.Migrations},
		testx.Schema{Name: leadpg.Schema, Migrations: leadpg.Migrations},
		testx.Schema{Name: briefpg.Schema, Migrations: briefpg.Migrations},
		testx.Schema{Name: assistpg.Schema, Migrations: assistpg.Migrations},
		testx.Schema{Name: cleanuppg.Schema, Migrations: cleanuppg.Migrations},
	)

	clk := clock.System{}
	ids := id.NewV7(clk, rand.Reader)
	publisher := outbox.NewPublisher(outbox.NewPostgres(p))

	accounts := identitypg.NewStore(p)
	hasher := crypto.NewHasher(cheap, rand.Reader)
	auth := identitycmd.NewAuthenticator(accounts, publisher, hasher,
		crypto.NewMinter(rand.Reader), ids, clk, 0)
	sessions := identityquery.NewSessions(accounts, clk)

	orgStore := orgpg.NewStore(p)
	workspaceService := workspacecmd.NewService(workspacepg.NewStore(p), publisher, ids, clk)

	sent := mail.NewMemory()
	mailer, err := mail.Wrap(sent, "Overwatch <no-reply@overwatch.test>", "http://localhost:7010")
	if err != nil {
		t.Fatal(err)
	}
	verifier := identitycmd.NewVerifier(accounts, mailer, publisher, hasher,
		crypto.NewMinter(rand.Reader), ids, clk)

	settings := identitycmd.NewSettings(accounts, mailer, publisher, hasher,
		crypto.NewMinter(rand.Reader), ids, clk)
	toolReads := toolquery.NewTools(toolpg.NewStore(p))
	checkReads := checkquery.NewChecks(checkpg.NewStore(p))
	workspaceReads := workspacequery.NewWorkspaces(workspacepg.NewStore(p))
	kit := toolbox{tools: toolReads}

	// A real blob store under the test's own temp directory. t.TempDir is
	// removed with the test, so artifacts do not leak between runs.
	bytes, err := blob.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runReads := runquery.NewRuns(runpg.NewStore(p), blobs{store: bytes})

	// One observation reader for the graph's two ports, built once rather than
	// three times inline.
	obsReadsForGraph := obsquery.NewObservations(obspg.NewStore(p),
		mappingStep{tools: toolReads},
		runSteps{runs: runReads},
		ruleStep{rules: scopequery.NewRules(scopepg.NewStore(p))},
		orgOf{reads: workspaceReads})

	// THE EXECUTOR. It was absent from this harness until 2026-09-08, which is
	// exactly how long the extraction half went unexercised: every run test
	// asserted what was PLANNED, and nothing had ever spawned a process, stored
	// an artifact, read it under a mapping and let the graph subscriber see the
	// result. `Tick` has said "it is exported so a test can drive exactly one
	// pass" since it was written, and no test called it.
	//
	// One instance of `Runs`, shared with the HTTP handler below. Two would each
	// hold their own — harmless today and the sort of thing that stops being
	// harmless the moment either grows a cache.
	runsCmd := runcmd.NewRuns(runpg.NewStore(p),
		chains{checks: checkReads, tools: toolReads, workspaces: workspaceReads},
		targets{targets: targetquery.NewTargets(targetpg.NewStore(p))},
		spawns{rules: scopequery.NewRules(scopepg.NewStore(p))},
		p, publisher, ids, clk)
	extractor := obscmd.NewExtractor(obspg.NewStore(p),
		liveMappings{tools: toolReads},
		sightings{findings: findingcmd.NewFindings(findingpg.NewStore(p),
			findingFragments{fragments: entpg.NewStore(p)}, p, publisher, ids, clk)},
		publisher, ids, clk)
	executor := runcmd.NewExecutor(runpg.NewStore(p), runsCmd, kit,
		orgOf{reads: workspaceReads}, execxSpawner{}, blobs{store: bytes},
		extracts{extractor: extractor},
		observedSubjects{observed: obsReadsForGraph}, p, publisher, ids, clk,
		execx.Policy{Timeout: 30 * time.Second, MaxOutput: 4 << 20},
		4, time.Second, logger.Nop())

	mux := http.NewServeMux()
	newMe(sessions, settings,
		identityquery.NewDirectory(accounts),
		orgquery.NewOrgs(orgStore),
		orgquery.NewAccess(orgStore, clk),
		workspaceReads,
		workspaceService,
		orgcmd.NewGrants(orgStore, orgquery.NewAccess(orgStore, clk), publisher, ids, clk),
		orgcmd.NewInvites(orgStore, directory{people: identityquery.NewDirectory(accounts)},
			orgquery.NewAccess(orgStore, clk), mailer, crypto.NewMinter(rand.Reader),
			publisher, p, ids, clk),
		orgcmd.NewMembers(orgStore, publisher, p, ids, clk),
		targetquery.NewTargets(targetpg.NewStore(p)),
		targetcmd.NewTargets(targetpg.NewStore(p), publisher, ids, clk),
		scopequery.NewRules(scopepg.NewStore(p)),
		scopecmd.NewRules(scopepg.NewStore(p), publisher, ids, clk),
		toolReads,
		toolcmd.NewTools(toolpg.NewStore(p), publisher, ids, clk),
		toolcmd.NewMappings(toolpg.NewStore(p), p, publisher, ids, clk),
		checkReads,
		checkcmd.NewChecks(checkpg.NewStore(p), kit, p, publisher, ids, clk),
		runReads,
		// The REAL adapters, not stubs. They are the only thing that proves the
		// five ports `run` borrows are wired to what they claim.
		runsCmd,
		obsquery.NewObservations(obspg.NewStore(p),
			mappingStep{tools: toolReads},
			runSteps{runs: runReads},
			ruleStep{rules: scopequery.NewRules(scopepg.NewStore(p))},
			orgOf{reads: workspaceReads}),
		entquery.NewGraph(entpg.NewStore(p),
			spawnPermits{rules: scopequery.NewRules(scopepg.NewStore(p))},
			coverageChecks{checks: checkReads, workspaces: workspaceReads},
			coverageChecked{runs: runReads, observed: obsquery.NewObservations(obspg.NewStore(p), mappingStep{tools: toolReads}, runSteps{runs: runReads}, ruleStep{rules: scopequery.NewRules(scopepg.NewStore(p))}, orgOf{reads: workspaceReads})}),
		entcmd.NewRulings(entpg.NewStore(p), publisher, ids, clk),
		findingquery.NewFindings(findingpg.NewStore(p)),
		findingcmd.NewFindings(findingpg.NewStore(p),
			findingFragments{fragments: entpg.NewStore(p)},
			p, publisher, ids, clk),
		reportquery.NewReports(reportpg.NewStore(p), blobs{store: bytes}),
		reportcmd.NewReports(reportpg.NewStore(p),
			sections{
				rules: scopequery.NewRules(scopepg.NewStore(p)),
				graph: entquery.NewGraph(entpg.NewStore(p),
					spawnPermits{rules: scopequery.NewRules(scopepg.NewStore(p))},
					coverageChecks{checks: checkReads, workspaces: workspaceReads},
					coverageChecked{runs: runReads, observed: obsquery.NewObservations(
						obspg.NewStore(p), mappingStep{tools: toolReads},
						runSteps{runs: runReads},
						ruleStep{rules: scopequery.NewRules(scopepg.NewStore(p))},
						orgOf{reads: workspaceReads})}),
				findings:        findingquery.NewFindings(findingpg.NewStore(p)),
				runs:            runReads,
				clock:           clk,
				engagementNotes: engagementNotes{notes: notequery.NewNotes(notepg.NewStore(p))},
			},
			blobs{store: bytes}, p, publisher, ids, clk),
		healthquery.NewDoctor(probes{
			runs: runpg.NewStore(p), tools: toolpg.NewStore(p),
			checks: checkpg.NewStore(p), observed: obspg.NewStore(p),
			events: outbox.NewPostgres(p), workspaces: workspaceReads,
		}, clk),
		notequery.NewNotes(notepg.NewStore(p)),
		notecmd.NewNotes(notepg.NewStore(p), knownKinds{}, publisher, ids, clk),
		newResearchWithFetcherAndOCR(p, bytes, publisher, ids, clk, fetcher, ocr),
		auditquery.NewLedger(auditpg.NewStore(p)),
		journalquery.NewTrail(journalpg.NewStore(p)),
		journalquery.NewLog(journalpg.NewStore(p)),
		seenquery.NewMarkers(seenpg.NewStore(p)),
		seencmd.NewMarkers(seenpg.NewStore(p), clk),
		logger.Nop()).register(mux)
	identityhttp.NewAPI(
		identitycmd.NewRegistrar(accounts, p, publisher, hasher, ids, clk),
		// BOTH LIMITERS NIL. A nil limiter permits everything, which is what
		// these tests need — they sign in far more than ten times, and a
		// rate limit firing mid-suite would fail a test about something else.
		// `identity`'s own tests are where the budget is exercised.
		// The mail limiter is nil — permits everything. The GUESS limiter is
		// whatever the test asked for, and nil for all but one.
		auth, verifier, settings, sessions, nil, guessBudget, logger.Nop()).Routes(mux)

	return traced{
		// The real middleware with the real Identifier. A registration carries
		// no token, so its actor is anonymous — but a signed-in request's is
		// not, which is what TestASignedInRequestNamesThePerson asserts.
		handler: httpx.Chain(mux, httpx.WithProvenance(ids, identityhttp.Identifier(sessions))),
		pump: outbox.New(outbox.Config{
			Store: outbox.NewPostgres(p), Clock: clk, Log: logger.Nop(),
			Handlers: []events.Handler{
				orgcmd.NewSubscriber(orgcmd.NewService(orgStore, publisher, ids, clk), ids).Handle,
				workspacecmd.NewSubscriber(workspaceService, ids).Handle,
				orgcmd.NewGranter(orgStore, ids, clk).Handle,
				orgcmd.NewDepartures(orgStore, orgStore, ids, clk).Handle,
				identitycmd.NewSubscriber(verifier, ids).Handle,
				// THE GRAPH — decisions/0036's two subscribers. They were
				// missing from this harness until 0044 needed a root entity:
				// both were built on 2026-09-08 and neither had ever run
				// end-to-end, so "a target gets a root entity" was a unit test
				// and an assumption.
				entcmd.NewSubscriber(entcmd.NewAssembler(entpg.NewStore(p),
					subjects{observed: obsReadsForGraph},
					provenances{observed: obsReadsForGraph, runs: runReads,
						tools: toolReads, spaces: workspaceReads},
					targetOfRun{runs: runReads},
					claims{rules: scopequery.NewRules(scopepg.NewStore(p))},
					publisher, ids, clk), ids).Handle,
				targetcmd.NewRootSubscriber(targetpg.NewStore(p)).Handle,
				auditapp.NewSubscriber(auditpg.NewStore(p), ids, clk).Handle,
				journalapp.NewSubscriber(journalpg.NewStore(p), ids, clk).Handle,
			},
		}),
		pool:     p,
		sent:     sent,
		executor: executor,
	}
}

// work drives the pipeline to a standstill: spawn, extract, then deliver the
// events that extraction published, then spawn again for anything those
// deliveries planned.
//
// **The two have to alternate.** The graph is assembled by a subscriber on
// `extract.observation.created`, so a tick with no drain after it leaves the
// fragments unwritten and a drain with no tick after it leaves a scheduled run
// unspawned — and a test that did one of them would assert on a half-built
// world and pass for the wrong reason.
func (s traced) work(t *testing.T, rounds int) {
	t.Helper()
	ctx := context.Background()
	for range rounds {
		if err := s.executor.Tick(ctx); err != nil {
			t.Fatalf("executor tick: %v", err)
		}
		if err := s.pump.Drain(ctx); err != nil {
			t.Fatalf("drain: %v", err)
		}
	}
}

func (s traced) register(t *testing.T, email string, headers map[string]string) *http.Response {
	t.Helper()
	body := `{"email":"` + email + `","password":"a passphrase nobody guesses","name":"Sam Lee"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/accounts", strings.NewReader(body))
	req.Header.Set("content-type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec.Result()
}

// drain runs the pump until the outbox is empty — the chain is three hops, and
// each hop's event is published by the hop before it.
func (s traced) post(t *testing.T, path, body string, headers map[string]string) *http.Response {
	t.Helper()
	return s.do(t, http.MethodPost, path, body, headers)
}

func (s traced) delete(t *testing.T, path string, headers map[string]string) *http.Response {
	t.Helper()
	return s.do(t, http.MethodDelete, path, "", headers)
}

func (s traced) do(t *testing.T, method, path, body string, headers map[string]string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("content-type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec.Result()
}

func decode(t *testing.T, res *http.Response, into any) {
	t.Helper()
	if err := json.NewDecoder(res.Body).Decode(into); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func (s traced) drain(t *testing.T) {
	t.Helper()
	if err := s.pump.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}
}

type line struct {
	Action      string
	EventID     string
	Correlation string
	Causation   string
	Depth       int
	Attempt     int
	Actor       string
	Origin      string
	Workspace   string
}

func (s traced) lines(t *testing.T) []line {
	t.Helper()
	ctx := context.Background()
	rows, err := s.pool.DB(ctx).Query(ctx, `
		select action, event_id::text, coalesce(correlation_id::text,''),
		       coalesce(causation_id::text,''), depth, attempt, actor, origin, workspace_id
		from journal.line order by occurred_at`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []line
	for rows.Next() {
		var l line
		if err := rows.Scan(&l.Action, &l.EventID, &l.Correlation, &l.Causation,
			&l.Depth, &l.Attempt, &l.Actor, &l.Origin, &l.Workspace); err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	return out
}

// ── the ledger, asserted end to end ────────────────────────────────────

func TestProvenanceSurvivesTheWholeChain(t *testing.T) {
	s := tracedSystem(t)

	res := s.register(t, "sam@example.com", nil)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d", res.StatusCode)
	}
	// The edge stamps both onto the response, which is what makes a chain
	// findable from a client's logs without access to the database.
	requestID := res.Header.Get(httpx.HeaderRequestID)
	correlation := res.Header.Get(httpx.HeaderCorrelationID)
	if requestID == "" || correlation == "" {
		t.Fatalf("headers: request=%q correlation=%q", requestID, correlation)
	}
	// At an ORIGIN the correlation IS the root's request id. No second
	// identifier is minted, so there is nothing to drift.
	if requestID != correlation {
		t.Errorf("at an origin correlation should equal request: %s vs %s", correlation, requestID)
	}

	s.drain(t)
	lines := s.lines(t)
	if len(lines) != 3 {
		t.Fatalf("journal holds %d lines, want 3", len(lines))
	}

	byAction := map[string]line{}
	for _, l := range lines {
		byAction[l.Action] = l
		if l.Correlation != correlation {
			t.Errorf("%s carries correlation %s, want the request's %s — the chain is broken at that hop",
				l.Action, l.Correlation, correlation)
		}
		if l.Attempt != 1 {
			t.Errorf("%s is attempt %d on a first delivery", l.Action, l.Attempt)
		}
		if l.Actor != "anonymous" {
			t.Errorf("%s names actor %q — a self-signup is unauthenticated", l.Action, l.Actor)
		}
		if l.Origin != "request" {
			t.Errorf("%s has origin %q; a subscriber inherits WHY the chain exists", l.Action, l.Origin)
		}
	}

	account := byAction["identity.account.created"]
	org := byAction["org.created"]
	workspace := byAction["workspace.created"]

	// Causation is the EDGE: each hop names the event that caused it, not the
	// request that emitted it. Correlation alone would not distinguish this
	// chain from three siblings.
	if account.Causation != "" {
		t.Errorf("the root has causation %s; nothing caused it", account.Causation)
	}
	if org.Causation != account.EventID {
		t.Errorf("org.created is caused by %s, want the account event %s", org.Causation, account.EventID)
	}
	if workspace.Causation != org.EventID {
		t.Errorf("workspace.created is caused by %s, want the org event %s", workspace.Causation, org.EventID)
	}

	for _, tc := range []struct {
		l    line
		want int
	}{{account, 0}, {org, 1}, {workspace, 2}} {
		if tc.l.Depth != tc.want {
			t.Errorf("%s is at depth %d, want %d", tc.l.Action, tc.l.Depth, tc.want)
		}
	}

	// Tenant is whose data it touches, and it cannot be known before the
	// workspace exists. It appears exactly where it becomes true.
	if account.Workspace != "" || org.Workspace != "" {
		t.Errorf("a tenant appeared before the workspace did: account=%q org=%q",
			account.Workspace, org.Workspace)
	}
	if workspace.Workspace == "" {
		t.Error("workspace.created carries no tenant, so audit cannot scope it")
	}
}

// Every unit of work gets its OWN request id. Reusing the parent's would make a
// fan-out indistinguishable from a retry, which is the pair Attempt exists to
// keep apart.
func TestEachHopMintsItsOwnRequestID(t *testing.T) {
	s := tracedSystem(t)
	ctx := context.Background()

	if _, err := s.pool.DB(ctx).Exec(ctx,
		"create temp table seen on commit preserve rows as select * from outbox with no data"); err != nil {
		t.Fatal(err)
	}
	if res := s.register(t, "sam@example.com", nil); res.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d", res.StatusCode)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.pool.DB(ctx).Exec(ctx, "insert into seen select * from outbox"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.pump.Dispatch(ctx); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := s.pool.DB(ctx).Query(ctx, "select distinct name, provenance from seen")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	requests := map[string]string{}
	for rows.Next() {
		var name string
		var raw []byte
		if err := rows.Scan(&name, &raw); err != nil {
			t.Fatal(err)
		}
		var p struct {
			Request string `json:"request_id"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		if p.Request == "" {
			t.Fatalf("%s carries no request id", name)
		}
		for other, seen := range requests {
			if seen == p.Request {
				t.Errorf("%s and %s share request %s — a hop reused its parent's", name, other, seen)
			}
		}
		requests[name] = p.Request
	}
	if len(requests) != 3 {
		t.Fatalf("saw %d events, want 3", len(requests))
	}
}

// The edge adopts an upstream correlation, and the whole chain inherits it. This
// is what makes a trace that starts in the client readable in the journal — and
// the depth-0 value is then NOT a root, because it opened this process's work
// rather than the chain.
func TestAnUpstreamCorrelationIsAdoptedAndCarriedToTheLastHop(t *testing.T) {
	s := tracedSystem(t)
	upstream := id.NewSequence(clock.System{}.Now()).NewID().String()

	res := s.register(t, "sam@example.com", map[string]string{
		httpx.HeaderCorrelationID: upstream,
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d", res.StatusCode)
	}
	if got := res.Header.Get(httpx.HeaderCorrelationID); got != upstream {
		t.Fatalf("the edge did not adopt: %s, want %s", got, upstream)
	}
	if res.Header.Get(httpx.HeaderRequestID) == upstream {
		t.Error("request was adopted too — request is minted here and never taken from a caller")
	}

	s.drain(t)
	for _, l := range s.lines(t) {
		if l.Correlation != upstream {
			t.Errorf("%s carries %s, want the upstream %s", l.Action, l.Correlation, upstream)
		}
	}
}

// Depth and attempt are the two fields something branches on, so neither is
// adoptable — a caller cannot reset a bound it is subject to. Adopted has no
// field for either, which is the compiler enforcing it; this asserts the edge
// does not smuggle them in some other way.
func TestDepthAndAttemptAreNotTakenFromTheCaller(t *testing.T) {
	s := tracedSystem(t)

	res := s.register(t, "sam@example.com", map[string]string{
		httpx.HeaderCorrelationID: id.NewSequence(clock.System{}.Now()).NewID().String(),
		"X-Depth":                 "31",
		"X-Attempt":               "9",
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d", res.StatusCode)
	}
	s.drain(t)
	for _, l := range s.lines(t) {
		if l.Attempt != 1 {
			t.Errorf("%s is attempt %d — a caller set it", l.Action, l.Attempt)
		}
	}
	lines := s.lines(t)
	if lines[0].Depth != 0 {
		t.Errorf("the root is at depth %d — a caller set it", lines[0].Depth)
	}
}

// The payoff, and the first time the record layer does what the product claims.
// Until an Identifier was wired, every audit entry and journal line in the
// system said `anonymous` — including for things a person plainly did.
func TestASignedInRequestNamesThePersonInTheRecord(t *testing.T) {
	s := tracedSystem(t)

	res := s.register(t, "sam@example.com", nil)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d", res.StatusCode)
	}
	var registered struct {
		AccountID string `json:"account_id"`
	}
	decode(t, res, &registered)

	// Signing in carries no token, so this one is anonymous too.
	signIn := s.post(t, "/v1/sessions",
		`{"email":"sam@example.com","password":"a passphrase nobody guesses"}`, nil)
	if signIn.StatusCode != http.StatusCreated {
		t.Fatalf("sign in: %d", signIn.StatusCode)
	}
	var session struct {
		Token  string `json:"token"`
		Status string `json:"status"`
	}
	decode(t, signIn, &session)
	if session.Token == "" {
		t.Fatal("no token")
	}
	// decisions/0018: a pending account signs in.
	if session.Status != "pending" {
		t.Errorf("status %q", session.Status)
	}

	s.drain(t)
	byAction := map[string]line{}
	for _, l := range s.lines(t) {
		byAction[l.Action] = l
	}

	started, ok := byAction["identity.session.started"]
	if !ok {
		t.Fatal("signing in produced no journal line")
	}
	want := "user:" + registered.AccountID
	if started.Actor != want {
		t.Errorf("actor is %q, want %q — the Identifier is not resolving the token", started.Actor, want)
	}
	// And a request made WITH the token names the person too.
	out := s.delete(t, "/v1/sessions/current", map[string]string{
		"Authorization": "Bearer " + session.Token,
	})
	if out.StatusCode != http.StatusNoContent {
		t.Fatalf("sign out: %d", out.StatusCode)
	}
	s.drain(t)
	for _, l := range s.lines(t) {
		if l.Action != "identity.session.ended" {
			continue
		}
		if l.Actor != want {
			t.Errorf("sign-out actor is %q, want %q", l.Actor, want)
		}
		return
	}
	t.Error("signing out produced no journal line")
}

// A bad token must not fail the request at the middleware. Provenance is
// metadata, never the thing that authorises — so it yields the anonymous actor
// and the handler refuses properly.
func TestABadTokenIsAnonymousRatherThanRejectedByTheMiddleware(t *testing.T) {
	s := tracedSystem(t)
	res := s.post(t, "/v1/sessions",
		`{"email":"nobody@example.com","password":"a passphrase nobody guesses"}`,
		map[string]string{"Authorization": "Bearer not-a-real-token"})

	// The handler's answer, not the middleware's: rejected credentials, not a
	// complaint about the header.
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("status %d, want 401 from the handler", res.StatusCode)
	}
	if res.Header.Get(httpx.HeaderRequestID) == "" {
		t.Error("the chain was not installed for a request with a bad token")
	}
}
