package root

import (
	"context"
	"crypto/rand"
	"os"
	"time"

	auditapp "github.com/0xsj/overwatch-backend/internal/audit/app"
	auditquery "github.com/0xsj/overwatch-backend/internal/audit/app/query"
	auditpg "github.com/0xsj/overwatch-backend/internal/audit/infra/postgres"
	identitycmd "github.com/0xsj/overwatch-backend/internal/identity/app/command"
	identityquery "github.com/0xsj/overwatch-backend/internal/identity/app/query"
	identitypg "github.com/0xsj/overwatch-backend/internal/identity/infra/postgres"
	identityhttp "github.com/0xsj/overwatch-backend/internal/identity/transport/http"
	journalapp "github.com/0xsj/overwatch-backend/internal/journal/app"
	journalquery "github.com/0xsj/overwatch-backend/internal/journal/app/query"
	journalpg "github.com/0xsj/overwatch-backend/internal/journal/infra/postgres"
	orgcmd "github.com/0xsj/overwatch-backend/internal/org/app/command"
	orgquery "github.com/0xsj/overwatch-backend/internal/org/app/query"
	orgpg "github.com/0xsj/overwatch-backend/internal/org/infra/postgres"
	workspacecmd "github.com/0xsj/overwatch-backend/internal/workspace/app/command"
	workspacequery "github.com/0xsj/overwatch-backend/internal/workspace/app/query"
	workspacepg "github.com/0xsj/overwatch-backend/internal/workspace/infra/postgres"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/crypto"
	"github.com/0xsj/overwatch-backend/pkg/env"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/limit"
	"github.com/0xsj/overwatch-backend/pkg/logger"
	"github.com/0xsj/overwatch-backend/pkg/mail"
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

	sweeper    *journalapp.Sweeper
	identity   *identityhttp.API
	whoami     httpx.Identifier
	dispatcher *outbox.Dispatcher

	// The composed read. See me.go for why it lives at the root and nowhere
	// else.
	me *me
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

	// Mail is constructed BEFORE anything that sends one, and fails here. A
	// mailer that cannot be built is a registration that silently never
	// delivers a link, which looks to the person registering like nothing
	// happened at all.
	mailer, err := mail.New(mail.Config{
		Addr:    cfg.MailAddr,
		From:    cfg.MailFrom,
		BaseURL: cfg.BaseURL,
		Timeout: 10 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	log.InfoContext(bootCtx, "mail ready",
		"addr", cfg.MailAddr, "from", cfg.MailFrom, "links_point_at", cfg.BaseURL)

	accounts := identitypg.NewStore(db)
	hasher := crypto.NewHasher(crypto.Default, rand.Reader)
	registrar := identitycmd.NewRegistrar(accounts, db, publisher, hasher, ids, clk)
	authenticator := identitycmd.NewAuthenticator(
		accounts, publisher, hasher, crypto.NewMinter(rand.Reader), ids, clk, 0)
	sessions := identityquery.NewSessions(accounts, clk)
	verifier := identitycmd.NewVerifier(
		accounts, mailer, publisher, hasher, crypto.NewMinter(rand.Reader), ids, clk)
	settings := identitycmd.NewSettings(
		accounts, mailer, publisher, hasher, crypto.NewMinter(rand.Reader), ids, clk)

	// Five an hour, per address, per endpoint. The endpoints that send mail are
	// unauthenticated by necessity, so without this they are a way to post mail
	// from this domain to anywhere, at whatever rate a script manages.
	mailLimit := limit.New(limit.Config{
		Rule:  limit.Rule{Burst: 5, Every: 12 * time.Minute},
		Clock: clk,
	})

	// The registration chain — decisions/0017. Each link runs in its own
	// transaction against its own schema, so any of the three can become a
	// separate service by replacing one handler here with a NATS publisher.
	orgStore := orgpg.NewStore(db)
	orgs := orgcmd.NewSubscriber(orgcmd.NewService(orgStore, publisher, ids, clk), ids)
	workspaceService := workspacecmd.NewService(workspacepg.NewStore(db), publisher, ids, clk)
	workspaces := workspacecmd.NewSubscriber(workspaceService, ids)

	// The fourth link — decisions/0020. It reacts to workspace.opened and gives
	// whoever opened an engagement `admin` on it. Registration does not trigger
	// it: the chain emits workspace.created, which is work, and never
	// workspace.opened, which is a choice somebody made.
	granter := orgcmd.NewGranter(orgStore, ids, clk)

	orgAccess := orgquery.NewAccess(orgStore)
	people := identityquery.NewDirectory(accounts)
	members := orgcmd.NewMembers(orgStore, publisher, db, ids, clk)
	invites := orgcmd.NewInvites(orgStore, directory{people: people}, orgAccess,
		mailer, crypto.NewMinter(rand.Reader), publisher, db, ids, clk)
	grants := orgcmd.NewGrants(orgStore, orgAccess, publisher, ids, clk)

	// The sweep — decisions/0022. Constructed HERE and started in run.go, so a
	// retention below the floor kills the process at boot rather than at the
	// first tick, by which point it would have deleted everything.
	sweeper, err := journalapp.NewSweeper(journalapp.SweeperConfig{
		Store:     journalpg.NewStore(db),
		Clock:     clk,
		Log:       log,
		Retention: time.Duration(cfg.JournalRetentionDays) * 24 * time.Hour,
	})
	if err != nil {
		return nil, err
	}

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
			granter.Handle,
			// Identity listens for its OWN account.created and sends the first
			// verification link. Registration therefore never learns that a
			// mail server exists, and an SMTP outage cannot fail a
			// registration that already succeeded.
			identitycmd.NewSubscriber(verifier, ids).Handle,
			// Ends every membership an archived account held — decisions/0028.
			// A membership it cannot end fails the handler and buries the event,
			// which is the alarm for the race that record names.
			orgcmd.NewDepartures(orgStore, orgStore, ids, clk).Handle,
			auditapp.NewSubscriber(auditpg.NewStore(db), ids, clk).Handle,
			journalapp.NewSubscriber(journalpg.NewStore(db), ids, clk).Handle,
		},
	})

	return &app{
		cfg: cfg, log: log, clk: clk, ids: ids, db: db, boot: bootCtx,
		identity: identityhttp.NewAPI(registrar, authenticator, verifier,
			settings, sessions, mailLimit, log),
		// The moment this is non-nil, every record in the system starts naming
		// a person instead of `anonymous`.
		whoami:     identityhttp.Identifier(sessions),
		dispatcher: dispatcher,
		sweeper:    sweeper,
		me: newMe(sessions, settings,
			people,
			orgquery.NewOrgs(orgpg.NewStore(db)),
			orgAccess,
			workspacequery.NewWorkspaces(workspacepg.NewStore(db)),
			workspaceService, grants, invites, members,
			auditquery.NewLedger(auditpg.NewStore(db)),
			journalquery.NewTrail(journalpg.NewStore(db)), log),
		close: func() { db.Close() },
	}, nil
}
