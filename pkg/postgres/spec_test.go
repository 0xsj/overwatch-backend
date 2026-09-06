// Package postgres_test tests github.com/0xsj/overwatch-backend/pkg/postgres
// against its package doc alone. Tests that need a real server skip when
// OVERWATCH_TEST_DSN is unset; tests over pure functions (Translate,
// IsConstraint, Checksum, FromFS, and the parts of Open that reject bad
// input before ever dialing) run unconditionally and are the most valuable
// tests in this file, because they can disagree with the implementation.
package postgres_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	errs "github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

// ─────────────────────────────────────────────────────────────────────────
// helpers (all prefixed spec per the mechanical constraints)
// ─────────────────────────────────────────────────────────────────────────

// specTestDSN returns the server DSN or skips: every database-dependent test
// funnels through this so the skip behavior is uniform and impossible to
// forget.
func specTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("OVERWATCH_TEST_DSN")
	if dsn == "" {
		t.Skip("OVERWATCH_TEST_DSN not set; this test needs a real PostgreSQL server")
	}
	return dsn
}

var specSchemaCounter int64

// specUniqueSchemaName builds a schema name unlikely to collide with the
// other test files and sibling packages exercising the same server
// concurrently.
func specUniqueSchemaName(t *testing.T) string {
	t.Helper()
	n := atomic.AddInt64(&specSchemaCounter, 1)
	raw := strings.ToLower(t.Name())
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	name := b.String()
	if len(name) > 24 {
		name = name[:24]
	}
	return fmt.Sprintf("spec_%s_%d_%d", name, time.Now().UnixNano(), n)
}

// specIsolatedDSN creates a fresh schema on the real server and returns a
// DSN whose search_path points at it, dropping the schema in cleanup. It
// skips if OVERWATCH_TEST_DSN is unset.
func specIsolatedDSN(t *testing.T) string {
	t.Helper()
	base := specTestDSN(t)
	ctx := context.Background()
	boot, err := postgres.Open(ctx, postgres.Config{
		DSN:            base,
		MaxConns:       2,
		ConnectTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("bootstrap Open against OVERWATCH_TEST_DSN: %v", err)
	}
	schema := specUniqueSchemaName(t)
	if _, err := boot.DB(ctx).Exec(ctx, "create schema "+schema); err != nil {
		boot.Close()
		t.Fatalf("create schema %s: %v", schema, err)
	}
	t.Cleanup(func() {
		dctx := context.Background()
		if _, err := boot.DB(dctx).Exec(dctx, "drop schema if exists "+schema+" cascade"); err != nil {
			t.Logf("cleanup: drop schema %s: %v", schema, err)
		}
		boot.Close()
	})
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + "search_path=" + schema
}

// specOpenPool opens a Pool against dsn and closes it in cleanup.
func specOpenPool(t *testing.T, dsn string) *postgres.Pool {
	t.Helper()
	p, err := postgres.Open(context.Background(), postgres.Config{
		DSN:              dsn,
		MaxConns:         4,
		ConnectTimeout:   10 * time.Second,
		StatementTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Open against an isolated schema on a live server must succeed: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

func specTableExists(t *testing.T, pool *postgres.Pool, name string) bool {
	t.Helper()
	ctx := context.Background()
	var exists bool
	err := pool.DB(ctx).QueryRow(ctx,
		`select exists (select 1 from information_schema.tables where table_name = $1 and table_schema = current_schema())`,
		name,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("checking whether table %q exists: %v", name, err)
	}
	return exists
}

func specCount(t *testing.T, pool *postgres.Pool, table string) int {
	t.Helper()
	ctx := context.Background()
	var n int
	if err := pool.DB(ctx).QueryRow(ctx, "select count(*) from "+table).Scan(&n); err != nil {
		t.Fatalf("counting rows in %s: %v", table, err)
	}
	return n
}

// ═══════════════════════════════════════════════════════════════════════
// PROPERTY / PURE TESTS — no server required, must not skip
// ═══════════════════════════════════════════════════════════════════════

func TestSpecMigrationChecksumIsDeterministic(t *testing.T) {
	m := postgres.Migration{Name: "0001_x.sql", SQL: "create table t (id int);\n"}
	a, b := m.Checksum(), m.Checksum()
	if a != b {
		t.Fatal("Checksum must be a pure function of its bytes: calling it twice on the same Migration must yield the same value")
	}
}

func TestSpecMigrationChecksumIsByteExactNotWhitespaceNormalized(t *testing.T) {
	original := postgres.Migration{Name: "x", SQL: "create table t (id int);\n"}
	identical := postgres.Migration{Name: "x", SQL: "create table t (id int);\n"}
	reformatted := postgres.Migration{Name: "x", SQL: "create table t (id int); \n"} // one trailing space added

	if original.Checksum() != identical.Checksum() {
		t.Fatal("identical bytes must produce identical checksums")
	}
	if original.Checksum() == reformatted.Checksum() {
		t.Fatal("Checksum is documented as deliberately not whitespace-normalized: a reformatted migration is a different migration, because the only question it answers is whether these are the bytes that ran")
	}
}

func TestSpecMigrationChecksumIdentityIsContentNotName(t *testing.T) {
	sql := "create table shared (id int primary key)"
	a := postgres.Migration{Name: "0001_a.sql", SQL: sql}
	b := postgres.Migration{Name: "9999_totally_different_name.sql", SQL: sql}
	if a.Checksum() != b.Checksum() {
		t.Fatal("the spec states Checksum is the identity of the CONTENT while Name is the identity of the step — identical SQL under different names must checksum the same")
	}
}

func TestSpecFromFSOrdersZeroPaddedNamesAndFiltersNonSQL(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/0002_second.sql": &fstest.MapFile{Data: []byte("create table second (id int)")},
		"migrations/0001_first.sql":  &fstest.MapFile{Data: []byte("create table first (id int)")},
		"migrations/0010_tenth.sql":  &fstest.MapFile{Data: []byte("create table tenth (id int)")},
		"migrations/README.txt":      &fstest.MapFile{Data: []byte("not a migration")},
	}
	ms, err := postgres.FromFS(fsys, "migrations")
	if err != nil {
		t.Fatalf("FromFS on a well-formed directory must not error: %v", err)
	}
	if len(ms) != 3 {
		t.Fatalf("FromFS must read only *.sql from dir, got %d migrations (want 3, README.txt must be excluded)", len(ms))
	}
	want := []string{"0001_first.sql", "0002_second.sql", "0010_tenth.sql"}
	for i, m := range ms {
		if m.Name != want[i] {
			t.Fatalf("FromFS must order by name; position %d is %q, want %q (got order %v)", i, m.Name, want[i], ms)
		}
	}
}

func TestSpecFromFSUnpaddedPrefixesSortLexicographicallyNotNumerically(t *testing.T) {
	// This is not a guess: the spec gives this exact example as the
	// documented footgun ("`10_x.sql` before `9_x.sql` is a bug that only
	// appears once there are ten migrations"), which only makes sense if
	// FromFS orders by plain string comparison of the name.
	fsys := fstest.MapFS{
		"migrations/9_ninth.sql":  &fstest.MapFile{Data: []byte("create table ninth (id int)")},
		"migrations/10_tenth.sql": &fstest.MapFile{Data: []byte("create table tenth (id int)")},
	}
	ms, err := postgres.FromFS(fsys, "migrations")
	if err != nil {
		t.Fatalf("FromFS must not error on this directory: %v", err)
	}
	if len(ms) != 2 {
		t.Fatalf("got %d migrations, want 2", len(ms))
	}
	if !(ms[0].Name == "10_tenth.sql" && ms[1].Name == "9_ninth.sql") {
		t.Fatalf("FromFS orders by name as a plain string sort, so unpadded prefixes are the documented footgun: want [10_tenth.sql 9_ninth.sql], got %v", []string{ms[0].Name, ms[1].Name})
	}
}

func TestSpecFromFSNonexistentDirReturnsError(t *testing.T) {
	fsys := fstest.MapFS{}
	_, err := postgres.FromFS(fsys, "does-not-exist")
	if err == nil {
		t.Fatal("FromFS on a directory that does not exist must return an error, not silently produce zero migrations")
	}
}

func TestSpecIsConstraintProperty(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		constraint string
		want       bool
	}{
		{
			name:       "a PgError naming the same constraint reports true",
			err:        &pgconn.PgError{Code: "23505", ConstraintName: "uq_email"},
			constraint: "uq_email",
			want:       true,
		},
		{
			name:       "a PgError naming a different constraint reports false",
			err:        &pgconn.PgError{Code: "23505", ConstraintName: "uq_email"},
			constraint: "uq_phone",
			want:       false,
		},
		{
			name:       "nil fails closed to false, the safe member for an unset value",
			err:        nil,
			constraint: "uq_email",
			want:       false,
		},
		{
			name:       "an error carrying no constraint at all reports false",
			err:        fmt.Errorf("some unrelated failure"),
			constraint: "uq_email",
			want:       false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := postgres.IsConstraint(tc.err, tc.constraint)
			if got != tc.want {
				t.Fatalf("IsConstraint(%v, %q) = %v, want %v — a repository turns a database refusal into a domain sentinel only when the constraint truly matches", tc.err, tc.constraint, got, tc.want)
			}
		})
	}
}

func TestSpecIsConstraintSeesThroughWrappedErrors(t *testing.T) {
	// INFERENCE: the spec does not state whether IsConstraint looks through
	// ordinary %w wrapping to find the PgError, only that it is "how a
	// repository turns 'the database refused this' into a domain sentinel,
	// without anything above it learning a SQLSTATE" — which presumes
	// callers may wrap on the way up. Tested, flagged as inferred.
	inner := &pgconn.PgError{Code: "23503", ConstraintName: "fk_owner"}
	wrapped := fmt.Errorf("insert failed: %w", inner)
	if !postgres.IsConstraint(wrapped, "fk_owner") {
		t.Fatal("IsConstraint should see through ordinary error wrapping to find the violated constraint (inferred behavior)")
	}
}

func TestSpecTranslateSQLSTATETable(t *testing.T) {
	cases := []struct {
		name     string
		code     string
		wantKind errs.Kind
	}{
		{"unique_violation (23505) is Conflict: the row is already there", "23505", errs.Conflict},
		{"exclusion_violation (23P01) is Conflict: the row is already there", "23P01", errs.Conflict},
		{"foreign_key_violation (23503) is Invalid: named parent absent (spec resolves the documented ambiguity to Invalid because inserts referencing a missing parent outnumber restricted deletes)", "23503", errs.Invalid},
		{"not_null_violation (23502) is Invalid: the request is not well-formed", "23502", errs.Invalid},
		{"check_violation (23514) is Unprocessable: well-formed, refused by a rule", "23514", errs.Unprocessable},
		{"invalid_text_representation (22P02) is Invalid: the value will not fit the column", "22P02", errs.Invalid},
		{"string_data_right_truncation (22001) is Invalid: the value will not fit the column", "22001", errs.Invalid},
		{"numeric_value_out_of_range (22003) is Invalid", "22003", errs.Invalid},
		{"serialization_failure (40001) is Unavailable and retryable", "40001", errs.Unavailable},
		{"deadlock_detected (40P01) is Unavailable and retryable", "40P01", errs.Unavailable},
		{"lock_not_available (55P03) is Unavailable", "55P03", errs.Unavailable},
		{"too_many_connections (53300) is Unavailable", "53300", errs.Unavailable},
		{"connection_exception (08000, class 08 member 1) is Unavailable: the connection went away", "08000", errs.Unavailable},
		{"connection_failure (08006, class 08 member 2) is Unavailable: the connection went away", "08006", errs.Unavailable},
		{"syntax_error (42601, class 42 member 1) is Internal: our SQL is wrong, not the caller's", "42601", errs.Internal},
		// AS-WRITTEN this case asserted Internal, faithfully following the
		// document's `42*** Internal` row, and it FAILED: the code carves 42501
		// out as Forbidden. Triaged in custody/evidence/0013 as "the spec is
		// wrong (about the table) and the decision is owed (about which way to
		// fix it)". doc.go was amended to state the carve-out and to record the
		// open question; this expectation now follows the amended spec. If the
		// decision goes the other way and the case is deleted from kindOf, this
		// line goes back to errs.Internal.
		{"insufficient_privilege (42501) is Forbidden: carved out of the 42 class — see doc.go, the carve-out is recorded as unsettled", "42501", errs.Forbidden},
		{"division_by_zero (22012) is Internal", "22012", errs.Internal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pgErr := &pgconn.PgError{Code: tc.code}
			got := postgres.Translate(context.Background(), pgErr, "a caller-safe message")
			if got == nil {
				t.Fatal("Translate must never return nil for a real driver error")
			}
			if !errs.IsKind(got, tc.wantKind) {
				t.Fatalf("SQLSTATE %s must translate to %s so a caller knows how to react; got kinds %v", tc.code, tc.wantKind, errs.KindsOf(got))
			}
			if want := tc.wantKind.Retryable(); errs.Retryable(got) != want {
				t.Fatalf("SQLSTATE %s maps to %s, whose own Retryable() is %v — errors.Retryable(translated) must agree, a caller must not get two different answers to the same question", tc.code, tc.wantKind, want)
			}
		})
	}
}

func TestSpecTranslate57014DistinguishesTimeoutFromCanceledByContext(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "57014"}

	t.Run("a live context means the statement_timeout fired: Timeout, retryable", func(t *testing.T) {
		got := postgres.Translate(context.Background(), pgErr, "query took too long")
		if !errs.IsKind(got, errs.Timeout) {
			t.Fatalf("57014 on a context that was not canceled must be Timeout, got kinds %v", errs.KindsOf(got))
		}
		if !errs.Retryable(got) {
			t.Fatal("a statement_timeout is retryable: the caller is still there and can try again")
		}
	})

	t.Run("a canceled context means the caller left: Canceled, not retryable", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		got := postgres.Translate(ctx, pgErr, "query took too long")
		if !errs.IsKind(got, errs.Canceled) {
			t.Fatalf("57014 on a canceled context must be Canceled, got kinds %v", errs.KindsOf(got))
		}
		if errs.Retryable(got) {
			t.Fatal("retrying a caller-canceled request spends work on somebody who has already gone")
		}
	})
}

func TestSpecTranslateUnwrapsWrappedPgError(t *testing.T) {
	// INFERENCE: whether Translate finds a *pgconn.PgError through ordinary
	// %w wrapping, as opposed to only a bare PgError, is not stated in the
	// package doc. Tested as the natural Go convention, flagged as inferred.
	inner := &pgconn.PgError{Code: "23505"}
	wrapped := fmt.Errorf("insert: %w", inner)
	got := postgres.Translate(context.Background(), wrapped, "msg")
	if !errs.IsKind(got, errs.Conflict) {
		t.Fatalf("Translate should see the SQLSTATE through ordinary wrapping (inferred); got kinds %v", errs.KindsOf(got))
	}
}

func TestSpecTranslateWrapsGenericErrorWithoutLosingIt(t *testing.T) {
	// SILENCE: the spec's own SQLSTATE table only covers *pgconn.PgError
	// codes. It does not state what Translate does with pgx.ErrNoRows or
	// with an error that carries no SQLSTATE at all — deliberately not
	// guessed here, no Kind is asserted for either shape. What follows from
	// Translate's stated job ("turns a driver error into this codebase's
	// vocabulary") is that it wraps rather than discards; that much is
	// tested.
	plain := fmt.Errorf("dial tcp: connection reset")
	got := postgres.Translate(context.Background(), plain, "msg")
	if got == nil {
		t.Fatal("Translate must return a non-nil error for a non-nil input")
	}
	if !errs.Is(got, plain) {
		t.Fatal("an error with no SQLSTATE must still be carried in the chain, not discarded — a caller further up may still need to compare against it")
	}
}

func TestSpecTranslateNeverLeaksDriverDetailIntoCallerMessage(t *testing.T) {
	canary := "CANARY-do-not-show-caller-9f3a1c"
	pgErr := &pgconn.PgError{
		Code:    "23505",
		Message: canary,
		Detail:  canary,
		Hint:    canary,
		Where:   canary,
	}
	safeMsg := "that target already exists"
	got := postgres.Translate(context.Background(), pgErr, safeMsg)
	if got == nil {
		t.Fatal("Translate must not return nil for a real driver error")
	}
	if shown := errs.Message(got); strings.Contains(shown, canary) {
		t.Fatalf("errors.Message(...) is what a caller sees; it must never contain raw driver diagnostics such as SQL detail — got %q", shown)
	}

	var recovered *pgconn.PgError
	if !errs.As(got, &recovered) {
		t.Fatal("the original driver error must still be reachable in the chain for diagnostics — discarding it here can never be recovered by a caller further up")
	} else if recovered.Detail != canary {
		t.Fatal("the driver error reachable via the chain must be the same one Translate received, not a stripped copy")
	}
}

func TestSpecTranslateMessageIsSafeForNonInternalAndHiddenForInternal(t *testing.T) {
	// INFERENCE: the spec states Msg is caller-safe, and the pkg/errors doc
	// states "Message additionally hides Msg for Internal" — implying the
	// base case (non-Internal) shows Msg unchanged. It does not say in so
	// many words that Translate sets the resulting Error's Msg field to
	// exactly the `msg` argument passed to it; that is inferred from the
	// parallel with errors.Wrap(err, kind, msg)'s identical shape.
	safeMsg := "could not save the target"
	conflict := postgres.Translate(context.Background(), &pgconn.PgError{Code: "23505"}, safeMsg)
	if got := errs.Message(conflict); got != safeMsg {
		t.Fatalf("a non-Internal kind must show the caller-supplied message unchanged: got %q, want %q (inferred wiring)", got, safeMsg)
	}

	internal := postgres.Translate(context.Background(), &pgconn.PgError{Code: "42601"}, safeMsg)
	if got := errs.Message(internal); got == safeMsg {
		t.Fatalf("Internal must hide Msg per the errors package contract — got the raw caller message %q back unchanged", got)
	}
}

func TestSpecOpenRejectsMalformedDSNBeforeConnecting(t *testing.T) {
	_, err := postgres.Open(context.Background(), postgres.Config{DSN: "postgres://[::1"})
	if err == nil {
		t.Fatal("Open is documented to parse the DSN before connecting; a malformed DSN must fail parsing rather than attempt to reach an undecodable address")
	}
}

func TestSpecOpenFailsForUnreachableServerWithoutHanging(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := postgres.Open(ctx, postgres.Config{
		DSN:            "postgres://spec:spec@127.0.0.1:1/spec?sslmode=disable",
		ConnectTimeout: 3 * time.Second,
	})
	if err == nil {
		t.Fatal("Open must verify the connection before returning; a server that is not there must produce an error, not a pool whose first failure arrives on the first query")
	}
}

// ═══════════════════════════════════════════════════════════════════════
// DATABASE-DEPENDENT TESTS — skip when OVERWATCH_TEST_DSN is unset
// ═══════════════════════════════════════════════════════════════════════

func TestSpecOpenPingSucceedsAgainstRealServer(t *testing.T) {
	dsn := specIsolatedDSN(t)
	pool := specOpenPool(t, dsn)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("Ping against a server Open already verified must succeed: %v", err)
	}
}

func TestSpecPoolDBRunsQueryOutsideTransaction(t *testing.T) {
	dsn := specIsolatedDSN(t)
	pool := specOpenPool(t, dsn)
	ctx := context.Background()
	var one int
	if err := pool.DB(ctx).QueryRow(ctx, "select 1").Scan(&one); err != nil {
		t.Fatalf("Pool.DB outside any transaction must be usable to run a query: %v", err)
	}
	if one != 1 {
		t.Fatalf("select 1 returned %d", one)
	}
}

func TestSpecPoolRawReturnsUsableUnderlyingPool(t *testing.T) {
	dsn := specIsolatedDSN(t)
	pool := specOpenPool(t, dsn)
	raw := pool.Raw()
	if raw == nil {
		t.Fatal("Raw is documented as the escape hatch for CopyFrom, dedicated connections and pool stats — it must not be nil once the pool is open")
	}
	if err := raw.Ping(context.Background()); err != nil {
		t.Fatalf("Raw() must return a live, usable underlying pool: %v", err)
	}
}

func TestSpecInTxCommitsWhenFnSucceeds(t *testing.T) {
	dsn := specIsolatedDSN(t)
	pool := specOpenPool(t, dsn)
	ctx := context.Background()
	if _, err := pool.DB(ctx).Exec(ctx, "create table t (id int primary key)"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	err := pool.InTx(ctx, func(ctx context.Context) error {
		_, err := pool.DB(ctx).Exec(ctx, "insert into t (id) values (1)")
		return err
	})
	if err != nil {
		t.Fatalf("InTx must commit when fn returns nil: %v", err)
	}
	if n := specCount(t, pool, "t"); n != 1 {
		t.Fatalf("InTx committed successfully but the row is not visible: count = %d, want 1", n)
	}
}

func TestSpecInTxRollsBackWhenFnReturnsError(t *testing.T) {
	dsn := specIsolatedDSN(t)
	pool := specOpenPool(t, dsn)
	ctx := context.Background()
	if _, err := pool.DB(ctx).Exec(ctx, "create table t (id int primary key)"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	err := pool.InTx(ctx, func(ctx context.Context) error {
		if _, err := pool.DB(ctx).Exec(ctx, "insert into t (id) values (1)"); err != nil {
			return err
		}
		return fmt.Errorf("boom")
	})
	if err == nil {
		t.Fatal("InTx must propagate the error fn returned")
	}
	if n := specCount(t, pool, "t"); n != 0 {
		t.Fatalf("InTx must roll back everything when fn returns an error rather than commit a partial operation: count = %d, want 0", n)
	}
}

func TestSpecInTxRollsBackAndRePanicsOnPanic(t *testing.T) {
	dsn := specIsolatedDSN(t)
	pool := specOpenPool(t, dsn)
	ctx := context.Background()
	if _, err := pool.DB(ctx).Exec(ctx, "create table events (id int primary key)"); err != nil {
		t.Fatalf("setup: %v", err)
	}

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("a panic inside InTx must be re-raised, not swallowed into a silent no-op commit")
			}
		}()
		_ = pool.InTx(ctx, func(ctx context.Context) error {
			_, _ = pool.DB(ctx).Exec(ctx, "insert into events (id) values (1)")
			panic("spec-induced panic")
		})
		t.Fatal("unreachable: InTx must propagate the panic to its caller instead of returning normally")
	}()

	if n := specCount(t, pool, "events"); n != 0 {
		t.Fatalf("a panic inside InTx must roll back everything it did, the same as any other error: count = %d, want 0", n)
	}
}

func TestSpecInTxNestedCallJoinsSingleTransactionNotIndependentOne(t *testing.T) {
	dsn := specIsolatedDSN(t)
	pool := specOpenPool(t, dsn)
	ctx := context.Background()
	if _, err := pool.DB(ctx).Exec(ctx, "create table t (id int primary key)"); err != nil {
		t.Fatalf("setup: %v", err)
	}

	outerErr := pool.InTx(ctx, func(ctx context.Context) error {
		// If the nested InTx opened an independent transaction, it would
		// commit row 2 here regardless of what the outer transaction does.
		if err := pool.InTx(ctx, func(ctx context.Context) error {
			_, err := pool.DB(ctx).Exec(ctx, "insert into t (id) values (2)")
			return err
		}); err != nil {
			return err
		}
		if _, err := pool.DB(ctx).Exec(ctx, "insert into t (id) values (1)"); err != nil {
			return err
		}
		return fmt.Errorf("force outer rollback")
	})
	if outerErr == nil {
		t.Fatal("the outer InTx was made to fail and must report that failure")
	}
	if n := specCount(t, pool, "t"); n != 0 {
		t.Fatalf("a nested InTx must JOIN the outer transaction rather than open a second one: the outer rolled back, so the nested insert must be gone too, but count = %d", n)
	}
}

func TestSpecPoolDBInsideInTxIsInvisibleToOtherConnectionsUntilCommit(t *testing.T) {
	dsn := specIsolatedDSN(t)
	p1 := specOpenPool(t, dsn)
	p2 := specOpenPool(t, dsn)
	bg := context.Background()
	if _, err := p1.DB(bg).Exec(bg, "create table visibility_check (id int primary key)"); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var sawDuringTx bool
	err := p1.InTx(bg, func(txCtx context.Context) error {
		if _, err := p1.DB(txCtx).Exec(txCtx, "insert into visibility_check (id) values (1)"); err != nil {
			return err
		}
		// Deliberately use a fresh, tx-less context on a separate pool: this
		// is an outside observer, not a participant in p1's transaction.
		var count int
		if err := p2.DB(context.Background()).QueryRow(context.Background(),
			"select count(*) from visibility_check").Scan(&count); err != nil {
			return err
		}
		sawDuringTx = count > 0
		return nil
	})
	if err != nil {
		t.Fatalf("InTx: %v", err)
	}
	if sawDuringTx {
		t.Fatal("a query run via Pool.DB inside InTx must go through the transaction, not a separate autocommitted connection — otherwise it becomes visible to others before commit")
	}

	var countAfter int
	if err := p2.DB(context.Background()).QueryRow(context.Background(),
		"select count(*) from visibility_check").Scan(&countAfter); err != nil {
		t.Fatalf("post-commit check: %v", err)
	}
	if countAfter != 1 {
		t.Fatalf("after InTx returns without error the transaction must be committed and visible to other connections: count = %d, want 1", countAfter)
	}
}

func TestSpecMigrateAppliesInOrderRecordsThemAndIsIdempotentOnRerun(t *testing.T) {
	dsn := specIsolatedDSN(t)
	pool := specOpenPool(t, dsn)
	ctx := context.Background()
	ms := []postgres.Migration{
		{Name: "0001_create_widgets.sql", SQL: "create table widgets (id int primary key)"},
		{Name: "0002_create_gadgets.sql", SQL: "create table gadgets (id int primary key)"},
	}

	// The precise meaning of the returned int is not spelled out beyond
	// "applies every migration not yet recorded" plus the (int, error)
	// signature; read here as "count applied during this call", the only
	// interpretation that gives the return value a use distinct from a
	// plain error. Flagged as an inference.
	n, err := postgres.Migrate(ctx, pool, ms)
	if err != nil {
		t.Fatalf("Migrate of two well-formed migrations into a fresh schema must succeed: %v", err)
	}
	if n != len(ms) {
		t.Fatalf("Migrate reported applying %d migrations, want %d (inferred meaning of the returned int: count applied this call)", n, len(ms))
	}
	if !specTableExists(t, pool, "widgets") || !specTableExists(t, pool, "gadgets") {
		t.Fatal("both migrations were reported applied but at least one table is missing")
	}

	n2, err2 := postgres.Migrate(ctx, pool, ms)
	if err2 != nil {
		t.Fatalf("re-running Migrate with an identical, already-recorded set must not error: %v", err2)
	}
	if n2 != 0 {
		t.Fatalf("Migrate applied %d migrations on a rerun of an already-recorded set, want 0 — Migrate applies every migration NOT YET recorded, and these all are", n2)
	}
}

func TestSpecMigratePartialFailureLeavesPrefixAppliedAndRestNot(t *testing.T) {
	dsn := specIsolatedDSN(t)
	pool := specOpenPool(t, dsn)
	ctx := context.Background()
	ms := []postgres.Migration{
		{Name: "0001_create_a.sql", SQL: "create table a (id int primary key)"},
		{Name: "0002_broken.sql", SQL: "this is not valid sql at all ;;;"},
		{Name: "0003_create_c.sql", SQL: "create table c (id int primary key)"},
	}
	_, err := postgres.Migrate(ctx, pool, ms)
	if err == nil {
		t.Fatal("a migration with invalid SQL must cause Migrate to fail")
	}
	if !specTableExists(t, pool, "a") {
		t.Fatal("everything before the failing migration must have applied — that is what makes a per-migration-transaction failure recoverable")
	}
	if specTableExists(t, pool, "c") {
		t.Fatal("everything after the failing migration must NOT have applied — a failure must not silently skip ahead")
	}
}

func TestSpecMigrateConcurrentCallsSerializeAndNeverDuplicateApplication(t *testing.T) {
	dsn := specIsolatedDSN(t)
	p1 := specOpenPool(t, dsn)
	p2 := specOpenPool(t, dsn)
	ctx := context.Background()
	ms := []postgres.Migration{
		{Name: "0001_create_race_check.sql", SQL: "create table race_check (id int primary key)"},
	}

	var n1, n2 int
	var err1, err2 error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); n1, err1 = postgres.Migrate(ctx, p1, ms) }()
	go func() { defer wg.Done(); n2, err2 = postgres.Migrate(ctx, p2, ms) }()
	wg.Wait()

	if err1 != nil {
		t.Fatalf("first concurrent Migrate: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("second concurrent Migrate: %v", err2)
	}
	if n1+n2 != len(ms) {
		t.Fatalf("the advisory lock must serialize two processes starting together so a migration is applied exactly once; got applied counts %d and %d summing to %d, want %d", n1, n2, n1+n2, len(ms))
	}
}

func TestSpecRealUniqueViolationIsRecognizedByNameAndTranslatedToConflict(t *testing.T) {
	dsn := specIsolatedDSN(t)
	pool := specOpenPool(t, dsn)
	ctx := context.Background()

	if _, err := pool.DB(ctx).Exec(ctx,
		"create table accounts (id int primary key, email text, constraint uq_accounts_email unique (email))"); err != nil {
		t.Fatalf("setup: create table: %v", err)
	}
	if _, err := pool.DB(ctx).Exec(ctx, "insert into accounts (id, email) values (1, 'a@example.com')"); err != nil {
		t.Fatalf("setup: first insert: %v", err)
	}
	_, dupErr := pool.DB(ctx).Exec(ctx, "insert into accounts (id, email) values (2, 'a@example.com')")
	if dupErr == nil {
		t.Fatal("setup: expected a real unique_violation from the server, got none")
	}

	if !postgres.IsConstraint(dupErr, "uq_accounts_email") {
		t.Fatal("IsConstraint must recognize the exact constraint the server reported as violated")
	}
	if postgres.IsConstraint(dupErr, "some_unrelated_constraint") {
		t.Fatal("IsConstraint must not report true for a constraint that was not the one violated")
	}

	translated := postgres.Translate(ctx, dupErr, "that email is already registered")
	if !errs.IsKind(translated, errs.Conflict) {
		t.Fatalf("a real unique_violation must translate to Conflict, got kinds %v", errs.KindsOf(translated))
	}
	// INFERENCE: the pkg/errors doc's own example of who attaches a Detail
	// is "pkg/postgres knows the constraint" — read here as meaning Translate
	// attaches one for a constraint violation, though it is not spelled out
	// verbatim for every SQLSTATE.
	if len(errs.DetailsOf(translated)) == 0 {
		t.Fatal("expected a diagnostic detail attached at the point the constraint is known (inferred from the pkg/errors doc's own example)")
	}
}
