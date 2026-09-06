package root

import (
	"context"
	"crypto/rand"
	"os"
	"testing"

	identitycmd "github.com/0xsj/overwatch-backend/internal/identity/app/command"
	identity "github.com/0xsj/overwatch-backend/internal/identity/domain"
	identitypg "github.com/0xsj/overwatch-backend/internal/identity/infra/postgres"
	orgcmd "github.com/0xsj/overwatch-backend/internal/org/app/command"
	org "github.com/0xsj/overwatch-backend/internal/org/domain"
	orgpg "github.com/0xsj/overwatch-backend/internal/org/infra/postgres"
	workspacecmd "github.com/0xsj/overwatch-backend/internal/workspace/app/command"
	workspacepg "github.com/0xsj/overwatch-backend/internal/workspace/infra/postgres"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/crypto"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/outbox"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

var cheap = crypto.Params{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}

type failing struct{ err error }

func (f *failing) Publish(context.Context, ...events.Event) error { return f.err }

func wired(t *testing.T, publisher events.Publisher) (*identitycmd.Registrar, *postgres.Pool) {
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

	for _, s := range []string{"identity", "org", "workspace"} {
		if _, err := p.DB(ctx).Exec(ctx, "drop schema if exists "+s+" cascade"); err != nil {
			t.Fatalf("drop %s: %v", s, err)
		}
	}
	// The ledger goes with the table: a forward-only migrator records what it
	// applied and will not re-apply it, so dropping outbox alone leaves
	// schema_migrations claiming a table that is not there.
	for _, q := range []string{"drop table if exists outbox cascade", "drop table if exists schema_migrations cascade"} {
		if _, err := p.DB(ctx).Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := postgres.Migrate(ctx, p, outbox.Migrations); err != nil {
		t.Fatalf("migrate outbox: %v", err)
	}
	for _, set := range []struct {
		ms     []postgres.Migration
		schema string
	}{
		{identitypg.Migrations, identitypg.Schema},
		{orgpg.Migrations, orgpg.Schema},
		{workspacepg.Migrations, workspacepg.Schema},
	} {
		if _, err := postgres.Migrate(ctx, p, set.ms, postgres.InSchema(set.schema)); err != nil {
			t.Fatalf("migrate %s: %v", set.schema, err)
		}
	}

	clk := clock.System{}
	ids := id.NewV7(clk, rand.Reader)
	if publisher == nil {
		publisher = outbox.NewPublisher(outbox.NewPostgres(p))
	}
	ten := &tenancy{
		orgs:       orgcmd.NewService(orgpg.NewStore(p), publisher, ids, clk),
		workspaces: workspacecmd.NewService(workspacepg.NewStore(p), publisher, ids, clk),
	}
	return identitycmd.NewRegistrar(identitypg.NewStore(p), ten, p, publisher,
		crypto.NewHasher(cheap, rand.Reader), ids, clk), p
}

func TestRegistrationThroughTheRealAdaptersLeavesAllFiveRows(t *testing.T) {
	r, p := wired(t, nil)
	ctx := context.Background()

	got, err := r.Register(ctx, identitycmd.Registration{
		Email:    "sam@example.com",
		Password: secret.New("a passphrase nobody guesses"),
		Name:     "Sam Lee",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	member, err := orgpg.NewStore(p).LiveMemberFor(ctx, got.Tenancy.OrgID, got.Account.ID)
	if err != nil || member.Role != org.RoleOwner {
		t.Fatalf("membership %+v: %v", member, err)
	}
	space, err := workspacepg.NewStore(p).ByID(ctx, got.Tenancy.WorkspaceID)
	if err != nil || space.OrgID != got.Tenancy.OrgID || space.Name != workspacecmd.DefaultName {
		t.Fatalf("workspace %+v: %v", space, err)
	}

	// Three events, one transaction: the account's decision plus the two the
	// provisioner emitted. They reach the outbox together or not at all.
	var n int
	if err := p.DB(ctx).QueryRow(ctx, "select count(*) from outbox").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("the outbox holds %d events, want 3", n)
	}
	var decisions int
	if err := p.DB(ctx).QueryRow(ctx, "select count(*) from outbox where decision").Scan(&decisions); err != nil {
		t.Fatal(err)
	}
	if decisions != 1 {
		t.Errorf("%d events claim to be decisions, want 1 — nobody chose to have an org", decisions)
	}
}

// The whole reason registration is one command. No foreign key crosses a schema
// boundary, so nothing in the database prevents an org with no account. The
// transaction opened in internal/identity/app is that guarantee, and the writes
// it protects happen in two sibling domains it has never heard of.
func TestAFailureInsideTheProvisionerRollsBackIdentityToo(t *testing.T) {
	r, p := wired(t, &failing{err: errors.New(errors.Unavailable, "the outbox is down")})
	ctx := context.Background()

	if _, err := r.Register(ctx, identitycmd.Registration{
		Email: "sam@example.com", Password: secret.New("a passphrase nobody guesses"),
	}); err == nil {
		t.Fatal("registration succeeded with a failing publisher")
	}

	for _, q := range []string{
		"select count(*) from identity.account",
		"select count(*) from identity.credential",
		"select count(*) from org.org",
		"select count(*) from org.member",
		"select count(*) from workspace.workspace",
		"select count(*) from outbox",
	} {
		var n int
		if err := p.DB(ctx).QueryRow(ctx, q).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if n != 0 {
			t.Errorf("%s left %d rows — five schemas did not roll back together", q, n)
		}
	}
}

func TestASecondRegistrationLeavesTheFirstsTenancyIntact(t *testing.T) {
	r, p := wired(t, nil)
	ctx := context.Background()
	in := identitycmd.Registration{Email: "sam@example.com", Password: secret.New("a passphrase nobody guesses")}

	if _, err := r.Register(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Register(ctx, in); !errors.Is(err, identity.ErrAccountExists) {
		t.Fatalf("the second registration gave %v, want ErrAccountExists", err)
	}
	for _, q := range []string{
		"select count(*) from org.org",
		"select count(*) from workspace.workspace",
	} {
		var n int
		if err := p.DB(ctx).QueryRow(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("%s has %d rows, want 1 — the refused attempt left debris", q, n)
		}
	}
}
