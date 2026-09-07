package root

import (
	"context"
	"crypto/rand"
	"testing"

	identitycmd "github.com/0xsj/overwatch-backend/internal/identity/app/command"
	"github.com/0xsj/overwatch-backend/internal/identity/domain"
	identitypg "github.com/0xsj/overwatch-backend/internal/identity/infra/postgres"
	orgcmd "github.com/0xsj/overwatch-backend/internal/org/app/command"
	orgpg "github.com/0xsj/overwatch-backend/internal/org/infra/postgres"
	workspacecmd "github.com/0xsj/overwatch-backend/internal/workspace/app/command"
	workspacepg "github.com/0xsj/overwatch-backend/internal/workspace/infra/postgres"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/crypto"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/logger"
	"github.com/0xsj/overwatch-backend/pkg/outbox"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
	"github.com/0xsj/overwatch-backend/pkg/secret"
	"github.com/0xsj/overwatch-backend/pkg/testx"
)

var cheap = crypto.Params{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}

type chain struct {
	registrar  *identitycmd.Registrar
	dispatcher *outbox.Dispatcher
	pool       *postgres.Pool
}

func wired(t *testing.T) chain {
	t.Helper()
	p := testx.Postgres(t,
		testx.Schema{Name: "outbox", Migrations: outbox.Migrations, Unqualified: true},
		testx.Schema{Name: identitypg.Schema, Migrations: identitypg.Migrations},
		testx.Schema{Name: orgpg.Schema, Migrations: orgpg.Migrations},
		testx.Schema{Name: workspacepg.Schema, Migrations: workspacepg.Migrations},
	)

	clk := clock.System{}
	ids := id.NewV7(clk, rand.Reader)
	publisher := outbox.NewPublisher(outbox.NewPostgres(p))
	orgs := orgcmd.NewSubscriber(orgcmd.NewService(orgpg.NewStore(p), publisher, ids, clk), ids)
	workspaces := workspacecmd.NewSubscriber(
		workspacecmd.NewService(workspacepg.NewStore(p), publisher, ids, clk), ids)

	return chain{
		registrar: identitycmd.NewRegistrar(identitypg.NewStore(p), p, publisher,
			crypto.NewHasher(cheap, rand.Reader), ids, clk),
		dispatcher: outbox.New(outbox.Config{
			Store: outbox.NewPostgres(p), Clock: clk, Log: logger.Nop(),
			Handlers: []events.Handler{orgs.Handle, workspaces.Handle},
		}),
		pool: p,
	}
}

func count(t *testing.T, p *postgres.Pool, q string) int {
	t.Helper()
	var n int
	if err := p.DB(context.Background()).QueryRow(context.Background(), q).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// decisions/0017. Registration writes an account and publishes a fact; org and
// workspace provision themselves from it, each in its own transaction against
// its own schema. Nothing here shares a transaction with anything else, which is
// what makes any link of it extractable into a service.
func TestTheChainProvisionsTenancyFromTheEventAlone(t *testing.T) {
	c := wired(t)
	ctx := context.Background()

	got, err := c.registrar.Register(ctx, identitycmd.Registration{
		Email:    "sam@example.com",
		Password: secret.New("a passphrase nobody guesses"),
		Name:     "Sam Lee",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// Immediately after registering: an account, and no tenancy yet.
	//
	// decisions/0018 moved what makes that window safe. It is no longer that the
	// account cannot authenticate — it can, and signing in immediately is the
	// point — it is that a PENDING account cannot ACT, and the capability gate
	// refuses a caller whose workspace does not resolve. That gate belongs to
	// the first workspace operation built, and 0018 is where the requirement is
	// written down.
	if got.Account.Status != domain.StatusPending {
		t.Fatalf("a freshly registered account is %v, want pending", got.Account.Status)
	}
	if !got.Account.CanAuthenticate() {
		t.Fatal("a pending account cannot sign in — decisions/0018 says it must be able to")
	}
	if n := count(t, c.pool, "select count(*) from org.org"); n != 0 {
		t.Errorf("%d orgs before the chain ran", n)
	}

	if err := c.dispatcher.Drain(ctx); err != nil {
		t.Fatalf("drain: %v", err)
	}

	for _, tc := range []struct {
		what string
		q    string
		want int
	}{
		{"account", "select count(*) from identity.account", 1},
		{"credential", "select count(*) from identity.credential", 1},
		{"org", "select count(*) from org.org", 1},
		{"member", "select count(*) from org.member", 1},
		{"workspace", "select count(*) from workspace.workspace", 1},
	} {
		if n := count(t, c.pool, tc.q); n != tc.want {
			t.Errorf("%s: %d rows, want %d", tc.what, n, tc.want)
		}
	}

	// The org is named from the payload, the workspace from its own default —
	// and the workspace's org is the one the chain made, not one it guessed.
	var orgName, spaceName string
	if err := c.pool.DB(ctx).QueryRow(ctx, `
		select o.name, w.name from org.org o
		join workspace.workspace w on w.org_id = o.id`).Scan(&orgName, &spaceName); err != nil {
		t.Fatalf("join: %v", err)
	}
	if orgName != "Sam Lee" || spaceName != workspacecmd.DefaultName {
		t.Errorf("org %q workspace %q", orgName, spaceName)
	}

	var owner id.ID
	if err := c.pool.DB(ctx).QueryRow(ctx,
		"select account_id from org.member").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner != got.Account.ID {
		t.Errorf("the founding member is %v, not the account that registered", owner)
	}
}

// The chain has to be READABLE as a chain. Every link derives its provenance
// from the event that asked for it, so all three share one correlation id and
// each causation names the event before it. Without that the journal holds three
// unrelated rows and cannot answer the only question it exists for.
func TestTheWholeChainSharesOneCorrelation(t *testing.T) {
	c := wired(t)
	ctx := context.Background()

	// Capture the events rather than let the outbox delete them, so the chain
	// can be read after it has run.
	if _, err := c.pool.DB(ctx).Exec(ctx,
		"create temp table seen on commit preserve rows as select * from outbox with no data"); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if _, err := c.registrar.Register(ctx, identitycmd.Registration{
		Email: "sam@example.com", Password: secret.New("a passphrase nobody guesses"), Name: "Sam",
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := c.pool.DB(ctx).Exec(ctx, "insert into seen select * from outbox"); err != nil {
			t.Fatal(err)
		}
		if _, err := c.dispatcher.Dispatch(ctx); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := c.pool.DB(ctx).Query(ctx,
		"select distinct name, coalesce(provenance->>'correlation_id', provenance->>'request_id') from seen")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]string{}
	for rows.Next() {
		var name, correlation string
		if err := rows.Scan(&name, &correlation); err != nil {
			t.Fatal(err)
		}
		seen[name] = correlation
	}
	if len(seen) != 3 {
		t.Fatalf("saw %d distinct events, want 3: %v", len(seen), seen)
	}
	root := seen["identity.account.created"]
	if root == "" {
		t.Fatal("the account event carries no correlation")
	}
	for name, correlation := range seen {
		if correlation != root {
			t.Errorf("%s has correlation %s, want %s — the chain is three unrelated rows",
				name, correlation, root)
		}
	}
}

// Delivery is at-least-once — decisions/0007 — so the guard has to be a unique
// index rather than a preflight read. Draining twice replays every event.
func TestARedeliveredEventProvisionsNothingTwice(t *testing.T) {
	c := wired(t)
	ctx := context.Background()

	if _, err := c.registrar.Register(ctx, identitycmd.Registration{
		Email: "sam@example.com", Password: secret.New("a passphrase nobody guesses"), Name: "Sam",
	}); err != nil {
		t.Fatal(err)
	}
	// Keep the rows before they are delivered. The outbox deletes on delivery,
	// so this is the only way to reproduce what a dispatcher that crashed after
	// handling and before deleting would cause.
	if _, err := c.pool.DB(ctx).Exec(ctx,
		"create temp table replay on commit preserve rows as select * from outbox"); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if err := c.dispatcher.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.pool.DB(ctx).Exec(ctx, "insert into outbox select * from replay"); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if err := c.dispatcher.Drain(ctx); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{
		"select count(*) from org.org",
		"select count(*) from org.member",
		"select count(*) from workspace.workspace",
	} {
		if n := count(t, c.pool, q); n != 1 {
			t.Errorf("%s: %d rows, want 1", q, n)
		}
	}
}

// A permanently failing link leaves an account that is pending forever. That is
// the failure decisions/0017 trades for, and it is visible rather than silent.
func TestAFailedLinkLeavesTheAccountPendingAndTheEventBuried(t *testing.T) {
	c := wired(t)
	ctx := context.Background()

	if _, err := c.registrar.Register(ctx, identitycmd.Registration{
		Email: "sam@example.com", Password: secret.New("a passphrase nobody guesses"),
	}); err != nil {
		t.Fatal(err)
	}

	broken := outbox.New(outbox.Config{
		Store: outbox.NewPostgres(c.pool), Clock: clock.System{}, MaxAttempts: 1, Log: logger.Nop(),
		Handlers: []events.Handler{func(context.Context, events.Event) error {
			return errors.New(errors.Unavailable, "org is down")
		}},
	})
	_ = broken.Drain(ctx)

	if n := count(t, c.pool, "select count(*) from identity.account"); n != 1 {
		t.Errorf("the account did not survive a failed chain")
	}
	if n := count(t, c.pool, "select count(*) from org.org"); n != 0 {
		t.Errorf("%d orgs from a failing handler", n)
	}
	var status string
	if err := c.pool.DB(ctx).QueryRow(ctx, "select status from identity.account").Scan(&status); err != nil {
		t.Fatal(err)
	}
	// Pending because nothing activated it, NOT because tenancy is missing —
	// decisions/0018 separated those. Such an account can sign in and is
	// refused at the workspace gate; what must not happen is a half-provisioned
	// account that looks finished.
	if status != "pending" {
		t.Errorf("status %q — a failed chain left the account looking complete", status)
	}
	if n := count(t, c.pool, "select count(*) from outbox where buried_at is not null"); n == 0 {
		t.Error("nothing was buried, so the failure is invisible")
	}
}
