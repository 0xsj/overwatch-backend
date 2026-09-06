// AUTHOR-WRITTEN. Not produced behind an information barrier: written after a
// mutation run showed eight of sixteen mutants surviving the barrier-written
// suite, by someone who had read the implementation and the survivors.
//
// Five of those eight were behaviours true of the code and absent from the
// document, so the suite derived from the document could not have covered them.
// The spec now states all five; these pin them. custody/0006 records the run.
package logger_test

import (
	"bytes"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/logger"
)

type stopped struct{ at time.Time }

func (s stopped) Now() time.Time { return s.at }

func console(t *testing.T, buf *bytes.Buffer, level slog.Level) *slog.Logger {
	t.Helper()
	return logger.New(logger.Config{
		Output: buf, Format: logger.FormatConsole, Level: level,
		Clock: stopped{time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)},
	})
}

func TestLevelIsAFloorNotAThresholdToExceed(t *testing.T) {
	for _, tt := range []struct {
		name    string
		at      slog.Level
		log     func(*slog.Logger)
		emitted bool
	}{
		{"a record at the configured level is emitted", slog.LevelInfo, func(l *slog.Logger) { l.Info("x") }, true},
		{"a record above it is emitted", slog.LevelInfo, func(l *slog.Logger) { l.Warn("x") }, true},
		{"a record below it is dropped", slog.LevelInfo, func(l *slog.Logger) { l.Debug("x") }, false},
		{"warn floor emits warn", slog.LevelWarn, func(l *slog.Logger) { l.Warn("x") }, true},
		{"warn floor drops info", slog.LevelWarn, func(l *slog.Logger) { l.Info("x") }, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tt.log(console(t, &buf, tt.at))
			if got := buf.Len() > 0; got != tt.emitted {
				t.Errorf("emitted=%v want %v — Level is a floor: a record AT it must appear, and the JSON path gets this from slog itself so only the console handler proves it", got, tt.emitted)
			}
		})
	}
}

func TestConsoleQuotesWhatWouldOtherwiseBeAmbiguous(t *testing.T) {
	for _, tt := range []struct{ name, value, want string }{
		{"a value with a space", "two words", `k="two words"`},
		{"an empty value", "", `k=""`},
		{"a value containing an equals sign", "a=b", `k="a=b"`},
		{"a value containing a quote", `say "hi"`, `k="say \"hi\""`},
		{"an ordinary value is left alone", "plain", "k=plain"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			console(t, &buf, slog.LevelInfo).Info("m", "k", tt.value)
			if !strings.Contains(buf.String(), tt.want) {
				t.Errorf("console output %q lacks %q — key=value is unparseable at exactly the moment it matters, and a value with a space silently becomes two fields to any reader and any tool", buf.String(), tt.want)
			}
		})
	}
}

func TestNewPanicsOnANilClockAndDefaultsTheOutput(t *testing.T) {
	t.Run("a nil Clock panics at construction", func(t *testing.T) {
		wantPanic(t, "nil Clock", func() {
			_ = logger.New(logger.Config{Output: &bytes.Buffer{}})
		})
	})
	t.Run("a nil Output defaults rather than panicking", func(t *testing.T) {
		defer func() {
			if v := recover(); v != nil {
				t.Errorf("a nil Output has an obvious right answer — os.Stdout — and no ambiguity to preserve, so it must not panic; got %v", v)
			}
		}()
		_ = logger.New(logger.Config{Clock: stopped{time.Now()}})
	})
}

func TestWithAttrsDoesNotMutateTheHandlerItCameFrom(t *testing.T) {
	// Both loggers share one handler and therefore one buffer, so the assertion
	// has to be per line: the child's record may carry the attribute, the
	// parent's must not.
	var buf bytes.Buffer
	parent := console(t, &buf, slog.LevelInfo)
	child := parent.With("only", "on-the-child")

	child.Info("child")
	parent.Info("parent")

	var parentLine string
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.Contains(line, " parent") {
			parentLine = line
		}
	}
	if parentLine == "" {
		t.Fatalf("no parent record in %q", buf.String())
	}
	if strings.Contains(parentLine, "only=") {
		t.Errorf("an attribute added to a derived logger reached the one it came from: %q — the handlers share an attrs slice, and that surfaces only when both are used, long after either was written", parentLine)
	}
}

func TestWithGroupQualifiesEverythingAddedAfterIt(t *testing.T) {
	t.Run("an attribute after a group is prefixed", func(t *testing.T) {
		var buf bytes.Buffer
		console(t, &buf, slog.LevelInfo).WithGroup("db").Info("m", "host", "localhost")
		if !strings.Contains(buf.String(), "db.host=localhost") {
			t.Errorf("got %q, want db.host= — without qualification a group is decoration, and two subsystems logging `host` collide in one field", buf.String())
		}
	})
	t.Run("groups nest by concatenation", func(t *testing.T) {
		var buf bytes.Buffer
		console(t, &buf, slog.LevelInfo).WithGroup("a").WithGroup("b").Info("m", "k", "v")
		if !strings.Contains(buf.String(), "a.b.k=v") {
			t.Errorf("got %q, want a.b.k=", buf.String())
		}
	})
	t.Run("With after a group is qualified too", func(t *testing.T) {
		var buf bytes.Buffer
		console(t, &buf, slog.LevelInfo).WithGroup("db").With("host", "localhost").Info("m")
		if !strings.Contains(buf.String(), "db.host=localhost") {
			t.Errorf("got %q — WithAttrs must qualify with the group in force when it was called", buf.String())
		}
	})
}

// Parent-to-child aliasing is not enough to expose a shared attrs slice: a
// parent with no attributes makes append allocate, so nothing is shared. The
// aliasing shows between SIBLINGS — two handlers derived from the same parent,
// each appending into the same backing array at the same index.
func TestSiblingHandlersDoNotShareABackingArray(t *testing.T) {
	var buf bytes.Buffer
	// Three attributes in one call: append grows the slice to cap 4 with len 3,
	// leaving spare capacity. Without that, every append reallocates and a
	// shared backing array is unobservable — which is what makes this bug hard.
	base := console(t, &buf, slog.LevelInfo).With("a", "1", "b", "2", "c", "3")

	left := base.With("side", "left")
	right := base.With("side", "right")

	left.Info("L")
	right.Info("R")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two records, got %q", buf.String())
	}
	for _, tt := range []struct{ line, want string }{
		{lines[0], "side=left"},
		{lines[1], "side=right"},
	} {
		if !strings.Contains(tt.line, tt.want) {
			t.Errorf("record %q lacks %q — two handlers derived from one parent appended into the same backing array, so the later one overwrote the earlier's attribute", tt.line, tt.want)
		}
	}
}

func TestConsoleQuotesEveryNonPrintable(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"an escape sequence would drive the terminal", "\x1b[2K\x1b[1;31mFAKE"},
		{"a carriage return would overwrite the line above", "ok\rERROR forged"},
		{"a newline would forge a second record", "ok\nlevel=ERROR"},
		{"a bare control character", "a\x00b"},
		{"a delete character", "a\x7fb"},
		{"invalid utf-8 is not passed through", "a\xffb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			console(t, &buf, slog.LevelInfo).Info("m", "v", tc.value)
			got := buf.String()
			if strings.Contains(got, tc.value) {
				t.Fatalf("the raw value reached the output: %q\nin: %q", tc.value, got)
			}
			if !strings.Contains(got, "v="+strconv.Quote(tc.value)) {
				t.Fatalf("want v=%s, got %q", strconv.Quote(tc.value), got)
			}
		})
	}
}

func TestConsoleLeavesAPrintableValueBare(t *testing.T) {
	for _, v := range []string{"plain", "a/b:c-d.e", "café", "127.0.0.1:7000"} {
		var buf bytes.Buffer
		console(t, &buf, slog.LevelInfo).Info("m", "v", v)
		if !strings.Contains(buf.String(), "v="+v+" ") && !strings.HasSuffix(strings.TrimRight(buf.String(), "\n"), "v="+v) {
			t.Fatalf("want a bare v=%s, got %q", v, buf.String())
		}
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
