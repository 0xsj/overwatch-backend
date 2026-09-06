// Package secret_test is an external, specification-driven test suite for
// pkg/secret. It was written without sight of the implementation, from the
// package doc comment alone. Every assertion traces to a specific claim in
// that prose; places where the prose does not settle a question are marked
// INFERENCE at the point where a guess was still made, or left untested with
// a note in the accompanying report.
package secret_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/0xsj/overwatch-backend/pkg/secret"
)

// ---------------------------------------------------------------------
// Reveal is the single documented exit. Everything else must round-trip
// nothing.
// ---------------------------------------------------------------------

func TestReveal_RoundTrip(t *testing.T) {
	cases := []string{
		"",
		"simple",
		"with spaces and\ttabs",
		"unicode-résumé-🔒",
		"line one\nline two",
		secret.Redacted,
		"a very long value " + strings.Repeat("x", 500),
	}
	for _, v := range cases {
		t.Run(fmt.Sprintf("round-trips %q", v), func(t *testing.T) {
			if got := secret.New(v).Reveal(); got != v {
				t.Errorf("Reveal must return exactly what New was given — it is documented as the single exit and must not transform the value; got %q want %q", got, v)
			}
		})
	}
}

func TestZeroValue_IsZeroAndRevealsEmpty(t *testing.T) {
	var zero secret.String
	if !zero.IsZero() {
		t.Errorf("the zero value of String (declared with var, no call to New) must report IsZero()==true")
	}
	if got := zero.Reveal(); got != "" {
		t.Errorf("the zero value must reveal the empty string, not panic or fabricate a value, got %q", got)
	}
	if !secret.New("").IsZero() {
		t.Errorf("New(\"\") must be indistinguishable from the zero value; both represent an empty credential")
	}
	if secret.New("x").IsZero() {
		t.Errorf("a non-empty credential must not report IsZero()==true")
	}
}

func TestReveal_IsTheOnlyMethodThatReturnsTheRawValue(t *testing.T) {
	const raw = "the-actual-credential-9f2c"
	s := secret.New(raw)

	if got := s.Reveal(); got != raw {
		t.Fatalf("Reveal must return exactly the value given to New, got %q want %q — it is documented as the single exit and must not itself be lossy", got, raw)
	}

	outputs := map[string]string{
		"String":       s.String(),
		"GoString":     s.GoString(),
		"Sprintf(%v)":  fmt.Sprintf("%v", s),
		"Sprintf(%s)":  fmt.Sprintf("%s", s),
		"Sprintf(%+v)": fmt.Sprintf("%+v", s),
	}
	for name, out := range outputs {
		if strings.Contains(out, raw) {
			t.Errorf("%s leaked the raw credential (found in %q) — Reveal is documented as the only way out, so grepping for Reveal alone must be enough to find every place a credential becomes an ordinary string", name, out)
		}
	}

	mt, err := s.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText must not error for an ordinary value, got %v", err)
	}
	if strings.Contains(string(mt), raw) {
		t.Errorf("MarshalText leaked the raw credential, defeating every encoding/json (and other encoding/*) text path")
	}

	if strings.Contains(s.LogValue().String(), raw) {
		t.Errorf("LogValue leaked the raw credential, defeating structured logging")
	}
}

// ---------------------------------------------------------------------
// Each formatting path redacts on its own — the doc is explicit that
// redaction "has to be installed once per formatting path" because the
// standard library's formatters do not agree on which interface to consult.
// ---------------------------------------------------------------------

func TestString_AlwaysRedactsRegardlessOfValue(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"an ordinary credential", "hunter2"},
		{"an empty credential", ""},
		{"a credential that happens to equal the redaction text itself", secret.Redacted},
		{"a credential containing the redaction text as a substring", "prefix-" + secret.Redacted + "-suffix"},
		{"a multi-line credential", "line one\nline two"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := secret.New(tt.value)
			if got := s.String(); got != secret.Redacted {
				t.Errorf("String() must always render as the package's Redacted constant so a credential is safe to print unconditionally, got %q want %q", got, secret.Redacted)
			}
		})
	}
}

func TestGoString_NeverLeaksTheValue(t *testing.T) {
	const raw = "hunter2-do-not-print-me"
	got := secret.New(raw).GoString()
	if strings.Contains(got, raw) {
		t.Fatalf("GoString (the %%#v path when invoked directly, outside fmt's Formatter dispatch) must not leak the raw value, got %q", got)
	}
	// INFERENCE not made: the doc does not state GoString's exact literal
	// output, only that redaction "has to be installed once per formatting
	// path" including this one. Only non-leakage is asserted; the exact
	// string is intentionally left unpinned.
}

func TestMarshalText_NeverLeaksAndSupportsJSON(t *testing.T) {
	const raw = "hunter2-json-path"
	s := secret.New(raw)

	b, err := s.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText must not error for an ordinary value, got %v", err)
	}
	if strings.Contains(string(b), raw) {
		t.Fatalf("MarshalText leaked the raw value: %q", b)
	}

	type payload struct {
		Password secret.String `json:"password"`
	}
	out, err := json.Marshal(payload{Password: s})
	if err != nil {
		t.Fatalf("marshaling a struct containing a credential must not error, got %v", err)
	}
	if strings.Contains(string(out), raw) {
		t.Fatalf("encoding/json leaked the raw credential when marshaling a struct field — this is exactly the leak the package doc calls out: json does not consult fmt.Stringer, so MarshalText is required to close it; got %s", out)
	}
	if !strings.Contains(string(out), secret.Redacted) {
		t.Errorf("expected the JSON output to contain the Redacted constant so a reader can tell a credential was present but withheld, got %s", out)
	}
}

func TestFormat_CoversEveryVerbWithoutLeaking(t *testing.T) {
	const raw = "hunter2-format-path"
	s := secret.New(raw)
	verbs := []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%10s", "%-10s", "%.3s"}
	for _, verb := range verbs {
		t.Run(verb, func(t *testing.T) {
			got := fmt.Sprintf(verb, s)
			if strings.Contains(got, raw) {
				t.Errorf("fmt.Sprintf(%q, s) leaked the raw credential as %q — the doc states Format \"covers every verb\" precisely so no verb is an escape hatch", verb, got)
			}
			// STRENGTHENED 2026-09-05. Absence of the secret is not sufficient:
			// fmt recovers a panic inside a Format method and substitutes
			// "%!v(PANIC=...)", which contains no credential, so the check above
			// alone passed against an unimplemented Format and asserted nothing.
			if !strings.Contains(got, secret.Redacted) {
				t.Errorf("fmt.Sprintf(%q, s) = %q, which does not contain %q — a verb must render the redaction, not merely fail to render the secret", verb, got, secret.Redacted)
			}
		})
	}
}

// ---------------------------------------------------------------------
// The doc's own worked comparison: LogValue alone leaks under slog's
// JSONHandler because encoding/json ignores fmt.Stringer; adding MarshalText
// closes it for both handlers. This type has both — verify the closed state
// directly, in both handlers, rather than trusting the general claim.
// ---------------------------------------------------------------------

func TestLogValue_SafeInBothSlogHandlers(t *testing.T) {
	const raw = "hunter2-slog-path"

	t.Run("JSONHandler", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf, nil))
		logger.Info("login attempt", "password", secret.New(raw))
		out := buf.String()
		if strings.Contains(out, raw) {
			t.Fatalf("slog's JSONHandler leaked the raw credential — the package doc calls this out explicitly: LogValue alone leaks under JSONHandler because encoding/json does not consult fmt.Stringer, and MarshalText is required to close it. got %s", out)
		}
		if !strings.Contains(out, secret.Redacted) {
			t.Errorf("expected the JSON log line to show the Redacted constant in place of the credential, got %s", out)
		}
	})

	t.Run("TextHandler", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))
		logger.Info("login attempt", "password", secret.New(raw))
		out := buf.String()
		if strings.Contains(out, raw) {
			t.Fatalf("slog's TextHandler leaked the raw credential, got %s", out)
		}
		if !strings.Contains(out, secret.Redacted) {
			t.Errorf("expected the text log line to show the Redacted constant in place of the credential, got %s", out)
		}
	})
}

// ---------------------------------------------------------------------
// A redacted output must carry no information about the value it is
// standing in for — not the value, not its length, not its shape.
// ---------------------------------------------------------------------

func TestRedactedOutputsAreIndependentOfTheUnderlyingValue(t *testing.T) {
	a := secret.New("value-one")
	b := secret.New("a-totally-different-and-much-longer-value-two")

	if a.String() != b.String() {
		t.Errorf("String() must reveal nothing about the underlying value, not even its length or shape — two different secrets produced different output: %q vs %q", a.String(), b.String())
	}

	ta, errA := a.MarshalText()
	tb, errB := b.MarshalText()
	if errA != nil || errB != nil {
		t.Fatalf("MarshalText must not error on ordinary values, got %v / %v", errA, errB)
	}
	if string(ta) != string(tb) {
		t.Errorf("MarshalText() must not vary with the underlying value, got %q vs %q", ta, tb)
	}

	if a.LogValue().String() != b.LogValue().String() {
		t.Errorf("LogValue() must not vary with the underlying value, got %q vs %q", a.LogValue().String(), b.LogValue().String())
	}
}
