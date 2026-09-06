package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
)

const (
	FieldError   = "error"
	FieldErrKind = "err_kind"
)

type Clock interface {
	Now() time.Time
}

type ContextAttrs func(ctx context.Context) []slog.Attr

type Format uint8

const (
	FormatConsole Format = iota
	FormatJSON
)

type ColorMode uint8

const (
	ColorAuto ColorMode = iota
	ColorNever
	ColorAlways
)

type Config struct {
	Level   slog.Level
	Format  Format
	Output  io.Writer
	Color   ColorMode
	Source  bool
	Clock   Clock
	Context ContextAttrs
}

func New(cfg Config) *slog.Logger {
	if cfg.Clock == nil {
		panic("logger: New with a nil Clock")
	}
	if cfg.Output == nil {
		cfg.Output = os.Stdout
	}

	var h slog.Handler
	switch cfg.Format {
	case FormatJSON:
		h = slog.NewJSONHandler(cfg.Output, &slog.HandlerOptions{
			Level:       cfg.Level,
			AddSource:   cfg.Source,
			ReplaceAttr: replaceAttr(cfg.Clock),
		})
	default:
		h = newConsole(cfg)
	}
	return slog.New(WithContext(h, cfg.Context))
}

func Nop() *slog.Logger { return slog.New(slog.DiscardHandler) }

func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, errors.Newf(errors.Invalid, "unknown log level %q", s)
}

func WithContext(h slog.Handler, attrs ContextAttrs) slog.Handler {
	if attrs == nil {
		return h
	}
	return contextHandler{inner: h, attrs: attrs}
}

func errorPair(a slog.Attr) ([]slog.Attr, bool) {
	if a.Value.Kind() != slog.KindAny {
		return nil, false
	}
	err, ok := a.Value.Any().(error)
	if !ok || err == nil {
		return nil, false
	}
	return []slog.Attr{
		slog.String(FieldError, err.Error()),
		slog.String(FieldErrKind, errors.KindOf(err).String()),
	}, true
}

func replaceAttr(clk Clock) func(groups []string, a slog.Attr) slog.Attr {
	return func(groups []string, a slog.Attr) slog.Attr {
		if len(groups) == 0 && a.Key == slog.TimeKey {
			return slog.Time(slog.TimeKey, clk.Now())
		}
		if pair, ok := errorPair(a); ok {
			return slog.Group("", pair[0], pair[1])
		}
		return a
	}
}

type contextHandler struct {
	inner slog.Handler
	attrs ContextAttrs
}

var _ slog.Handler = contextHandler{}

func (h contextHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if attrs := h.attrs(ctx); len(attrs) > 0 {
		r.AddAttrs(attrs...)
	}
	return h.inner.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h.inner = h.inner.WithAttrs(attrs)
	return h
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	h.inner = h.inner.WithGroup(name)
	return h
}
