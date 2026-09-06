package postgres

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xsj/overwatch-backend/pkg/errors"
)

// DBTX is what a query runs on. Both *pgxpool.Pool and pgx.Tx satisfy it, which
// is what lets one repository method work inside a transaction and outside one.
// It is also the shape sqlc generates against.
type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Config struct {
	// DSN is a parameter and never an environment read. A test appends its own
	// search_path to this string to get an isolated schema.
	DSN string

	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
	ConnectTimeout  time.Duration

	// StatementTimeout bounds every statement on every connection. Without it a
	// single pathological query holds a pool slot until somebody notices, which
	// is the failure that looks like the whole service being down.
	StatementTimeout time.Duration
}

func (c Config) withDefaults() Config {
	if c.MaxConns == 0 {
		c.MaxConns = 10
	}
	if c.MaxConnLifetime == 0 {
		c.MaxConnLifetime = time.Hour
	}
	if c.MaxConnIdleTime == 0 {
		c.MaxConnIdleTime = 30 * time.Minute
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = 5 * time.Second
	}
	if c.StatementTimeout == 0 {
		c.StatementTimeout = 30 * time.Second
	}
	return c
}

func (c Config) validate() error {
	// A negative duration would render as a negative parameter and be refused by
	// the server with a message naming an empty string. Refuse it here, where
	// the field that is wrong can be named.
	if c.StatementTimeout < 0 {
		return errors.New(errors.Invalid, "postgres: negative StatementTimeout")
	}
	if c.ConnectTimeout < 0 {
		return errors.New(errors.Invalid, "postgres: negative ConnectTimeout")
	}
	if c.MinConns > c.MaxConns {
		return errors.Newf(errors.Invalid,
			"postgres: MinConns %d exceeds MaxConns %d", c.MinConns, c.MaxConns)
	}
	return nil
}

type Pool struct {
	pool *pgxpool.Pool
}

// Open parses the DSN, applies the timeouts, connects, and verifies the
// connection before returning. A pool that has never talked to the server is a
// pool whose first failure arrives on the first request instead of at boot.
func Open(ctx context.Context, cfg Config) (*Pool, error) {
	cfg = cfg.withDefaults()
	if cfg.DSN == "" {
		return nil, errors.New(errors.Invalid, "postgres: empty DSN")
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	pc, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, errors.Wrap(err, errors.Invalid, "postgres: unreadable DSN")
	}
	pc.MaxConns = cfg.MaxConns
	pc.MinConns = cfg.MinConns
	pc.MaxConnLifetime = cfg.MaxConnLifetime
	pc.MaxConnIdleTime = cfg.MaxConnIdleTime
	pc.ConnConfig.ConnectTimeout = cfg.ConnectTimeout

	// Set on the connection rather than per statement, so it covers everything
	// including queries this package never sees.
	//
	// PostgreSQL takes whole milliseconds and reads 0 as DISABLED, so integer
	// division fails OPEN: a StatementTimeout under 1ms truncates to 0 and
	// removes the bound the caller was tightening. Measured — 900µs produced
	// statement_timeout="0" and a 1.2s query returned with no error. Round up,
	// so the smallest bound anyone can ask for is the smallest the server has.
	ms := (cfg.StatementTimeout + time.Millisecond - 1) / time.Millisecond
	pc.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(int64(ms), 10)

	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, errors.Wrap(err, errors.Unavailable, "postgres: connect")
	}

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, Translate(pingCtx, err, "postgres: ping")
	}
	return &Pool{pool: pool}, nil
}

func (p *Pool) Close() { p.pool.Close() }

func (p *Pool) Ping(ctx context.Context) error {
	return Translate(ctx, p.pool.Ping(ctx), "postgres: ping")
}

// Raw is the escape hatch for the things a DBTX cannot express — CopyFrom, a
// dedicated connection, pool statistics. Naming it Raw is deliberate: a caller
// reaching for it is leaving the boundary this package draws.
func (p *Pool) Raw() *pgxpool.Pool { return p.pool }
