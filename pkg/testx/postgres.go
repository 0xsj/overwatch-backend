package testx

import (
	"context"
	"os"
	"testing"

	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const DSNVar = "OVERWATCH_TEST_DSN"

// Schema is one domain's storage, named by the caller so this package never
// learns that the domain exists.
type Schema struct {
	Name       string
	Migrations []postgres.Migration

	// Unqualified is for a set that owns no schema of its own — pkg/outbox's
	// tables are shared infrastructure, so its ledger has nowhere to belong.
	Unqualified bool
}

// Postgres returns a pool with every named schema dropped and freshly migrated,
// and registers cleanup. It SKIPS when there is no DSN.
func Postgres(t *testing.T, schemas ...Schema) *postgres.Pool {
	t.Helper()
	dsn := os.Getenv(DSNVar)
	if dsn == "" {
		t.Skipf("%s is unset — run `make test-db`", DSNVar)
	}
	ctx := context.Background()
	pool, err := postgres.Open(ctx, postgres.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("testx: open: %v", err)
	}
	t.Cleanup(pool.Close)

	for _, s := range schemas {
		Reset(t, pool, s)
	}
	return pool
}

// Reset drops a schema and applies its migrations from nothing.
func Reset(t *testing.T, pool *postgres.Pool, s Schema) {
	t.Helper()
	ctx := context.Background()

	if s.Unqualified {
		// The ledger goes with the tables. Dropping the tables alone leaves the
		// migrator believing its own record of what it applied — correctly.
		for _, q := range []string{
			"drop table if exists outbox cascade",
			"drop table if exists schema_migrations cascade",
		} {
			if _, err := pool.DB(ctx).Exec(ctx, q); err != nil {
				t.Fatalf("testx: %s: %v", q, err)
			}
		}
		if _, err := postgres.Migrate(ctx, pool, s.Migrations); err != nil {
			t.Fatalf("testx: migrate %s: %v", s.Name, err)
		}
		return
	}

	if _, err := pool.DB(ctx).Exec(ctx, "drop schema if exists "+s.Name+" cascade"); err != nil {
		t.Fatalf("testx: drop schema %s: %v", s.Name, err)
	}
	if _, err := postgres.Migrate(ctx, pool, s.Migrations, postgres.InSchema(s.Name)); err != nil {
		t.Fatalf("testx: migrate %s: %v", s.Name, err)
	}
}

// Count is the assertion every suite writes by hand, with the query in the
// failure so a red test names what it counted.
func Count(t *testing.T, pool *postgres.Pool, query string) int {
	t.Helper()
	ctx := context.Background()
	var n int
	if err := pool.DB(ctx).QueryRow(ctx, query).Scan(&n); err != nil {
		t.Fatalf("testx: %s: %v", query, err)
	}
	return n
}
