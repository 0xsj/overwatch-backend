// Package env_test is an external, specification-driven test suite for
// pkg/env. It was written without sight of the implementation, from the
// package doc comment and the errors-package contract it is specified
// against. Every assertion traces to a specific claim in that prose; places
// where the prose does not settle a question are marked INFERENCE at the
// point where a guess was still made, or left untested with a note in the
// accompanying report.
package env_test

import (
	"os"
	"strings"
	"testing"

	"github.com/0xsj/overwatch-backend/pkg/env"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

func newReader(t *testing.T, m map[string]string) *env.Reader {
	t.Helper()
	return env.New(env.Map(m))
}

func declaredByKey(t *testing.T, r *env.Reader) map[string]env.Var {
	t.Helper()
	out := map[string]env.Var{}
	for _, v := range r.Declared() {
		out[v.Key] = v
	}
	return out
}

// ---------------------------------------------------------------------
// Lookup is a port: Map and OS must agree on what "absent" and "empty" mean,
// since every getter's three-state table is built on top of that contract.
// ---------------------------------------------------------------------

func TestMap_DistinguishesAbsentEmptySet(t *testing.T) {
	lookup := env.Map(map[string]string{"EMPTY": "", "SET": "value"})

	tests := []struct {
		name        string
		key         string
		wantValue   string
		wantPresent bool
	}{
		{"a key never placed in the map is absent, not empty", "MISSING", "", false},
		{"a key present with an empty string is present, distinct from absent", "EMPTY", "", true},
		{"a key with a value is present with that value", "SET", "value", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, present := lookup(tt.key)
			if v != tt.wantValue || present != tt.wantPresent {
				t.Errorf("distinguishing absent from empty is the entire premise of the three-state design; got value=%q present=%v, want value=%q present=%v",
					v, present, tt.wantValue, tt.wantPresent)
			}
		})
	}
}

func TestMap_NilMapIsSafeAndReportsAbsent(t *testing.T) {
	lookup := env.Map(nil)
	v, present := lookup("ANYTHING")
	if present || v != "" {
		t.Errorf("Map(nil) must behave like a source with nothing in it (every key absent), not panic or fabricate presence; got value=%q present=%v", v, present)
	}
}

func TestOS_ReflectsProcessEnvironment(t *testing.T) {
	t.Run("a variable set in the process environment is present with its value", func(t *testing.T) {
		t.Setenv("OVERWATCH_ENV_SPEC_TEST_VAR", "hello")
		v, present := env.OS()("OVERWATCH_ENV_SPEC_TEST_VAR")
		if !present || v != "hello" {
			t.Errorf("OS() must read the real process environment; got value=%q present=%v", v, present)
		}
	})

	t.Run("a variable never set in the process is absent, not empty", func(t *testing.T) {
		// Best effort: this name is chosen to be unlikely to exist in any real
		// environment. It can only false-positive-fail if something external
		// happens to define exactly this variable.
		const key = "OVERWATCH_ENV_SPEC_TEST_DEFINITELY_UNSET_VAR_9f3c1"
		os.Unsetenv(key)
		v, present := env.OS()(key)
		if present || v != "" {
			t.Errorf("an unset process variable must report present=false; got value=%q present=%v", v, present)
		}
	})
}

// ---------------------------------------------------------------------
// The three-state table, one getter at a time. This is the richest part of
// the spec: each row names a different reaction to absent/empty/set, and a
// naive implementation collapsing any two of the three is exactly the bug
// this package exists to prevent.
// ---------------------------------------------------------------------

func TestRequired_ThreeStates(t *testing.T) {
	t.Run("absent key is a problem and yields the zero value", func(t *testing.T) {
		r := newReader(t, map[string]string{})
		if got := r.Required("DATABASE_URL"); got != "" {
			t.Errorf("a failed getter returns the zero value, not a guess; got %q", got)
		}
		if r.Err() == nil {
			t.Fatalf("a required variable that was never set must fail the reader, not silently pass")
		}
	})

	t.Run("empty value is a problem, identically to absent", func(t *testing.T) {
		r := newReader(t, map[string]string{"DATABASE_URL": ""})
		if got := r.Required("DATABASE_URL"); got != "" {
			t.Errorf("a failed getter returns the zero value; got %q", got)
		}
		if r.Err() == nil {
			t.Fatalf("Required must treat an explicitly empty value as a problem, not as the value — collapsing empty and absent is exactly what this package exists to prevent")
		}
	})

	t.Run("set value is returned verbatim and is not a problem", func(t *testing.T) {
		r := newReader(t, map[string]string{"DATABASE_URL": "postgres://x"})
		if got := r.Required("DATABASE_URL"); got != "postgres://x" {
			t.Errorf("Required must return the value exactly as provided, got %q", got)
		}
		if err := r.Err(); err != nil {
			t.Errorf("a required variable that was set must not fail the reader, got %v", err)
		}
	})
}

func TestString_ThreeStates(t *testing.T) {
	t.Run("absent key falls back to the configured default", func(t *testing.T) {
		r := newReader(t, map[string]string{})
		if got := r.String("LOG_LEVEL", "info"); got != "info" {
			t.Errorf("an absent String() variable must resolve to its fallback, got %q", got)
		}
		if err := r.Err(); err != nil {
			t.Errorf("falling back to a default is not a problem, got %v", err)
		}
	})

	t.Run("empty value IS the value — the fallback must not override an operator's explicit empty string", func(t *testing.T) {
		r := newReader(t, map[string]string{"LOG_LEVEL": ""})
		if got := r.String("LOG_LEVEL", "info"); got != "" {
			t.Errorf("String() documents \"\\\"\\\" is the value\" for the empty cell — an operator who explicitly set the variable to empty must see empty back, not the fallback; got %q", got)
		}
		if err := r.Err(); err != nil {
			t.Errorf("an explicit empty string is not a problem for String(), got %v", err)
		}
	})

	t.Run("set value is returned and is not a problem", func(t *testing.T) {
		r := newReader(t, map[string]string{"LOG_LEVEL": "debug"})
		if got := r.String("LOG_LEVEL", "info"); got != "debug" {
			t.Errorf("String() must return the set value over the fallback, got %q", got)
		}
		if err := r.Err(); err != nil {
			t.Errorf("a set value is not a problem, got %v", err)
		}
	})
}

func TestRequiredInt_ThreeStates(t *testing.T) {
	t.Run("absent key is a problem and yields the zero value", func(t *testing.T) {
		r := newReader(t, map[string]string{})
		if got := r.RequiredInt("PORT"); got != 0 {
			t.Errorf("a failed getter returns the zero value, got %d", got)
		}
		if r.Err() == nil {
			t.Fatalf("a required integer that was never set must fail the reader")
		}
	})

	t.Run("empty value is a problem, identically to absent", func(t *testing.T) {
		r := newReader(t, map[string]string{"PORT": ""})
		if got := r.RequiredInt("PORT"); got != 0 {
			t.Errorf("a failed getter returns the zero value, got %d", got)
		}
		if r.Err() == nil {
			t.Fatalf("RequiredInt must treat an explicitly empty value as a problem, not as zero-that-happens-to-parse")
		}
	})

	t.Run("set value parses to the integer and is not a problem", func(t *testing.T) {
		r := newReader(t, map[string]string{"PORT": "443"})
		if got := r.RequiredInt("PORT"); got != 443 {
			t.Errorf("RequiredInt must parse the set value, got %d", got)
		}
		if err := r.Err(); err != nil {
			t.Errorf("a valid set integer is not a problem, got %v", err)
		}
	})

	t.Run("set but unparseable value is a problem and yields the zero value, not a guess", func(t *testing.T) {
		r := newReader(t, map[string]string{"PORT": "abc"})
		if got := r.RequiredInt("PORT"); got != 0 {
			t.Errorf("a failed parse returns the zero value, got %d", got)
		}
		if r.Err() == nil {
			t.Fatalf("an unparseable required integer must fail the reader")
		}
	})
}

func TestInt_ThreeStates(t *testing.T) {
	t.Run("absent key falls back to the configured default", func(t *testing.T) {
		r := newReader(t, map[string]string{})
		if got := r.Int("PORT", 8080); got != 8080 {
			t.Errorf("an absent Int() variable must resolve to its fallback, got %d", got)
		}
		if err := r.Err(); err != nil {
			t.Errorf("falling back to a default is not a problem, got %v", err)
		}
	})

	t.Run("empty value is a problem — unlike String, Int does not accept empty as a value nor silently fall back", func(t *testing.T) {
		r := newReader(t, map[string]string{"PORT": ""})
		got := r.Int("PORT", 8080)
		if got == 8080 {
			t.Errorf("Int's empty cell is documented as \"problem\", not \"fallback\" — returning the fallback here would hide a real misconfiguration behind a value that looks fine; got %d", got)
		}
		if got != 0 {
			t.Errorf("a failed getter returns the zero value, got %d", got)
		}
		if r.Err() == nil {
			t.Fatalf("an explicitly empty Int() variable must fail the reader")
		}
	})

	t.Run("set value parses to the integer and is not a problem", func(t *testing.T) {
		r := newReader(t, map[string]string{"PORT": "9090"})
		if got := r.Int("PORT", 8080); got != 9090 {
			t.Errorf("Int() must return the parsed set value over the fallback, got %d", got)
		}
		if err := r.Err(); err != nil {
			t.Errorf("a valid set integer is not a problem, got %v", err)
		}
	})

	t.Run("set but unparseable value is a problem and yields the zero value, not the fallback", func(t *testing.T) {
		r := newReader(t, map[string]string{"PORT": "seven"})
		got := r.Int("PORT", 8080)
		if got == 8080 {
			t.Errorf("falling back on a parse failure would mask the bad config as if it were never set; got the fallback %d", got)
		}
		if got != 0 {
			t.Errorf("a failed parse returns the zero value, got %d", got)
		}
		if r.Err() == nil {
			t.Fatalf("an unparseable Int() variable must fail the reader")
		}
	})

	t.Run("negative integers parse correctly", func(t *testing.T) {
		r := newReader(t, map[string]string{"OFFSET": "-1"})
		if got := r.Int("OFFSET", 0); got != -1 {
			t.Errorf("Int() must accept negative integers, got %d", got)
		}
		if err := r.Err(); err != nil {
			t.Errorf("a valid negative integer is not a problem, got %v", err)
		}
	})
}

func TestEnum_ThreeStates(t *testing.T) {
	t.Run("absent key falls back to the configured default", func(t *testing.T) {
		r := newReader(t, map[string]string{})
		if got := r.Enum("MODE", "dev", "dev", "prod"); got != "dev" {
			t.Errorf("an absent Enum() variable must resolve to its fallback, got %q", got)
		}
		if err := r.Err(); err != nil {
			t.Errorf("falling back to a default is not a problem, got %v", err)
		}
	})

	t.Run("empty value is a problem and yields the zero value, not the fallback", func(t *testing.T) {
		r := newReader(t, map[string]string{"MODE": ""})
		got := r.Enum("MODE", "dev", "dev", "prod")
		if got == "dev" {
			t.Errorf("Enum's empty cell is documented as \"problem\", not \"fallback\"; got the fallback %q", got)
		}
		if got != "" {
			t.Errorf("a failed getter returns the zero value, got %q", got)
		}
		if r.Err() == nil {
			t.Fatalf("an explicitly empty Enum() variable must fail the reader")
		}
	})

	t.Run("set value in the allowed list is returned and is not a problem", func(t *testing.T) {
		r := newReader(t, map[string]string{"MODE": "prod"})
		if got := r.Enum("MODE", "dev", "dev", "prod"); got != "prod" {
			t.Errorf("Enum() must return an allowed set value, got %q", got)
		}
		if err := r.Err(); err != nil {
			t.Errorf("an allowed set value is not a problem, got %v", err)
		}
	})

	t.Run("set value not in the allowed list is a problem and yields the zero value, not the fallback and not the rejected value", func(t *testing.T) {
		r := newReader(t, map[string]string{"MODE": "nonsense"})
		got := r.Enum("MODE", "dev", "dev", "prod")
		if got == "dev" {
			t.Errorf("falling back on a disallowed value would silently treat an operator's typo as if unset; got the fallback %q", got)
		}
		if got == "nonsense" {
			t.Errorf("Enum() must not pass a disallowed value through just because it is a well-formed string; got the rejected value back")
		}
		if got != "" {
			t.Errorf("a failed getter returns the zero value, got %q", got)
		}
		if r.Err() == nil {
			t.Fatalf("a disallowed Enum() value must fail the reader")
		}
	})
}

func TestSecret_ThreeStates(t *testing.T) {
	t.Run("absent credential is a problem — Secret has no fallback, unlike every other getter", func(t *testing.T) {
		r := newReader(t, map[string]string{})
		got := r.Secret("API_KEY")
		if !got.IsZero() {
			t.Errorf("a failed Secret read must yield the zero value, not a partially-formed credential")
		}
		if r.Err() == nil {
			t.Fatalf("Secret deliberately has no fallback form: a credential with a default is one that ships in production when the real one is missing, so an absent secret must always fail the reader")
		}
	})

	t.Run("empty credential is a problem, identically to absent", func(t *testing.T) {
		r := newReader(t, map[string]string{"API_KEY": ""})
		got := r.Secret("API_KEY")
		if !got.IsZero() {
			t.Errorf("an explicitly empty credential must still fail, not be accepted as an empty secret")
		}
		if r.Err() == nil {
			t.Fatalf("Secret's empty state is documented as a problem, identically to absent")
		}
	})

	t.Run("set credential is revealed exactly as provided and is not a problem", func(t *testing.T) {
		const raw = "sk-abc123"
		r := newReader(t, map[string]string{"API_KEY": raw})
		got := r.Secret("API_KEY")
		if got.Reveal() != raw {
			t.Errorf("Reveal must return exactly what the source provided, got %q want %q", got.Reveal(), raw)
		}
		if err := r.Err(); err != nil {
			t.Errorf("a credential that was set must not fail the reader, got %v", err)
		}
		if got.String() == raw {
			t.Errorf("even a value produced by Reader.Secret must redact on String() — a raw secret must never print as itself")
		}
	})
}

// ---------------------------------------------------------------------
// Cross-cutting: "a getter that fails records the problem and returns the
// zero value" — never the fallback. Stated once directly here because it is
// easy to assume a failed getter degrades gracefully to its default.
// ---------------------------------------------------------------------

func TestOnProblem_NeverFallsBackToTheConfiguredDefault(t *testing.T) {
	t.Run("Int with an unparseable value returns zero, not the fallback that would mask the bad config", func(t *testing.T) {
		r := newReader(t, map[string]string{"PORT": "not-a-port"})
		got := r.Int("PORT", 8080)
		if got == 8080 {
			t.Errorf("a fallback here would hide a real misconfiguration behind a value that looks legitimate; got the fallback %d", got)
		}
	})

	t.Run("Enum with a disallowed value returns empty, not the fallback and not the bad value", func(t *testing.T) {
		r := newReader(t, map[string]string{"MODE": "nonsense"})
		got := r.Enum("MODE", "dev", "dev", "prod")
		if got == "dev" || got == "nonsense" {
			t.Errorf("a fallback or pass-through here would treat a disallowed value as either unset or acceptable; got %q", got)
		}
	})
}

// ---------------------------------------------------------------------
// "Every problem at once": the aggregated error.
// ---------------------------------------------------------------------

func TestErr_AggregatesEveryProblemNotJustFirst(t *testing.T) {
	r := newReader(t, map[string]string{"SET_BUT_BAD_INT": "seven"})
	_ = r.Required("DATABASE_URL")
	_ = r.Int("SET_BUT_BAD_INT", 1)
	_ = r.Secret("API_KEY")

	err := r.Err()
	if err == nil {
		t.Fatalf("three independent problems were introduced; a reader that reports none of them is worse than one that reports only the first, since the operator cannot see how much work remains")
	}

	fields := errors.FieldsOf(err)
	for _, key := range []string{"DATABASE_URL", "SET_BUT_BAD_INT", "API_KEY"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("a run with multiple invalid variables must report ALL of them at once, not stop at the first — %q is missing from %v", key, fields)
		}
	}

	if got := errors.KindOf(err); got.String() != "invalid" {
		t.Errorf("the aggregated config error must render as kind Invalid so a caller can distinguish a misconfigured process from an internal fault, got %v", got)
	}
	if !errors.IsKind(err, errors.Invalid) {
		t.Errorf("IsKind(err, errors.Invalid) must be true for the aggregated config error")
	}
}

func TestErr_NilWhenNothingFailed(t *testing.T) {
	r := newReader(t, map[string]string{"LOG_LEVEL": "info"})
	_ = r.String("LOG_LEVEL", "warn")
	_ = r.Int("PORT", 8080) // absent, uses fallback — not a problem
	if err := r.Err(); err != nil {
		t.Errorf("a reader with zero problems must report Err()==nil so a process can boot cleanly; got %v", err)
	}
}

func TestFieldsOf_ReturnsACopy(t *testing.T) {
	r := newReader(t, map[string]string{})
	_ = r.Required("A")
	err := r.Err()
	if err == nil {
		t.Fatalf("setup: expected a problem after reading an absent required variable")
	}

	f1 := errors.FieldsOf(err)
	f1["A"] = "tampered"
	f1["INJECTED"] = "should not appear anywhere else"

	f2 := errors.FieldsOf(err)
	if f2["A"] == "tampered" {
		t.Errorf("FieldsOf is documented to return a copy; mutating a previously returned map altered the error's own state")
	}
	if _, ok := f2["INJECTED"]; ok {
		t.Errorf("FieldsOf is documented to return a copy; a key injected into one call's result leaked into a later call")
	}
}

func TestFailuresAreIsolatedPerKey(t *testing.T) {
	r := newReader(t, map[string]string{"GOOD": "fine-value"})
	good := r.String("GOOD", "fallback")
	_ = r.Required("BAD") // absent -> problem

	if good != "fine-value" {
		t.Errorf("a getter that already returned a value must not be retroactively changed by an unrelated failure on a different key, got %q", good)
	}

	fields := errors.FieldsOf(r.Err())
	if _, ok := fields["GOOD"]; ok {
		t.Errorf("a key that never failed must not appear among the recorded problems, got fields=%v", fields)
	}
	if _, ok := fields["BAD"]; !ok {
		t.Errorf("the actually failing key must appear among the recorded problems, got fields=%v", fields)
	}
}

func TestFields_GrowMonotonicallyAsMoreProblemsAreIntroduced(t *testing.T) {
	r := newReader(t, map[string]string{})
	_ = r.Required("FIRST")
	afterFirst := errors.FieldsOf(r.Err())
	if len(afterFirst) != 1 {
		t.Fatalf("expected exactly one recorded problem after one failing getter, got %v", afterFirst)
	}

	_ = r.Required("SECOND")
	afterSecond := errors.FieldsOf(r.Err())
	if len(afterSecond) != 2 {
		t.Fatalf("a second independent failure must add to the set of problems, never replace or drop the first — an operator fixing config one variable at a time must see the count go down, not jump around; got %v", afterSecond)
	}
	if _, ok := afterSecond["FIRST"]; !ok {
		t.Errorf("the first problem must still be present after a second is introduced, got %v", afterSecond)
	}
	if _, ok := afterSecond["SECOND"]; !ok {
		t.Errorf("the second problem must be present, got %v", afterSecond)
	}
}

func TestErr_IsIdempotent(t *testing.T) {
	r := newReader(t, map[string]string{})
	_ = r.Required("X")
	e1 := r.Err()
	e2 := r.Err()
	if (e1 == nil) != (e2 == nil) {
		t.Fatalf("Err() must be a stable read of accumulated state, not something that changes across calls with no getters run in between")
	}
	f1 := errors.FieldsOf(e1)
	f2 := errors.FieldsOf(e2)
	if len(f1) != len(f2) || f1["X"] != f2["X"] {
		t.Errorf("two calls to Err() with no getters run in between must report identical problems, got %v then %v", f1, f2)
	}
}

func TestFieldsOf_MessageText(t *testing.T) {
	t.Run("an absent required variable is recorded with the message shown in the package documentation", func(t *testing.T) {
		// INFERENCE: the doc's own worked example shows {"DATABASE_URL": "required"}
		// for a Required-family failure. The three-state table names "problem" for
		// both the absent and empty cells but does not separately give the message
		// text for "empty"; this test only pins the documented example (absent).
		r := newReader(t, map[string]string{})
		_ = r.Required("DATABASE_URL")
		fields := errors.FieldsOf(r.Err())
		if got := fields["DATABASE_URL"]; got != "required" {
			t.Errorf("INFERENCE from the package doc's own example {\"DATABASE_URL\": \"required\"}: expected message %q, got %q", "required", got)
		}
	})

	t.Run("an unparseable integer is recorded with the raw text embedded verbatim, per the package documentation's example", func(t *testing.T) {
		r := newReader(t, map[string]string{"PORT_SERVER": "seven"})
		_ = r.Int("PORT_SERVER", 0)
		fields := errors.FieldsOf(r.Err())
		want := "not a number: seven"
		if got := fields["PORT_SERVER"]; got != want {
			t.Errorf("the package doc gives this exact example {\"PORT_SERVER\": \"not a number: seven\"}: got %q, want %q — an operator needs the invalid text to fix it without re-deriving what was typed", got, want)
		}
	})

	t.Run("the invalid text is echoed verbatim for other unparseable values too", func(t *testing.T) {
		// INFERENCE: generalizing the doc's single worked example ("not a number:
		// seven") to a different raw string, assuming the pattern is literally
		// "not a number: " + the raw value rather than a one-off literal message.
		r := newReader(t, map[string]string{"PORT": "3.14"})
		_ = r.Int("PORT", 0)
		fields := errors.FieldsOf(r.Err())
		want := "not a number: 3.14"
		if got := fields["PORT"]; got != want {
			t.Errorf("INFERENCE generalizing the doc's example pattern \"not a number: <raw>\": got %q, want %q", got, want)
		}
	})
}

func TestErrorVocabulary_NilIsFailClosedNotAWildcardPass(t *testing.T) {
	r := newReader(t, map[string]string{"OK": "yes"})
	_ = r.String("OK", "fallback")
	err := r.Err()
	if err != nil {
		t.Fatalf("setup: expected a clean reader, got %v", err)
	}
	if k := errors.KindOf(err); k != errors.Internal {
		t.Errorf("KindOf(nil) must land on Internal, the fail-closed member of the enum, and not on Invalid or any kind a caller could mistake for a real classification; got %v", k)
	}
	if errors.IsKind(err, errors.Invalid) {
		t.Errorf("IsKind(nil, ...) must always be false — a nil error is not invalid config, it is the absence of a problem")
	}
}

// ---------------------------------------------------------------------
// Declared() — the boot manifest.
// ---------------------------------------------------------------------

func TestDeclared_EmptyWhenNothingWasRead(t *testing.T) {
	r := newReader(t, map[string]string{"UNUSED": "value"})
	vars := r.Declared()
	if len(vars) != 0 {
		t.Errorf("Declared must only report variables that were actually read; a boot manifest including keys nobody asked for would misrepresent what the process depends on, got %v", vars)
	}
}

func TestDeclared_KeyOrder(t *testing.T) {
	r := newReader(t, map[string]string{"ZETA": "z", "ALPHA": "a", "MID": "m"})
	// Read in an order deliberately different from alphabetical.
	_ = r.String("ZETA", "")
	_ = r.String("ALPHA", "")
	_ = r.String("MID", "")

	vars := r.Declared()
	var keys []string
	for _, v := range vars {
		keys = append(keys, v.Key)
	}
	want := []string{"ALPHA", "MID", "ZETA"}
	if len(keys) != len(want) {
		t.Fatalf("expected %d declared variables, got %d: %v", len(want), len(keys), keys)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("Declared must return variables in key order regardless of the order they were read, so a manifest reads the same however the composition root happened to call getters; got %v want %v", keys, want)
			break
		}
	}
}

func TestDeclared_SetReflectsUnderlyingPresence(t *testing.T) {
	r := newReader(t, map[string]string{"EMPTY_BUT_SET": ""})
	_ = r.String("EMPTY_BUT_SET", "fallback")
	_ = r.String("TRULY_ABSENT", "fallback")

	byKey := declaredByKey(t, r)

	if v, ok := byKey["EMPTY_BUT_SET"]; !ok || !v.Set {
		t.Errorf("a variable the operator set to an empty string was present in the environment and must be recorded as Set=true, distinguishing it from one never set at all; got %+v (ok=%v)", v, ok)
	}
	if v, ok := byKey["TRULY_ABSENT"]; !ok || v.Set {
		t.Errorf("a variable never present in the environment must be recorded as Set=false even though a fallback was used; got %+v (ok=%v)", v, ok)
	}
}

func TestDeclared_DefaultRecordsTheConfiguredFallback(t *testing.T) {
	r := newReader(t, map[string]string{})
	_ = r.String("LOG_LEVEL", "info")
	_ = r.Enum("MODE", "dev", "dev", "prod")
	_ = r.Int("PORT", 8080)
	_ = r.Required("MUST_HAVE") // no fallback concept

	byKey := declaredByKey(t, r)

	if got := byKey["LOG_LEVEL"].Default; got != "info" {
		t.Errorf("Var.Default must record the fallback passed to String(), got %q want %q", got, "info")
	}
	if got := byKey["MODE"].Default; got != "dev" {
		t.Errorf("Var.Default must record the fallback passed to Enum(), got %q want %q", got, "dev")
	}
	if got := byKey["PORT"].Default; got != "8080" {
		// INFERENCE: the doc does not state how a numeric fallback is turned into
		// the string-typed Var.Default field. Assuming ordinary decimal string form.
		t.Errorf("INFERENCE: expected Int's numeric fallback recorded as its decimal string form in Var.Default so the manifest can render it, got %q", got)
	}
	if got := byKey["MUST_HAVE"].Default; got != "" {
		// INFERENCE: Required has no fallback parameter; assuming Var.Default is
		// left at its zero value ("") for getters that do not take one.
		t.Errorf("INFERENCE: Required has no fallback concept; expected Var.Default to be the zero value \"\" for it, got %q", got)
	}
}

func TestDeclared_ValueReflectsWhatWasResolved(t *testing.T) {
	r := newReader(t, map[string]string{"PORT_SET": "9090"})
	_ = r.Int("PORT_SET", 8080)
	_ = r.Int("PORT_ABSENT", 8080)

	byKey := declaredByKey(t, r)

	if got := byKey["PORT_SET"].Value; got != "9090" {
		t.Errorf("a variable that was set must have its resolved value recorded verbatim in the manifest, got %q", got)
	}
	if got := byKey["PORT_ABSENT"].Value; got != "8080" {
		// INFERENCE: the doc does not state how a numeric fallback is stringified
		// into Var.Value when a variable is absent; assuming decimal string form,
		// consistent with the same assumption made for Var.Default.
		t.Errorf("INFERENCE: expected the fallback's decimal string form recorded as Value when absent, got %q", got)
	}
}

func TestDeclared_SecretValueIsAlwaysRedacted(t *testing.T) {
	const raw = "sk-supersecretvalue-123"
	r := newReader(t, map[string]string{"API_KEY": raw})
	_ = r.Secret("API_KEY")

	byKey := declaredByKey(t, r)
	v, ok := byKey["API_KEY"]
	if !ok {
		t.Fatalf("Secret(%q) was read but does not appear in Declared() — the boot manifest must record every variable that was read", "API_KEY")
	}
	if !v.Secret {
		t.Errorf("a variable read through Reader.Secret must be marked Secret=true in the manifest so a logger knows to treat it carefully, got %+v", v)
	}
	if v.Value == raw {
		t.Fatalf("Var.Value for a secret must never contain the raw credential — the whole point of a boot manifest is that it is safe to log whole, and this leaked %q", v.Value)
	}
	if strings.Contains(v.Value, raw) {
		t.Fatalf("Var.Value for a secret must not embed the raw credential as a substring, got %q", v.Value)
	}
	if v.Value != secret.Redacted {
		t.Errorf("Var.Value for a secret is documented as \"already redacted\"; expected the package's own Redacted constant %q, got %q", secret.Redacted, v.Value)
	}
}

// AMENDED 2026-09-05 after the spec was made explicit. The original asserted the
// opposite — that a failed read still appears in Declared — which the doc did not
// say either way at the time it was written. It now says the two are disjoint, so
// this pins the invariant that decision buys: Var.Set false means "defaulted" and
// can never also mean "failed".
func TestDeclared_ExcludesAKeyThatFailed(t *testing.T) {
	r := newReader(t, map[string]string{})
	_ = r.Secret("API_KEY")

	if _, ok := declaredByKey(t, r)["API_KEY"]; ok {
		t.Errorf("a key that produced a problem must not appear in Declared() — the manifest and Err are disjoint, so a reader of the manifest never has to cross-reference Err to learn whether Set=false meant defaulted or failed")
	}
	if fields := errors.FieldsOf(r.Err()); fields["API_KEY"] == "" {
		t.Errorf("a key excluded from the manifest must be accounted for in Err's Fields, or the failure is recorded nowhere; got %v", fields)
	}
}

func TestDeclared_OnlySecretGettersAreMarkedSecret(t *testing.T) {
	r := newReader(t, map[string]string{"LOG_LEVEL": "info"})
	_ = r.String("LOG_LEVEL", "warn")

	byKey := declaredByKey(t, r)
	if v := byKey["LOG_LEVEL"]; v.Secret {
		t.Errorf("a plain String() read must not be marked Secret — that flag tells a logger which values are unsafe to print, and marking a non-secret would either desensitize the flag or hide a real value from the manifest")
	}
}
