package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	colorReset = "\033[0m"
	colorDim   = "\033[2m"
	colorRed   = "\033[31m"
	colorAmber = "\033[33m"
	colorBlue  = "\033[34m"
	colorGrey  = "\033[90m"
)

const timeLayout = "15:04:05.000"

type console struct {
	mu     *sync.Mutex
	w      io.Writer
	level  slog.Level
	color  bool
	source bool
	clock  Clock
	attrs  []slog.Attr
	group  string
}

var _ slog.Handler = (*console)(nil)

func newConsole(cfg Config) slog.Handler {
	return &console{
		mu:     &sync.Mutex{},
		w:      cfg.Output,
		level:  cfg.Level,
		color:  useColor(cfg.Color, cfg.Output),
		source: cfg.Source,
		clock:  cfg.Clock,
	}
}

func useColor(mode ColorMode, w io.Writer) bool {
	switch mode {
	case ColorAlways:
		return true
	case ColorNever:
		return false
	}
	if v, ok := os.LookupEnv("NO_COLOR"); ok && v != "" {
		return false
	}
	return isTerminal(w)
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func (h *console) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level }

func (h *console) clone() *console {
	c := *h
	c.attrs = append([]slog.Attr(nil), h.attrs...)
	return &c
}

func (h *console) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	c := h.clone()
	for _, a := range attrs {
		c.attrs = append(c.attrs, qualify(h.group, a))
	}
	return c
}

func (h *console) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	c := h.clone()
	c.group = h.group + name + "."
	return c
}

func (h *console) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder

	h.paint(&b, colorDim, h.clock.Now().Format(timeLayout))
	b.WriteByte(' ')
	h.paint(&b, levelColor(r.Level), fmt.Sprintf("%-5s", levelName(r.Level)))
	b.WriteByte(' ')
	b.WriteString(r.Message)

	for _, a := range h.attrs {
		h.appendAttr(&b, "", a)
	}
	r.Attrs(func(a slog.Attr) bool {
		h.appendAttr(&b, h.group, a)
		return true
	})

	if h.source && r.PC != 0 {
		b.WriteByte(' ')
		h.paint(&b, colorGrey, source(r.PC))
	}
	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *console) appendAttr(b *strings.Builder, prefix string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return
	}

	if a.Value.Kind() == slog.KindGroup {
		next := prefix
		if a.Key != "" {
			next = prefix + a.Key + "."
		}
		for _, g := range a.Value.Group() {
			h.appendAttr(b, next, g)
		}
		return
	}

	if pair, ok := errorPair(a); ok {
		for _, p := range pair {
			h.appendAttr(b, prefix, p)
		}
		return
	}

	b.WriteByte(' ')
	h.paint(b, colorGrey, prefix+a.Key+"=")
	b.WriteString(escape(a.Value))
}

func (h *console) paint(b *strings.Builder, color, s string) {
	if !h.color {
		b.WriteString(s)
		return
	}
	b.WriteString(color)
	b.WriteString(s)
	b.WriteString(colorReset)
}

func qualify(prefix string, a slog.Attr) slog.Attr {
	if prefix == "" {
		return a
	}
	a.Key = prefix + a.Key
	return a
}

func escape(v slog.Value) string {
	s := v.String()
	if s == "" || !bare(s) {
		return strconv.Quote(s)
	}
	return s
}

func bare(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		switch r {
		case ' ', '"', '=':
			return false
		}
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func levelName(l slog.Level) string {
	switch {
	case l < slog.LevelInfo:
		return "DEBUG"
	case l < slog.LevelWarn:
		return "INFO"
	case l < slog.LevelError:
		return "WARN"
	default:
		return "ERROR"
	}
}

func levelColor(l slog.Level) string {
	switch {
	case l < slog.LevelInfo:
		return colorGrey
	case l < slog.LevelWarn:
		return colorBlue
	case l < slog.LevelError:
		return colorAmber
	default:
		return colorRed
	}
}

func source(pc uintptr) string {
	fs := runtime.CallersFrames([]uintptr{pc})
	f, _ := fs.Next()
	if f.File == "" {
		return ""
	}
	return filepath.Base(f.File) + ":" + strconv.Itoa(f.Line)
}
