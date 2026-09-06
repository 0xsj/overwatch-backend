package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"sort"

	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/env"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/logger"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

func main() {
	if err := run(); err != nil {
		report(err)
		os.Exit(1)
	}
}

func run() error {
	cfg, declared, err := loadConfig(env.OS())
	if err != nil {
		return err
	}

	level, err := logger.ParseLevel(cfg.LogLevel)
	if err != nil {
		return err
	}

	clk := clock.System{}
	ids := id.NewV7(clk, rand.Reader)

	ctx := provenance.NewContext(
		context.Background(),
		provenance.New(provenance.OriginStartup, ids),
	)

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

	log.InfoContext(ctx, "configuration", "config", cfg, "declared", manifest)
	log.WarnContext(ctx, "no server is constructed yet", "port", cfg.ServerPort)
	return nil
}

func report(err error) {
	fmt.Fprintln(os.Stderr, errors.Message(err))
	fields := errors.FieldsOf(err)
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(os.Stderr, "  %s: %s\n", k, fields[k])
	}
}
