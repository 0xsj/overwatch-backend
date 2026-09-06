package postgres_test

// These need a real server. They SKIP when OVERWATCH_TEST_DSN is unset rather
// than hiding behind a build tag, so `go build ./...` and `make check` still
// compile them — a test that does not compile is a test that rots. `make test-db`
// runs them.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

func open(t *testing.T) *postgres.Pool {
	t.Helper()
	dsn := os.Getenv("OVERWATCH_TEST_DSN")
	if dsn == "" {
		t.Skip("OVERWATCH_TEST_DSN is unset — run `make test-db`")
	}
	// Every test gets its own schema, which is the reason the DSN is a parameter
	// rather than something this package reads for itself.
	schema := fmt.Sprintf("t_%d", time.Now().UnixNano())
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	p, err := postgres.Open(context.Background(), postgres.Config{
		DSN: dsn + sep + "search_path=" + schema,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	if _, err := p.DB(ctx).Exec(ctx, "create schema "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = p.DB(context.Background()).Exec(context.Background(), "drop schema "+schema+" cascade")
		p.Close()
	})
	return p
}

func exec(t *testing.T, p *postgres.Pool, sql string, args ...any) {
	t.Helper()
	ctx := context.Background()
	if _, err := p.DB(ctx).Exec(ctx, sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// ── the mapping, against a real server ─────────────────────────────────

func TestEverySQLStateMapsToTheKindACallerCanActOn(t *testing.T) {
	p := open(t)
	ctx := context.Background()
	exec(t, p, `create table parent (id int primary key, name text not null unique, n int check (n > 0))`)
	exec(t, p, `create table child (id int primary key, parent int references parent(id))`)
	exec(t, p, `insert into parent values (1,'a',1)`)
	exec(t, p, `insert into child values (1,1)`)

	cases := []struct {
		name string
		sql  string
		want errors.Kind
		why  string
	}{
		{"a duplicate primary key", `insert into parent values (1,'z',1)`, errors.Conflict,
			"the row is already there, and the caller can act on that"},
		{"a duplicate unique column", `insert into parent values (2,'a',1)`, errors.Conflict, ""},
		{"a null in a not-null column", `insert into parent values (3,null,1)`, errors.Invalid,
			"the request omitted something required"},
		{"a check constraint refusing a value", `insert into parent values (4,'b',0)`, errors.Unprocessable,
			"well-formed, and refused by a rule — that is 422 and not 400"},
		{"a foreign key naming nothing", `insert into child values (2,999)`, errors.Invalid,
			"the caller named a parent that does not exist"},
		{"a value that will not parse", `select 'abc'::int`, errors.Invalid, ""},
		{"a table that does not exist", `select * from nope`, errors.Internal,
			"our SQL is wrong, and no caller can fix it by sending different input"},
		{"a column that does not exist", `select nope from parent`, errors.Internal, ""},
		{"our own division by zero", `select 1/0`, errors.Internal, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, raw := p.DB(ctx).Exec(ctx, tc.sql)
			if raw == nil {
				t.Fatal("the statement succeeded; this test no longer probes anything")
			}
			err := postgres.Translate(ctx, raw, "probe")
			if got := errors.KindOf(err); got != tc.want {
				t.Fatalf("got %v, want %v — %s", got, tc.want, tc.why)
			}
			if d := errors.DetailsOf(err); d["sqlstate"] == "" {
				t.Error("no sqlstate detail; it is a diagnostic for a log and must survive translation")
			}
		})
	}
}

func TestAConstraintNameSurvivesAsADetailAndNeverAsAMessage(t *testing.T) {
	p := open(t)
	ctx := context.Background()
	exec(t, p, `create table t (id int primary key)`)
	exec(t, p, `insert into t values (1)`)

	_, raw := p.DB(ctx).Exec(ctx, `insert into t values (1)`)
	err := postgres.Translate(ctx, raw, "insert t")

	if !postgres.IsConstraint(err, "t_pkey") {
		t.Errorf("IsConstraint could not see t_pkey; it is how a repository turns a refusal into a domain sentinel")
	}
	if got := errors.DetailsOf(err)["constraint"]; got != "t_pkey" {
		t.Errorf("constraint detail = %q", got)
	}
	if msg := errors.Message(err); strings.Contains(msg, "t_pkey") {
		t.Errorf("the caller-safe message leaked a constraint name: %q", msg)
	}
}

// No database: this asks Translate about a synthetic error and nothing else.
// It took a pool and never used it, which made a pure unit test skip without a
// server while proving nothing about one.
func TestACancelledCallerIsNotRetryable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := postgres.Translate(ctx, context.Canceled, "gone")
	if errors.Retryable(err) || !errors.IsKind(err, errors.Canceled) {
		t.Errorf("cancelled: kind=%v retryable=%v — retrying spends work on a caller who has already gone",
			errors.KindOf(err), errors.Retryable(err))
	}
}

func TestASubMillisecondTimeoutDoesNotBecomeNoTimeout(t *testing.T) {
	dsn := os.Getenv("OVERWATCH_TEST_DSN")
	if dsn == "" {
		t.Skip("OVERWATCH_TEST_DSN is unset — run `make test-db`")
	}
	ctx := context.Background()
	p, err := postgres.Open(ctx, postgres.Config{DSN: dsn, StatementTimeout: 900 * time.Microsecond})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	var got string
	if err := p.DB(ctx).QueryRow(ctx, `show statement_timeout`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got == "0" {
		t.Fatalf("statement_timeout=%q — PostgreSQL reads 0 as DISABLED, so a bound under 1ms truncated to no bound at all: asking for a tighter limit removed the limit", got)
	}
	if _, err := p.DB(ctx).Exec(ctx, `select pg_sleep(1)`); err == nil {
		t.Fatal("a 1s query completed under a sub-millisecond timeout")
	}
}

func TestOpenRefusesAConfigTheServerWouldOnlyComplainAbout(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		cfg  postgres.Config
	}{
		{"a negative statement timeout", postgres.Config{DSN: "postgres://x/y", StatementTimeout: -time.Second}},
		{"a negative connect timeout", postgres.Config{DSN: "postgres://x/y", ConnectTimeout: -time.Second}},
		{"more minimum connections than maximum", postgres.Config{DSN: "postgres://x/y", MinConns: 20, MaxConns: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := postgres.Open(ctx, tc.cfg)
			if !errors.IsKind(err, errors.Invalid) {
				t.Fatalf("got %v; the field that is wrong is nameable here, and the server would only report an empty parameter value", err)
			}
		})
	}
}

func TestAStatementTimeoutIsATimeoutAndIsRetryable(t *testing.T) {
	dsn := os.Getenv("OVERWATCH_TEST_DSN")
	if dsn == "" {
		t.Skip("OVERWATCH_TEST_DSN is unset")
	}
	ctx := context.Background()
	p, err := postgres.Open(ctx, postgres.Config{DSN: dsn, StatementTimeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	_, raw := p.DB(ctx).Exec(ctx, `select pg_sleep(1)`)
	if raw == nil {
		t.Fatal("the sleep completed; statement_timeout was not applied to the connection")
	}
	got := postgres.Translate(ctx, raw, "sleep")
	// The context is NOT done — the server gave up, not the caller. Same
	// SQLSTATE as a cancellation, different meaning, and the difference is
	// whether retrying is worth anything.
	if !errors.IsKind(got, errors.Timeout) || !errors.Retryable(got) {
		t.Fatalf("kind=%v retryable=%v; a server-side timeout is worth retrying and a cancellation is not",
			errors.KindOf(got), errors.Retryable(got))
	}
}

// ── transactions ───────────────────────────────────────────────────────

func TestARollbackUndoesEverythingInTheUnit(t *testing.T) {
	p := open(t)
	ctx := context.Background()
	exec(t, p, `create table t (id int primary key)`)

	want := errors.New(errors.Invalid, "no")
	err := p.InTx(ctx, func(ctx context.Context) error {
		if _, err := p.DB(ctx).Exec(ctx, `insert into t values (1)`); err != nil {
			return err
		}
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("InTx returned %v, want the function's own error unwrapped", err)
	}
	var n int
	if err := p.DB(ctx).QueryRow(ctx, `select count(*) from t`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d rows survived a rollback", n)
	}
}

// AMENDED 2026-09-06, found by custody 0013's mutation round as T01.
//
// The first version made the NESTED call fail. Under a mutant where InTx opens
// a second transaction, that second transaction rolls back too — so the count
// was 0 and the test passed. It constructed the one arrangement in which the
// failure it is named for is invisible, while its own comment described the
// arrangement that would have caught it.
//
// The nested call must SUCCEED and the outer must fail. Then joining rolls the
// nested insert back with everything else, and a separate transaction commits
// it and leaves it behind — which is half a unit of work committed, the thing
// the whole rule exists to prevent.
func TestANestedInTxJoinsRatherThanOpeningASecond(t *testing.T) {
	p := open(t)
	ctx := context.Background()
	exec(t, p, `create table t (id int primary key)`)

	err := p.InTx(ctx, func(ctx context.Context) error {
		if err := p.InTx(ctx, func(ctx context.Context) error {
			_, err := p.DB(ctx).Exec(ctx, `insert into t values (1)`)
			return err
		}); err != nil {
			return err
		}
		// The nested call returned cleanly. If it committed on its own, its row
		// is now durable and this rollback cannot reach it.
		return errors.New(errors.Invalid, "the outer unit fails after the inner one succeeded")
	})
	if err == nil {
		t.Fatal("want the outer failure")
	}

	var n int
	if err := p.DB(ctx).QueryRow(ctx, `select count(*) from t`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d rows survived the outer rollback; the nested call committed independently, which is half a logical unit of work durable and the other half gone", n)
	}
}

// The other direction, and it is a different property: a failure inside a
// nested call must reach the outer caller rather than being absorbed.
func TestANestedFailureReachesTheOuterCaller(t *testing.T) {
	p := open(t)
	ctx := context.Background()
	exec(t, p, `create table t (id int primary key)`)

	want := errors.New(errors.Invalid, "fail inside the nested call")
	err := p.InTx(ctx, func(ctx context.Context) error {
		if _, err := p.DB(ctx).Exec(ctx, `insert into t values (1)`); err != nil {
			return err
		}
		return p.InTx(ctx, func(ctx context.Context) error {
			if _, err := p.DB(ctx).Exec(ctx, `insert into t values (2)`); err != nil {
				return err
			}
			return want
		})
	})
	if !errors.Is(err, want) {
		t.Fatalf("InTx returned %v, want the nested function's own error unwrapped", err)
	}
	var n int
	if err := p.DB(ctx).QueryRow(ctx, `select count(*) from t`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d rows survived", n)
	}
}

func TestAPanicRollsBackAndIsRePanicked(t *testing.T) {
	p := open(t)
	ctx := context.Background()
	exec(t, p, `create table t (id int primary key)`)

	// The panic's own value must come back out. A bare recover() check passes
	// when InTx replaces it with a nil dereference of its own.
	wantPanic(t, "boom", func() {
		_ = p.InTx(ctx, func(ctx context.Context) error {
			_, _ = p.DB(ctx).Exec(ctx, `insert into t values (1)`)
			panic("boom")
		})
	})

	var n int
	if err := p.DB(ctx).QueryRow(ctx, `select count(*) from t`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d rows survived a panic", n)
	}
}

func TestDBOutsideATransactionIsThePool(t *testing.T) {
	p := open(t)
	ctx := context.Background()
	exec(t, p, `create table t (id int primary key)`)
	exec(t, p, `insert into t values (1)`)
	var n int
	if err := p.DB(ctx).QueryRow(ctx, `select count(*) from t`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v; the same repository method must work with no transaction in flight", n, err)
	}
}

// ── migrations ─────────────────────────────────────────────────────────

func TestMigrateIsForwardOnlyAndIdempotent(t *testing.T) {
	p := open(t)
	ctx := context.Background()
	set := []postgres.Migration{
		{Name: "0001_a.sql", SQL: `create table a (id int primary key)`},
		{Name: "0002_b.sql", SQL: `create table b (id int primary key)`},
	}
	if n, err := postgres.Migrate(ctx, p, set); err != nil || n != 2 {
		t.Fatalf("first run applied %d: %v", n, err)
	}
	if n, err := postgres.Migrate(ctx, p, set); err != nil || n != 0 {
		t.Fatalf("second run applied %d, want 0: %v", n, err)
	}
	set = append(set, postgres.Migration{Name: "0003_c.sql", SQL: `create table c (id int primary key)`})
	if n, err := postgres.Migrate(ctx, p, set); err != nil || n != 1 {
		t.Fatalf("third run applied %d, want 1: %v", n, err)
	}
}

func TestAFailedMigrationLeavesTheOnesBeforeItApplied(t *testing.T) {
	p := open(t)
	ctx := context.Background()
	set := []postgres.Migration{
		{Name: "0001_ok.sql", SQL: `create table ok (id int primary key)`},
		{Name: "0002_bad.sql", SQL: `this is not sql`},
		{Name: "0003_never.sql", SQL: `create table never (id int primary key)`},
	}
	n, err := postgres.Migrate(ctx, p, set)
	if err == nil {
		t.Fatal("want a failure on 0002")
	}
	if n != 1 {
		t.Fatalf("reported %d applied, want 1", n)
	}
	if !errors.IsKind(err, errors.Internal) {
		t.Errorf("kind %v; broken SQL in our own migration is ours", errors.KindOf(err))
	}
	var exists bool
	if err := p.DB(ctx).QueryRow(ctx,
		`select exists (select 1 from information_schema.tables where table_name='never')`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("a migration after the failure was applied")
	}
}

func TestTwoProcessesMigratingTogetherDoNotRace(t *testing.T) {
	p := open(t)
	ctx := context.Background()
	set := []postgres.Migration{{Name: "0001_a.sql", SQL: `create table a (id int primary key)`}}

	var wg sync.WaitGroup
	total := make([]int, 4)
	errs := make([]error, 4)
	for i := range total {
		wg.Add(1)
		go func(i int) { defer wg.Done(); total[i], errs[i] = postgres.Migrate(ctx, p, set) }(i)
	}
	wg.Wait()

	applied := 0
	for i := range total {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		applied += total[i]
	}
	if applied != 1 {
		t.Fatalf("%d applications of one migration; the advisory lock did not serialise them", applied)
	}
}

func TestFromFSOrdersByNameAndSkipsEverythingElse(t *testing.T) {
	fsys := fstest.MapFS{
		"m/0010_ten.sql": {Data: []byte("select 10")},
		"m/0002_two.sql": {Data: []byte("select 2")},
		"m/README.md":    {Data: []byte("not a migration")},
		"m/0001_one.sql": {Data: []byte("select 1")},
	}
	got, err := postgres.FromFS(fsys, "m")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"0001_one.sql", "0002_two.sql", "0010_ten.sql"}
	if len(got) != len(want) {
		t.Fatalf("got %d migrations, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i] {
			t.Fatalf("order %v, want %v — zero padding is what makes 10 sort after 9", names(got), want)
		}
	}
}

func names(ms []postgres.Migration) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Name
	}
	return out
}

func TestAnEditedMigrationIsRefusedBeforeAnythingRuns(t *testing.T) {
	p := open(t)
	ctx := context.Background()

	set := []postgres.Migration{
		{Name: "0001_a.sql", SQL: `create table a (id int primary key)`},
		{Name: "0002_b.sql", SQL: `create table b (id int primary key)`},
	}
	if n, err := postgres.Migrate(ctx, p, set); err != nil || n != 2 {
		t.Fatalf("first run applied %d: %v", n, err)
	}

	// Edit an applied one, and add an unapplied one after it. The edit must be
	// caught BEFORE the new migration runs — a partial apply on top of a
	// divergence is worse than a refusal.
	edited := []postgres.Migration{
		set[0],
		{Name: "0002_b.sql", SQL: `create table b (id int primary key, extra text)`},
		{Name: "0003_c.sql", SQL: `create table c (id int primary key)`},
	}
	n, err := postgres.Migrate(ctx, p, edited)
	if err == nil {
		t.Fatal("an edited migration was accepted; a forward-only runner never re-applies it, so this database and a fresh one diverge permanently and silently")
	}
	if n != 0 {
		t.Errorf("applied %d before refusing; the check must precede every apply", n)
	}
	if !errors.IsKind(err, errors.Internal) {
		t.Errorf("kind %v; our own migration files are not caller input", errors.KindOf(err))
	}
	if !strings.Contains(err.Error(), "0002_b.sql") {
		t.Errorf("the error does not name the edited migration: %v", err)
	}

	var exists bool
	if err := p.DB(ctx).QueryRow(ctx,
		`select exists (select 1 from information_schema.tables where table_name='c')`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("0003 was applied despite the refusal")
	}
}

func TestAnUnchangedSetStillPassesAndRecordsItsChecksum(t *testing.T) {
	p := open(t)
	ctx := context.Background()
	set := []postgres.Migration{{Name: "0001_a.sql", SQL: `create table a (id int primary key)`}}

	if _, err := postgres.Migrate(ctx, p, set); err != nil {
		t.Fatal(err)
	}
	if n, err := postgres.Migrate(ctx, p, set); err != nil || n != 0 {
		t.Fatalf("re-running an unchanged set applied %d: %v", n, err)
	}

	var got string
	if err := p.DB(ctx).QueryRow(ctx,
		`select checksum from schema_migrations where name = $1`, set[0].Name).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != set[0].Checksum() {
		t.Errorf("stored checksum %q, want %q", got, set[0].Checksum())
	}
	if len(got) != 64 {
		t.Errorf("checksum is %d characters; a sha256 in hex is 64", len(got))
	}
}

func TestALedgerWrittenBeforeChecksumsExistedStillWorks(t *testing.T) {
	p := open(t)
	ctx := context.Background()

	// The shape the previous version of this runner created.
	exec(t, p, `create table schema_migrations (
		name text primary key, applied_at timestamptz not null default now())`)
	exec(t, p, `insert into schema_migrations (name) values ('0001_a.sql')`)

	set := []postgres.Migration{
		{Name: "0001_a.sql", SQL: `whatever this used to be`},
		{Name: "0002_b.sql", SQL: `create table b (id int primary key)`},
	}
	n, err := postgres.Migrate(ctx, p, set)
	if err != nil {
		t.Fatalf("a ledger with no checksum column broke the runner: %v", err)
	}
	if n != 1 {
		t.Fatalf("applied %d, want 1 — the recorded one must be skipped, and an empty checksum cannot be compared against anything", n)
	}
}

// wantPanic asserts WHICH panic, not merely that one happened. `recover() != nil`
// cannot distinguish a deliberate guard from a nil dereference two statements
// later, so it passes when the guard is deleted — measured on custody 0010
// (M27/M28) and 0014.
func wantPanic(t *testing.T, contains string, call func()) {
	t.Helper()
	defer func() {
		v := recover()
		if v == nil {
			t.Errorf("did not panic; wanted the guard mentioning %q", contains)
			return
		}
		s, ok := v.(string)
		if !ok || !strings.Contains(s, contains) {
			t.Errorf("panicked with %v (%T); wanted the guard mentioning %q", v, v, contains)
		}
	}()
	call()
}

// ── the ledger belongs to whoever owns the set ─────────────────────────
//
// The property under test is not "the option is honoured". It is that two
// domains migrating into the same database keep SEPARATE histories, which is
// what makes extracting one of them a dump of one schema rather than an
// untangling. A shared ledger passes every other test in this file.
func TestTwoSetsInTwoSchemasKeepSeparateLedgers(t *testing.T) {
	p := open(t)
	ctx := context.Background()
	one := fmt.Sprintf("d_one_%d", time.Now().UnixNano())
	two := fmt.Sprintf("d_two_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		c := context.Background()
		_, _ = p.DB(c).Exec(c, "drop schema if exists "+one+" cascade")
		_, _ = p.DB(c).Exec(c, "drop schema if exists "+two+" cascade")
	})

	// Both sets open with 0001_, which is the collision a shared ledger turns
	// into a silently skipped migration.
	setOne := []postgres.Migration{{Name: "0001_init.sql", SQL: "create table " + one + ".a (id int primary key)"}}
	setTwo := []postgres.Migration{{Name: "0001_init.sql", SQL: "create table " + two + ".a (id int primary key)"}}

	if n, err := postgres.Migrate(ctx, p, setOne, postgres.InSchema(one)); err != nil || n != 1 {
		t.Fatalf("first domain applied %d: %v", n, err)
	}
	if n, err := postgres.Migrate(ctx, p, setTwo, postgres.InSchema(two)); err != nil || n != 1 {
		t.Fatalf("second domain applied %d, want 1 — a shared ledger would report 0: %v", n, err)
	}

	for _, schema := range []string{one, two} {
		var count int
		if err := p.DB(ctx).QueryRow(ctx,
			"select count(*) from "+schema+".schema_migrations").Scan(&count); err != nil {
			t.Fatalf("%s ledger: %v", schema, err)
		}
		if count != 1 {
			t.Errorf("%s.schema_migrations has %d rows, want 1", schema, count)
		}
	}
}

// The schema cannot come from the migration file: the ledger is written first,
// so on an empty database the CREATE TABLE would have nowhere to go.
func TestInSchemaCreatesTheSchemaBeforeTheLedger(t *testing.T) {
	p := open(t)
	ctx := context.Background()
	name := fmt.Sprintf("d_fresh_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		c := context.Background()
		_, _ = p.DB(c).Exec(c, "drop schema if exists "+name+" cascade")
	})

	// No `create schema` anywhere in the set.
	set := []postgres.Migration{{Name: "0001_init.sql", SQL: "create table " + name + ".a (id int primary key)"}}
	if n, err := postgres.Migrate(ctx, p, set, postgres.InSchema(name)); err != nil || n != 1 {
		t.Fatalf("applied %d: %v", n, err)
	}
}

// A schema name is interpolated into DDL, where a bind parameter is not
// accepted. The refusal is the whole defence, so it is asserted rather than
// assumed from the fact that every caller today is a constant.
func TestAnUnusableSchemaNameIsRefusedRatherThanQuoted(t *testing.T) {
	p := open(t)
	ctx := context.Background()
	for _, name := range []string{
		"public; drop table x",
		"Identity",
		"1identity",
		"identity-db",
		"",
	} {
		if name == "" {
			continue // the zero value means "no schema", not "a bad one"
		}
		_, err := postgres.Migrate(ctx, p, nil, postgres.InSchema(name))
		if !errors.IsKind(err, errors.Invalid) {
			t.Errorf("InSchema(%q) gave %v, want Invalid", name, err)
		}
	}
}
