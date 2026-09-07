package root

import (
	"context"
	"crypto/rand"
	"os"
	"time"

	auditapp "github.com/0xsj/overwatch-backend/internal/audit/app"
	auditpg "github.com/0xsj/overwatch-backend/internal/audit/infra/postgres"
	identitycmd "github.com/0xsj/overwatch-backend/internal/identity/app/command"
	identityquery "github.com/0xsj/overwatch-backend/internal/identity/app/query"
	identitypg "github.com/0xsj/overwatch-backend/internal/identity/infra/postgres"
	identityhttp "github.com/0xsj/overwatch-backend/internal/identity/transport/http"
	journalapp "github.com/0xsj/overwatch-backend/internal/journal/app"
	journalpg "github.com/0xsj/overwatch-backend/internal/journal/infra/postgres"
	orgcmd "github.com/0xsj/overwatch-backend/internal/org/app/command"
	orgpg "github.com/0xsj/overwatch-backend/internal/org/infra/postgres"
	workspacecmd "github.com/0xsj/overwatch-backend/internal/workspace/app/command"
	workspacepg "github.com/0xsj/overwatch-backend/internal/workspace/infra/postgres"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/crypto"
	"github.com/0xsj/overwatch-backend/pkg/env"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/logger"
	"github.com/0xsj/overwatch-backend/pkg/outbox"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
	"github.com/0xsj/overwatch-backend/pkg/provenance"

	"log/slog"
)

// app is everything this binary owns, constructed once. It is the dependency
// graph, written out — no container, no reflection, no code generation. When a
// second binary shares two thirds of this, that is the moment to reach for
// google/wire; a runtime container never, because resolving by reflection moves
// wiring errors from compile time to boot and gives up the one property
// RUNNING.md leans on: if it started, everything it needs was present.
type app struct {
	cfg   Config
	log   *slog.Logger
	clk   clock.System
	ids   *id.V7
	db    *postgres.Pool
	boot  context.Context
	close func()

	identity   *identityhttp.API
	whoami     httpx.Identifier
	dispatcher *outbox.Dispatcher
}

// boot constructs in dependency order and fails at the first thing missing.
// Everything expensive or fallible happens here, before a listener exists, so a
// bad DSN kills the process at start rather than on the first request that
// happens to need a row.
func Boot(ctx context.Context) (*app, error) {
	cfg, declared, err := loadConfig(env.OS())
	if err != nil {
		return nil, err
	}

	level, err := logger.ParseLevel(cfg.LogLevel)
	if err != nil {
		return nil, err
	}

	clk := clock.System{}
	ids := id.NewV7(clk, rand.Reader)

	// The process's own chain. Everything logged before a request arrives hangs
	// off this, so a boot line and a request line are distinguishable by origin
	// rather than by guessing from the message.
	bootCtx := provenance.NewContext(ctx, provenance.New(provenance.OriginStartup, ids))

	log := logger.New(logger.Config{
		Level:   level,
		Format:  logger.FormatConsole,
		Output:  os.Stdout,
		Clock:   clk,
		Context: provenance.Attrs,
	})

	manifest := make([]string, 0, len(declared))
	for _, v := range declared {
		manifest = append(manifest, v.String())
	}
	log.InfoContext(bootCtx, "configuration", "config", cfg, "declared", manifest)

	db, err := postgres.Open(bootCtx, postgres.Config{
		DSN:              cfg.DatabaseURL.Reveal(),
		ConnectTimeout:   5 * time.Second,
		StatementTimeout: 30 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	// Ask the server what it is, rather than reporting what we asked for. A
	// boot line saying "connected" proves the pool was constructed; this proves
	// a statement ran, and names the thing on the other end.
	var version, database, user string
	if err := db.DB(bootCtx).QueryRow(bootCtx,
		`select current_setting('server_version'), current_database(), current_user`,
	).Scan(&version, &database, &user); err != nil {
		return nil, err
	}
	log.InfoContext(bootCtx, "database ready",
		"postgres", version, "database", database, "user", user,
		"statement_timeout", "30s", "max_conns", 10)

	// Migrations, in the order a fresh database needs them. Each domain owns its
	// own schema and its own ledger, so the only thing this ordering asserts is
	// that the shared outbox exists before anything publishes into it.
	for _, set := range []struct {
		name string
		ms   []postgres.Migration
		opts []postgres.MigrateOption
	}{
		{"outbox", outbox.Migrations, nil},
		{"identity", identitypg.Migrations, []postgres.MigrateOption{postgres.InSchema(identitypg.Schema)}},
		{"org", orgpg.Migrations, []postgres.MigrateOption{postgres.InSchema(orgpg.Schema)}},
		{"workspace", workspacepg.Migrations, []postgres.MigrateOption{postgres.InSchema(workspacepg.Schema)}},
		{"audit", auditpg.Migrations, []postgres.MigrateOption{postgres.InSchema(auditpg.Schema)}},
		{"journal", journalpg.Migrations, []postgres.MigrateOption{postgres.InSchema(journalpg.Schema)}},
	} {
		n, err := postgres.Migrate(bootCtx, db, set.ms, set.opts...)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			log.InfoContext(bootCtx, "migrated", "schema", set.name, "applied", n)
		}
	}

	// The dependency graph, written out. No container and no reflection: if this
	// compiles, every dependency was present, and a missing one is a build error
	// rather than a nil pointer on the first request that needs it.
	store := outbox.NewPostgres(db)
	publisher := outbox.NewPublisher(store)

	accounts := identitypg.NewStore(db)
	hasher := crypto.NewHasher(crypto.Default, rand.Reader)
	registrar := identitycmd.NewRegistrar(accounts, db, publisher, hasher, ids, clk)
	authenticator := identitycmd.NewAuthenticator(
		accounts, publisher, hasher, crypto.NewMinter(rand.Reader), ids, clk, 0)
	sessions := identityquery.NewSessions(accounts, clk)

	// The registration chain — decisions/0017. Each link runs in its own
	// transaction against its own schema, so any of the three can become a
	// separate service by replacing one handler here with a NATS publisher.
	orgs := orgcmd.NewSubscriber(orgcmd.NewService(orgpg.NewStore(db), publisher, ids, clk), ids)
	workspaces := workspacecmd.NewSubscriber(
		workspacecmd.NewService(workspacepg.NewStore(db), publisher, ids, clk), ids)

	// The poll interval is a backstop, not the delivery mechanism. A publish
	// notifies inside its own transaction, so a committed event wakes the
	// dispatcher in milliseconds instead of waiting out the ticker. If the
	// listening connection is lost the wakes stop and the ticker carries on —
	// slower, never wrong, and logged.
	wake, err := db.Listen(bootCtx, outbox.NotifyChannel, log)
	if err != nil {
		return nil, err
	}

	dispatcher := outbox.New(outbox.Config{
		Store: store,
		Wake:  wake,
		Clock: clk,
		Log:   log,
		Handlers: []events.Handler{
			// The chain first: a link that fails is retried with the event, and
			// the observers are idempotent, so they see it again harmlessly.
			orgs.Handle,
			workspaces.Handle,
			auditapp.NewSubscriber(auditpg.NewStore(db), ids, clk).Handle,
			journalapp.NewSubscriber(journalpg.NewStore(db), ids, clk).Handle,
		},
	})

	return &app{
		cfg: cfg, log: log, clk: clk, ids: ids, db: db, boot: bootCtx,
		identity: identityhttp.NewAPI(registrar, authenticator, log),
		// The moment this is non-nil, every record in the system starts naming
		// a person instead of `anonymous`.
		whoami:     identityhttp.Identifier(sessions),
		dispatcher: dispatcher,
		close:      func() { db.Close() },
	}, nil
}
