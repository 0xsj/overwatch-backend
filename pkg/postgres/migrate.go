package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/0xsj/overwatch-backend/pkg/errors"
)

// advisoryLock is an arbitrary constant, shared by every process running these
// migrations. Two servers starting together take it in turn instead of racing.
const advisoryLock int64 = 8_270_413_559_001

type Migration struct {
	Name string
	SQL  string
}

// Checksum is the identity of the CONTENT, where Name is the identity of the
// step. Byte-exact and deliberately not whitespace-normalised: a reformatted
// migration is a different migration, because the only question this answers is
// "are these the bytes that ran".
func (m Migration) Checksum() string {
	sum := sha256.Sum256([]byte(m.SQL))
	return hex.EncodeToString(sum[:])
}

// FromFS reads *.sql from dir and orders them by name. Names sort, so they must
// sort correctly — a numeric prefix, zero-padded. `10_x.sql` before `9_x.sql` is
// a bug that only appears once there are ten migrations.
func FromFS(fsys fs.FS, dir string) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, errors.Wrap(err, errors.Internal, "postgres: read migrations")
	}
	var out []Migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, errors.Wrap(err, errors.Internal, "postgres: read "+e.Name())
		}
		out = append(out, Migration{Name: e.Name(), SQL: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Migrate applies every migration not yet recorded, in order, each in its own
// transaction. Forward-only: there is no down.
//
// Each in its OWN transaction is deliberate. One transaction around all of them
// would be atomic and would also mean a failure in the tenth leaves you with a
// schema that matches no recorded state. Per-migration, a failure leaves
// everything before it applied and everything after it not, which is exactly
// what the ledger says and is recoverable by fixing the file.
// defaultLedger is where an applied migration is recorded when a caller names
// no schema. pkg/outbox uses it: its tables are shared infrastructure rather
// than a domain's, so its ledger has no schema to belong to.
const defaultLedger = "schema_migrations"

// schemaName is what InSchema will accept. A schema is an identifier
// interpolated into DDL, where a bind parameter is not accepted, so it is
// checked rather than quoted. Quoting would make `"weird-name"` work and put
// the quoting question at every call site instead.
var schemaName = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// reservedSchema is the shape check's blind spot, found on 2026-09-07 by a
// domain called `check`: the name matched the pattern and `create schema check`
// is still a syntax error, because `check` is the SQL constraint keyword.
//
// **The refusal is here rather than a pair of quotes in the DDL** because
// quoting only moves the problem. A schema that needs quotes needs them in every
// hand-written query file too, forever, and that is a tax paid by a person who
// fails at deploy rather than at compile. Refusing the name costs one word.
//
// It is not the full reserved list — it is the words a DOMAIN in a product like
// this plausibly gets called. A name that is reserved and absent here still
// fails, just later and less legibly.
var reservedSchema = map[string]bool{
	"check": true, "user": true, "order": true, "group": true, "table": true,
	"grant": true, "session": true, "default": true, "all": true, "case": true,
	"references": true, "collate": true, "column": true, "constraint": true,
	"limit": true, "offset": true, "select": true, "where": true, "current_user": true,
}

type MigrateOption func(*migrateConfig)

type migrateConfig struct {
	schema string
	ledger string
}

// InSchema puts the tables and their ledger in one schema, so a domain's
// migration history leaves with the domain. The schema is created by [Migrate]
// rather than by the first migration, because the ledger is written before any
// migration runs.
func InSchema(schema string) MigrateOption {
	return func(c *migrateConfig) {
		c.schema = schema
		c.ledger = schema + "." + defaultLedger
	}
}

func newMigrateConfig(opts []MigrateOption) (migrateConfig, error) {
	c := migrateConfig{ledger: defaultLedger}
	for _, o := range opts {
		o(&c)
	}
	if reservedSchema[c.schema] {
		return c, errors.Newf(errors.Invalid,
			"postgres: %q is a reserved SQL word and cannot be a schema name", c.schema)
	}
	if c.schema != "" && !schemaName.MatchString(c.schema) {
		return c, errors.Newf(errors.Invalid,
			"postgres: %q is not a usable schema name; it is interpolated into DDL, "+
				"where a bind parameter is not accepted, so it must match [a-z_][a-z0-9_]{0,62}",
			c.schema)
	}
	return c, nil
}

func Migrate(ctx context.Context, p *Pool, ms []Migration, opts ...MigrateOption) (int, error) {
	cfg, err := newMigrateConfig(opts)
	if err != nil {
		return 0, err
	}

	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return 0, Translate(ctx, err, "postgres: acquire for migrate")
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `select pg_advisory_lock($1)`, advisoryLock); err != nil {
		return 0, Translate(ctx, err, "postgres: migration lock")
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `select pg_advisory_unlock($1)`, advisoryLock)
	}()

	if cfg.schema != "" {
		if _, err := conn.Exec(ctx, `create schema if not exists `+cfg.schema); err != nil {
			return 0, Translate(ctx, err, "postgres: create schema "+cfg.schema)
		}
	}

	if _, err := conn.Exec(ctx, `
		create table if not exists `+cfg.ledger+` (
			name       text        primary key,
			checksum   text        not null default '',
			applied_at timestamptz not null default now()
		)`); err != nil {
		return 0, Translate(ctx, err, "postgres: create "+cfg.ledger)
	}
	// A ledger written by an earlier version of this runner has no checksum
	// column. Adding it here rather than shipping a migration for the migration
	// table keeps the runner able to bootstrap itself from nothing.
	if _, err := conn.Exec(ctx,
		`alter table `+cfg.ledger+` add column if not exists checksum text not null default ''`,
	); err != nil {
		return 0, Translate(ctx, err, "postgres: add "+cfg.ledger+".checksum")
	}

	rows, err := conn.Query(ctx, `select name, checksum from `+cfg.ledger)
	if err != nil {
		return 0, Translate(ctx, err, "postgres: read "+cfg.ledger)
	}
	applied := map[string]string{}
	for rows.Next() {
		var n, sum string
		if err := rows.Scan(&n, &sum); err != nil {
			rows.Close()
			return 0, Translate(ctx, err, "postgres: scan "+cfg.ledger)
		}
		applied[n] = sum
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, Translate(ctx, err, "postgres: read "+cfg.ledger)
	}

	// Check every recorded migration in this set BEFORE applying any of them. A
	// forward-only runner never re-runs an applied step, so editing one changes
	// what new databases get and changes nothing about existing ones — the two
	// diverge permanently and silently, and the divergence is invisible in the
	// diff that caused it. Refusing to run at all is the only signal available.
	//
	// A name recorded but absent from this set is NOT an error: a caller may
	// legitimately pass a subset, and every test here does.
	var drifted []string
	for _, m := range ms {
		sum, ok := applied[m.Name]
		if !ok || sum == "" {
			continue // never applied, or applied before checksums existed
		}
		if sum != m.Checksum() {
			drifted = append(drifted, m.Name)
		}
	}
	if len(drifted) > 0 {
		return 0, errors.Newf(errors.Internal,
			"postgres: %d applied migration(s) have been edited since they ran: %s — "+
				"a forward-only runner will not re-apply them, so this database and a fresh one "+
				"would diverge permanently. Revert the edit, or add a new migration",
			len(drifted), strings.Join(drifted, ", "))
	}

	n := 0
	for _, m := range ms {
		if _, ok := applied[m.Name]; ok {
			continue
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return n, Translate(ctx, err, "postgres: begin migration")
		}
		if _, err := tx.Exec(ctx, m.SQL); err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			return n, Translate(ctx, err, "postgres: apply "+m.Name)
		}
		if _, err := tx.Exec(ctx,
			`insert into `+cfg.ledger+` (name, checksum) values ($1, $2)`,
			m.Name, m.Checksum()); err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			return n, Translate(ctx, err, "postgres: record "+m.Name)
		}
		if err := tx.Commit(ctx); err != nil {
			return n, Translate(ctx, err, "postgres: commit "+m.Name)
		}
		n++
	}
	return n, nil
}
