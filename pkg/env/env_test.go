// AUTHOR-WRITTEN. Not produced behind an information barrier: written by someone
// who had read the implementation, and in most cases the surviving mutants that
// exposed the gap. Separate from spec_test.go so provenance is a property of the
// file rather than of a comment block somebody has to notice.
//
// custody/ records which run each of these came from.
package env_test

import (
	"strings"
	"testing"

	"github.com/0xsj/overwatch-backend/pkg/env"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

// ---------------------------------------------------------------------
// POST-HOC. The three tests below were NOT written behind the information
// barrier: they were added after mutation testing, by an author who had seen
// the implementation and the surviving mutants. Recorded separately because
// their provenance differs from every test above, and custody 0002 says so.
// ---------------------------------------------------------------------

func TestEnum_PanicsWhenTheCallersOwnFallbackIsNotAllowed(t *testing.T) {
	// The message names the fallback, so asserting on it distinguishes this
	// guard from any other panic the getter might grow.
	wantPanic(t, "trace", func() {
		r := newReader(t, map[string]string{})
		_ = r.Enum("LOG_LEVEL", "trace", "debug", "info", "warn", "error")
	})
}

func TestEnum_DoesNotPanicWhenTheFallbackIsAllowed(t *testing.T) {
	defer func() {
		if v := recover(); v != nil {
			t.Errorf("a fallback inside the allowed set is ordinary use and must not panic, got %v", v)
		}
	}()
	r := newReader(t, map[string]string{})
	if got := r.Enum("LOG_LEVEL", "info", "debug", "info", "warn", "error"); got != "info" {
		t.Errorf("an absent key must yield the fallback, got %q", got)
	}
}

func TestDeclared_ReturnsACopy(t *testing.T) {
	r := newReader(t, map[string]string{"B_KEY": "b", "A_KEY": "a"})
	_ = r.Required("B_KEY")
	_ = r.Required("A_KEY")

	first := r.Declared()
	if len(first) != 2 {
		t.Fatalf("expected two resolved reads, got %d", len(first))
	}
	first[0] = env.Var{Key: "OVERWRITTEN"}

	second := r.Declared()
	if second[0].Key == "OVERWRITTEN" {
		t.Errorf("mutating the slice returned by Declared() changed the Reader's own manifest — a caller handed a reference into a Reader's internals can reorder or overwrite it from the outside, which is why errors.FieldsOf copies too")
	}
	if second[0].Key != "A_KEY" {
		t.Errorf("the manifest must still be sorted lexicographically after an outside mutation, got %q first", second[0].Key)
	}
}

// POST-HOC, 2026-09-06. Var.String was added after the barrier-written suite
// existed. custody/0005 records why that matters: new API added after a suite
// is untested by construction, and coverage does not notice because the old
// tests still execute every line they always did.
func TestVarStringRendersTheManifestLine(t *testing.T) {
	tests := []struct {
		name string
		v    env.Var
		want string
	}{
		{"a value from the environment", env.Var{Key: "PORT_SERVER", Value: "7000", Set: true}, "PORT_SERVER=7000"},
		{"a value that came from a fallback says so", env.Var{Key: "LOG_LEVEL", Value: "info", Default: "info"}, `LOG_LEVEL=info (default)`},
		{"a secret shows its redaction, never its value", env.Var{Key: "DATABASE_URL", Value: secret.Redacted, Set: true, Secret: true}, "DATABASE_URL=" + secret.Redacted},
		{"an empty value is quoted so it is visibly empty", env.Var{Key: "PREFIX", Value: "", Set: true}, `PREFIX=""`},
		{"a value with a space is quoted so the line stays one field", env.Var{Key: "NOTE", Value: "two words", Set: true}, `NOTE="two words"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.v.String(); got != tt.want {
				t.Errorf("Var.String() = %q, want %q — the manifest is read by an operator asking what the process is running on, and an ambiguous line is a worse answer than no line", got, tt.want)
			}
		})
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
