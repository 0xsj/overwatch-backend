package logger_test

// Test suite written against the pkg/logger specification (go doc -all
// ./pkg/logger) without sight of the implementation. Where a test encodes a
// choice the specification does not make explicit, that is marked inline as
// an inference rather than asserted silently.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	pkgerrors "github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/logger"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

// fixedClock is a test double for logger.Clock. The package deliberately
// does not import any concrete clock implementation (the doc: "neither is
// imported"), so a caller — including a test — must supply its own.
type fixedClock struct{ t time.Time }

func (f fixedClock) Now() time.Time { return f.t }

// ctxKey is a private context key used only to prove that WithContext's
// ContextAttrs closure receives the same context Handle was given.
type ctxKey struct{}

// decodeJSONLines parses a FormatJSON buffer as newline-delimited JSON
// objects, failing the test if any line is not valid JSON — FormatJSON is
// documented as "one object per line".
func decodeJSONLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	trimmed := strings.TrimRight(buf.String(), "\n")
	if trimmed == "" {
		return nil
	}
	lines := strings.Split(trimmed, "\n")
	out := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("FormatJSON promises one valid JSON object per line; line %q did not parse: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

// toStr gives a stable, order-independent textual form of a decoded JSON
// value for equality checks (encoding/json sorts map keys when marshaling).
func toStr(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// withEnv sets or unsets an environment variable for the duration of a test
// and restores whatever was there before, regardless of prior state.
func withEnv(t *testing.T, key, value string, set bool) {
	t.Helper()
	old, existed := os.LookupEnv(key)
	if set {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("failed to set %s for test setup: %v", key, err)
		}
	} else {
		os.Unsetenv(key)
	}
	t.Cleanup(func() {
		if existed {
			os.Setenv(key, old)
		} else {
			os.Unsetenv(key)
		}
	})
}

// --- zero values / totality over closed enums -----------------------------

func TestFormat_ZeroValueIsConsole(t *testing.T) {
	var f logger.Format
	if f != logger.FormatConsole {
		t.Errorf("FormatConsole is documented as the zero value so a fresh clone defaults to readable output at `make dev`; the zero Format is not FormatConsole")
	}
}

func TestColorMode_ZeroValueIsAuto(t *testing.T) {
	var c logger.ColorMode
	if c != logger.ColorAuto {
		t.Errorf("ColorAuto is documented as the zero value so an unconfigured Color field behaves correctly unattended; the zero ColorMode is not ColorAuto")
	}
}

// --- documented constants (example test: values are given verbatim) -------

func TestFieldConstants_MatchDocumentedNames(t *testing.T) {
	if logger.FieldError != "error" {
		t.Errorf("FieldError is documented as %q, got %q; a component matching on the literal from the doc would silently stop matching", "error", logger.FieldError)
	}
	if logger.FieldErrKind != "err_kind" {
		t.Errorf("FieldErrKind is documented as %q, got %q; a component matching on the literal from the doc would silently stop matching", "err_kind", logger.FieldErrKind)
	}
}

// --- ParseLevel -------------------------------------------------------------

func TestParseLevel_RoundTripsEveryNamedLevel(t *testing.T) {
	levels := []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
	for _, lvl := range levels {
		name := lvl.String()
		t.Run("ParseLevel undoes Level.String for "+name, func(t *testing.T) {
			got, err := logger.ParseLevel(name)
			if err != nil {
				t.Fatalf("ParseLevel must accept every name slog.Level itself renders; ParseLevel(%q) returned error: %v", name, err)
			}
			if got != lvl {
				t.Errorf("ParseLevel(%q) = %v, want %v; a level's own rendered name must map back to itself", name, got, lvl)
			}
		})
	}
}

func TestParseLevel_FailsClosedOnUnknownInput(t *testing.T) {
	_, err := logger.ParseLevel("not-a-real-level-name")
	if err == nil {
		t.Error("an unrecognised level string must be rejected rather than silently coerced to some default level; ParseLevel returned no error for garbage input")
	}
}

// --- Nop ---------------------------------------------------------------------

func TestNop_NeverPanics(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Nop exists so a component constructed without a logger has something safe to call; it panicked: %v", r)
		}
	}()
	log := logger.Nop()
	if log == nil {
		t.Fatal("Nop must return a usable *slog.Logger, not nil; a nil *slog.Logger panics on use, which is exactly what Nop exists to avoid")
	}
	log.Debug("discarded")
	log.Info("discarded")
	log.Warn("discarded")
	log.Error("discarded", "cause", pkgerrors.New(pkgerrors.Internal, "x"))
}

// --- format shape ------------------------------------------------------------

func TestNew_FormatJSON_OneObjectPerLine(t *testing.T) {
	buf := &bytes.Buffer{}
	log := logger.New(logger.Config{Output: buf, Format: logger.FormatJSON, Level: slog.LevelDebug, Clock: fixedClock{time.Now()}})
	log.Info("first")
	log.Info("second")
	log.Info("third")

	lines := decodeJSONLines(t, buf)
	if len(lines) != 3 {
		t.Fatalf("FormatJSON promises one JSON object per line for three log calls; got %d parseable lines from output %q", len(lines), buf.String())
	}
}

func TestNew_FormatConsole_IsHumanTextNotJSON(t *testing.T) {
	buf := &bytes.Buffer{}
	log := logger.New(logger.Config{Output: buf, Format: logger.FormatConsole, Level: slog.LevelDebug, Clock: fixedClock{time.Now()}})
	log.Info("hello", "key", "value")

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err == nil {
		t.Errorf("FormatConsole exists specifically because JSON in a terminal is unreadable; output parsed as JSON anyway: %q", buf.String())
	}
}

// --- level filtering -----------------------------------------------------

func TestNew_LevelFiltering_DropsRecordsBelowConfiguredLevel(t *testing.T) {
	buf := &bytes.Buffer{}
	log := logger.New(logger.Config{Output: buf, Format: logger.FormatJSON, Level: slog.LevelWarn, Clock: fixedClock{time.Now()}})

	log.Info("must not appear")
	if buf.Len() != 0 {
		t.Fatalf("a level below the configured minimum must produce no output, or a WARN-configured logger floods its channel with INFO noise; got %q", buf.String())
	}

	log.Warn("must appear")
	if buf.Len() == 0 {
		t.Fatal("a level at or above the configured minimum must be emitted; a WARN-configured logger silently dropped a WARN record")
	}
}

// --- the error-to-two-fields contract ---------------------------------------
// This is the richest promise in the spec: the caller's key is replaced
// wherever an error is logged, the message is the full chain, and a kind is
// attached derived from Kind.String() rather than any literal.

func TestNew_ErrorBecomesTwoFields(t *testing.T) {
	kinds := []struct {
		name string
		kind pkgerrors.Kind
	}{
		{"invalid", pkgerrors.Invalid},
		{"not found", pkgerrors.NotFound},
		{"unavailable", pkgerrors.Unavailable},
		{"internal", pkgerrors.Internal},
	}
	// Note: the spec says kind constants "including" these four, implying
	// the set may be larger. This table covers only the kinds named in the
	// spec, not the full closed set of pkgerrors.Kind.
	for _, tc := range kinds {
		t.Run("kind "+tc.name+": an error logged under any caller-chosen key surfaces as "+logger.FieldError+" and "+logger.FieldErrKind, func(t *testing.T) {
			buf := &bytes.Buffer{}
			log := logger.New(logger.Config{Output: buf, Format: logger.FormatJSON, Level: slog.LevelDebug, Clock: fixedClock{time.Now()}})
			err := pkgerrors.New(tc.kind, "boom")
			log.Error("operation failed", "whatever_key_the_caller_chose", err)

			lines := decodeJSONLines(t, buf)
			if len(lines) != 1 {
				t.Fatalf("expected exactly one log line, got %d", len(lines))
			}
			m := lines[0]

			if _, present := m["whatever_key_the_caller_chose"]; present {
				t.Error("the caller's original key must be replaced so error fields are queryable under one name regardless of what a caller calls them; the original key is still present")
			}
			got, ok := m[logger.FieldError]
			if !ok {
				t.Fatalf("an error argument must always surface under %q so every component's errors are queryable the same way; field is missing", logger.FieldError)
			}
			if got != err.Error() {
				t.Errorf("the logged error message must be the full chain (err.Error()), not a caller-safe summary; got %q, want %q", got, err.Error())
			}
			gotKind, ok := m[logger.FieldErrKind]
			if !ok {
				t.Fatalf("err_kind is what makes an error line queryable by failure category ('the one that earns the package'); field is missing for kind %s", tc.kind.String())
			}
			if gotKind != tc.kind.String() {
				t.Errorf("err_kind must render exactly what Kind.String() produces, so one component logging a kind never drifts from another; got %q, want %q", gotKind, tc.kind.String())
			}
		})
	}
}

func TestNew_ErrorFieldReplacement_PreservesOtherAttrsAndIsOrderIndependent(t *testing.T) {
	err := pkgerrors.New(pkgerrors.Internal, "boom")
	orderings := []struct {
		name string
		args []any
	}{
		{"error argument last", []any{"user", "alice", "cause", err}},
		{"error argument first", []any{"cause", err, "user", "alice"}},
	}
	for _, tc := range orderings {
		t.Run("position of the error arg ("+tc.name+") does not change the outcome", func(t *testing.T) {
			buf := &bytes.Buffer{}
			log := logger.New(logger.Config{Output: buf, Format: logger.FormatJSON, Level: slog.LevelDebug, Clock: fixedClock{time.Now()}})
			log.Error("op failed", tc.args...)

			lines := decodeJSONLines(t, buf)
			if len(lines) != 1 {
				t.Fatalf("expected exactly one line, got %d", len(lines))
			}
			m := lines[0]

			if m["user"] != "alice" {
				t.Errorf("an unrelated attribute logged alongside an error must survive the error-to-two-fields transformation unchanged; got user=%v", m["user"])
			}
			if _, present := m["cause"]; present {
				t.Errorf("the caller's key (%q) must be replaced by %q wherever an error is logged, regardless of argument position", "cause", logger.FieldError)
			}
			if m[logger.FieldError] != err.Error() {
				t.Errorf("expected %s=%q, got %v", logger.FieldError, err.Error(), m[logger.FieldError])
			}
			if m[logger.FieldErrKind] != pkgerrors.Internal.String() {
				t.Errorf("expected %s=%q, got %v", logger.FieldErrKind, pkgerrors.Internal.String(), m[logger.FieldErrKind])
			}
		})
	}
}

func TestNew_ErrorMessage_IsTheFullChainNotJustTheTopMessage(t *testing.T) {
	inner := pkgerrors.New(pkgerrors.NotFound, "user missing")
	outer := pkgerrors.Wrap(inner, pkgerrors.Unavailable, "lookup failed")

	buf := &bytes.Buffer{}
	log := logger.New(logger.Config{Output: buf, Format: logger.FormatJSON, Level: slog.LevelDebug, Clock: fixedClock{time.Now()}})
	log.Error("op failed", "cause", outer)

	lines := decodeJSONLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("expected exactly one line, got %d", len(lines))
	}
	m := lines[0]

	got, _ := m[logger.FieldError].(string)
	if got != outer.Error() {
		t.Errorf("the logged message must be the full chain (err.Error()), not a caller-safe summary; got %q, want %q", got, outer.Error())
	}
	if got == "lookup failed" {
		t.Errorf("a log is where the full cause belongs, distinct from a caller-safe form ('not errors.Message'); the logged error collapsed to just the top-level message %q, losing the wrapped cause", got)
	}
	if !strings.Contains(got, "user missing") {
		t.Errorf("the full chain must include the wrapped cause's message so a log carries everything; %q is missing from logged error %q", "user missing", got)
	}
	// Inference: the spec's single worked example is a one-level wrap, so
	// which level's Kind should win in a nested wrap is not stated
	// explicitly. Asserted here as the outermost Wrap's kind, since that is
	// the kind under which the caller is choosing to log.
	if m[logger.FieldErrKind] != pkgerrors.Unavailable.String() {
		t.Errorf("err_kind must reflect the outermost wrap's kind (inferred); got %v, want %q", m[logger.FieldErrKind], pkgerrors.Unavailable.String())
	}
}

// --- secrets must never leak, in either format ------------------------------

func TestNew_SecretNeverAppearsInOutput(t *testing.T) {
	const rawValue = "sekret-value-do-not-print-9f3a"
	formats := []struct {
		name string
		fmt  logger.Format
	}{
		{"json", logger.FormatJSON},
		{"console", logger.FormatConsole},
	}
	for _, tc := range formats {
		t.Run(tc.name+" format never prints a secret.String value", func(t *testing.T) {
			buf := &bytes.Buffer{}
			log := logger.New(logger.Config{Output: buf, Format: tc.fmt, Level: slog.LevelDebug, Clock: fixedClock{time.Now()}})
			log.Info("logging in", "password", secret.New(rawValue))
			out := buf.String()

			if strings.Contains(out, rawValue) {
				t.Errorf("a credential logged as an attribute must never appear in log output regardless of handler; found the raw secret in %s output: %q", tc.name, out)
			}
			if !strings.Contains(out, string(secret.Redacted)) {
				t.Errorf("secret.String is documented to refuse printing through slog.LogValuer, substituting %q; that placeholder is absent from %s output %q", string(secret.Redacted), tc.name, out)
			}
		})
	}
}

// --- the clock seam must be applied twice -----------------------------------

func TestNew_TimestampReflectsInjectedClockNotRealTime(t *testing.T) {
	fixed := time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC)
	formats := []struct {
		name string
		fmt  logger.Format
	}{
		{"json", logger.FormatJSON},
		{"console", logger.FormatConsole},
	}
	for _, tc := range formats {
		t.Run(tc.name+" format stamps records with the injected clock, not the real wall clock", func(t *testing.T) {
			buf := &bytes.Buffer{}
			log := logger.New(logger.Config{Output: buf, Format: tc.fmt, Level: slog.LevelDebug, Clock: fixedClock{fixed}})
			log.Info("tick")
			// AMENDED 2026-09-06. The original looked for "1999" in both formats.
			// Console renders time only — 15:04:05.000, no date — so no year can
			// appear, and the doc had never said so. The spec now states both
			// renderings; this asserts against each.
			out := buf.String()
			want := fixed.Format("15:04:05.000")
			if tc.fmt == logger.FormatJSON {
				want = "1999"
			}
			if !strings.Contains(out, want) {
				t.Errorf("slog stamps a record with time.Now() inside the standard library where an injected clock cannot reach; this package must re-stamp using Config.Clock, but %q is absent from %s output: %q", want, tc.name, out)
			}
			if strings.Contains(out, time.Now().Format("15:04:05")) {
				t.Errorf("%s output carries the real wall clock rather than the injected one: %q", tc.name, out)
			}
		})
	}
}

// --- colour ------------------------------------------------------------------

func TestNew_ColorAlways_EmitsEscapeSequencesInConsoleOutput(t *testing.T) {
	// Inference: the spec states colour is on or off but never names the
	// escape mechanism. ANSI CSI ("\x1b[") is the universal terminal colour
	// convention and is assumed here.
	buf := &bytes.Buffer{}
	log := logger.New(logger.Config{Output: buf, Format: logger.FormatConsole, Color: logger.ColorAlways, Level: slog.LevelDebug, Clock: fixedClock{time.Now()}})
	log.Info("colourful")
	if !strings.Contains(buf.String(), "\x1b[") {
		t.Errorf("ColorAlways is documented to force colour on; no ANSI escape sequence found in console output %q (escape convention inferred, see test comment)", buf.String())
	}
}

func TestNew_ColorNever_NeverEmitsEscapeSequences(t *testing.T) {
	buf := &bytes.Buffer{}
	log := logger.New(logger.Config{Output: buf, Format: logger.FormatConsole, Color: logger.ColorNever, Level: slog.LevelDebug, Clock: fixedClock{time.Now()}})
	log.Info("plain")
	if strings.Contains(buf.String(), "\x1b[") {
		t.Errorf("ColorNever must suppress colour unconditionally; found an ANSI escape sequence in output %q", buf.String())
	}
}

func TestNew_ColorAuto_NeverEmitsEscapesAgainstANonTerminalBuffer(t *testing.T) {
	cases := []struct {
		name string
		set  bool
		val  string
	}{
		{"NO_COLOR unset", false, ""},
		{"NO_COLOR set and non-empty", true, "1"},
	}
	for _, tc := range cases {
		t.Run("ColorAuto against a bytes.Buffer produces no escapes when "+tc.name, func(t *testing.T) {
			withEnv(t, "NO_COLOR", tc.val, tc.set)
			buf := &bytes.Buffer{}
			log := logger.New(logger.Config{Output: buf, Format: logger.FormatConsole, Color: logger.ColorAuto, Level: slog.LevelDebug, Clock: fixedClock{time.Now()}})
			log.Info("auto")
			if strings.Contains(buf.String(), "\x1b[") {
				t.Errorf("ColorAuto must resolve to off when the output is not a terminal, whatever NO_COLOR is set to; a bytes.Buffer is never a terminal but output contained an escape sequence: %q", buf.String())
			}
		})
	}
	// Not tested: that an empty-but-present NO_COLOR fails to disable colour
	// when the output IS a terminal. That distinction only matters against a
	// real terminal, which this test environment cannot construct (per the
	// spec's own instruction not to test isTerminal against a real one).
}

// --- source location ---------------------------------------------------------

func TestNew_Source_AddsSourceLocationWhenEnabled(t *testing.T) {
	// Inference: the exact key/shape of the source field is not specified
	// beyond "Source bool". Checked here via a ".go:" substring, which any
	// Go file:line reference contains, rather than an assumed key name.
	formats := []struct {
		name string
		fmt  logger.Format
	}{
		{"json", logger.FormatJSON},
		{"console", logger.FormatConsole},
	}
	for _, tc := range formats {
		t.Run(tc.name+": Source true includes a file:line reference, Source false omits it", func(t *testing.T) {
			on := &bytes.Buffer{}
			off := &bytes.Buffer{}
			logOn := logger.New(logger.Config{Output: on, Format: tc.fmt, Source: true, Level: slog.LevelDebug, Clock: fixedClock{time.Now()}})
			logOff := logger.New(logger.Config{Output: off, Format: tc.fmt, Source: false, Level: slog.LevelDebug, Clock: fixedClock{time.Now()}})
			logOn.Info("with source")
			logOff.Info("without source")

			// AMENDED 2026-09-06. The original looked for ".go:" in both formats.
			// JSON carries slog's own source group — "file":"…/x.go","line":42 —
			// where the extension and the line number are separate fields, so the
			// substring never appears. Console renders base.go:line. The doc now
			// says both, and says a caller must assert per format.
			marker := ".go:"
			if tc.fmt == logger.FormatJSON {
				marker = `"line":`
			}
			if !strings.Contains(on.String(), marker) {
				t.Errorf("Config.Source=true adds the caller's location; no %q found in %s output %q", marker, tc.name, on.String())
			}
			if strings.Contains(off.String(), marker) {
				t.Errorf("Config.Source=false must not add caller location, but found %q in %s output %q", marker, tc.name, off.String())
			}
		})
	}
}

// --- Config.Context left unset (inference) ----------------------------------

func TestNew_UnsetContextAttrs_DoesNotPanic(t *testing.T) {
	// Inference: the spec confirms a ContextAttrs FUNC that returns nil is
	// normal, but never states that leaving Config.Context itself as the
	// zero value (a nil func) is safe. Treated here as intended, since most
	// simple loggers have no request-scoped fields to contribute.
	buf := &bytes.Buffer{}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("a Config with no Context set is expected to work for a logger with no request-scoped fields (inferred); logging panicked: %v", r)
		}
	}()
	log := logger.New(logger.Config{Output: buf, Format: logger.FormatJSON, Level: slog.LevelDebug, Clock: fixedClock{time.Now()}})
	log.InfoContext(context.Background(), "no context configured")
}

// --- WithContext -------------------------------------------------------------

func TestWithContext_NilAttrsFuncReturningNilIsNotAnError(t *testing.T) {
	buf := &bytes.Buffer{}
	wrapped := logger.WithContext(slog.NewJSONHandler(buf, nil), func(ctx context.Context) []slog.Attr { return nil })
	l := slog.New(wrapped)
	l.InfoContext(context.Background(), "no request scope", "k", "v")

	lines := decodeJSONLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("expected exactly one line, got %d", len(lines))
	}
	m := lines[0]
	if m["k"] != "v" {
		t.Errorf("a ContextAttrs returning nil is documented as normal, not an error; it must not interfere with the record's own attributes, got k=%v", m["k"])
	}
}

func TestWithContext_AddsAttrsWithoutRemovingWhatTheBaseHandlerWouldEmit(t *testing.T) {
	baseBuf := &bytes.Buffer{}
	wrappedBuf := &bytes.Buffer{}

	baseHandler := slog.NewJSONHandler(baseBuf, nil)
	l := slog.New(baseHandler)
	l.InfoContext(context.Background(), "event", "k", "v")

	wrapped := logger.WithContext(slog.NewJSONHandler(wrappedBuf, nil), func(ctx context.Context) []slog.Attr {
		return []slog.Attr{slog.String("request_id", "req-123")}
	})
	l2 := slog.New(wrapped)
	l2.InfoContext(context.Background(), "event", "k", "v")

	var baseMap, wrappedMap map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(baseBuf.Bytes()), &baseMap); err != nil {
		t.Fatalf("baseline handler produced invalid JSON, cannot compare: %v", err)
	}
	if err := json.Unmarshal(bytes.TrimSpace(wrappedBuf.Bytes()), &wrappedMap); err != nil {
		t.Fatalf("wrapped handler produced invalid JSON: %v", err)
	}
	delete(baseMap, slog.TimeKey)
	delete(wrappedMap, slog.TimeKey)

	for k, v := range baseMap {
		wv, ok := wrappedMap[k]
		if !ok {
			t.Errorf("adding request-scoped context attrs must never remove a field the base handler would have emitted on its own (monotonic: a layer must never remove information); %q=%v is missing after wrapping", k, v)
			continue
		}
		if toStr(v) != toStr(wv) {
			t.Errorf("field %q changed value after wrapping with ContextAttrs (base=%v, wrapped=%v); wrapping must only add, never alter, existing fields", k, v, wv)
		}
	}
	if wrappedMap["request_id"] != "req-123" {
		t.Errorf("ContextAttrs-supplied attrs must appear in the final output; expected request_id=req-123, got %v", wrappedMap["request_id"])
	}
}

func TestWithContext_ReceivesTheSameContextPassedToHandle(t *testing.T) {
	buf := &bytes.Buffer{}
	wrapped := logger.WithContext(slog.NewJSONHandler(buf, nil), func(ctx context.Context) []slog.Attr {
		v, _ := ctx.Value(ctxKey{}).(string)
		if v == "" {
			return nil
		}
		return []slog.Attr{slog.String("trace_id", v)}
	})
	l := slog.New(wrapped)
	ctx := context.WithValue(context.Background(), ctxKey{}, "trace-abc")
	l.InfoContext(ctx, "handled with context")

	lines := decodeJSONLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("expected exactly one line, got %d", len(lines))
	}
	m := lines[0]
	if m["trace_id"] != "trace-abc" {
		t.Errorf("slog.Handler.Handle is documented to receive the context specifically so request-scoped fields can be read from it — the reason a bespoke Info(msg, args...) interface was rejected; a value stored in context did not reach the ContextAttrs closure (got trace_id=%v)", m["trace_id"])
	}
}

// --- determinism -------------------------------------------------------------

func TestNew_IdenticalCallsUnderAFixedClockProduceIdenticalLines(t *testing.T) {
	buf := &bytes.Buffer{}
	fixed := fixedClock{time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)}
	log := logger.New(logger.Config{Output: buf, Format: logger.FormatJSON, Level: slog.LevelDebug, Clock: fixed, Source: false})
	log.Info("same call", "n", 1)
	log.Info("same call", "n", 1)

	lines := decodeJSONLines(t, buf)
	if len(lines) != 2 {
		t.Fatalf("expected two lines, got %d", len(lines))
	}
	if toStr(lines[0]) != toStr(lines[1]) {
		t.Errorf("two identical calls under a fixed, injected clock and with Source disabled must produce identical output; nothing in this package's design should vary call-to-call once real time is injected away. line1=%v line2=%v", lines[0], lines[1])
	}
}
