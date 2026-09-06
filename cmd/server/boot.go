package main

import (
	"context"
	"crypto/rand"
	"os"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/env"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/logger"
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
}

// boot constructs in dependency order and fails at the first thing missing.
// Everything expensive or fallible happens here, before a listener exists, so a
// bad DSN kills the process at start rather than on the first request that
// happens to need a row.
func boot(ctx context.Context) (*app, error) {
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

	return &app{
		cfg: cfg, log: log, clk: clk, ids: ids, db: db, boot: bootCtx,
		close: func() { db.Close() },
	}, nil
}
