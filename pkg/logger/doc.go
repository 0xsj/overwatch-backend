// Package logger builds the process's structured logger.
//
// The port is log/slog. This package supplies handlers — console and JSON — and
// the wiring that puts the same fields on every line. It does not wrap slog in
// an interface of its own, and that is a decision rather than laziness.
//
//   - slog is the standard library's answer, and every Go developer already
//     knows it. A bespoke interface is a second thing to learn that does less.
//   - **Two types in this repository already speak it.** [secret.String] refuses
//     to print through slog.LogValuer, and anything else that must redact will
//     do the same. A bespoke interface duplicates that machinery, and getting
//     the redaction half wrong prints a credential.
//   - **slog.Handler.Handle receives the context**, which is where
//     request-scoped fields come from. An interface shaped
//     Info(msg string, args ...any) has nowhere for a context to arrive, so
//     anything ambient is lost at the boundary.
//
// zap and zerolog are faster per line. That buys nanoseconds on a process
// logging once per request and once per pipeline item, and costs a second field
// system to implement redaction in.
//
// # No global
//
// [New] returns a *slog.Logger to inject. Nothing here calls slog.SetDefault.
// The composition root may — so third-party libraries route through the same
// handlers — but no code in this repository depends on it.
//
// A default logger is ambient state, and ambient state is what makes a
// dependency invisible. The same argument as env.Reader having no package-level
// Get: a value reached without declaring it is a dependency nothing announced.
//
// # Two seams, and neither is imported
//
//	Clock         Now() time.Time                    clock.System satisfies it
//	ContextAttrs  func(context.Context) []slog.Attr  a provenance package will
//
// The composition root connects them:
//
//	logger.New(logger.Config{Clock: clock.System{}, Context: provenance.Attrs})
//
// [Clock] is declared here with one method, as pkg/id declares its own. Three
// packages now name the same shape without any of them importing pkg/clock —
// interfaces belong to the consumer, and the composition root is where the
// structural match is asserted.
//
// It is **wall** time, deliberately. A log timestamp is compared against other
// systems' logs, so it is an instant rather than an interval — see
// [[monotonic-and-wall-time]] for why that distinction has to be made per use.
//
// A [ContextAttrs] returning nil is normal. A record logged outside a request
// has no request-scoped fields, and that is not an error.
//
// # The clock has to be applied twice
//
// slog stamps a record with time.Now when the record is created, **inside the
// standard library, where no injected clock can reach**. So the JSON handler
// re-stamps in ReplaceAttr and the console handler formats the clock's reading
// directly rather than the record's.
//
// Without that, the rule that time is injected would be violated in the one
// package nobody would think to check — the package whose whole output is
// timestamps.
//
// The two formats render it differently, and the difference is deliberate.
// Console emits **time only**, 15:04:05.000 — a terminal log is read live, the
// date is today, and twelve more characters on every line is noise in the one
// place a human is reading rather than a machine. JSON keeps slog's full
// RFC3339 with the date and offset, because a shipped log is correlated across
// days and machines and has no "today".
//
// Stated because it was not, and its absence made the claim above untestable: a
// test given a clock fixed to 1999 and reading console output cannot tell an
// injected clock from a real one, since neither prints a year.
//
// # Source
//
// [Config.Source] adds the caller's location to each record, off by default
// because it costs a runtime.CallersFrames lookup per line and answers a
// question only a developer asks.
//
// The two formats differ here too, and this time not by choice: JSON carries
// slog's own shape — a source group of function, file and line — because
// AddSource is slog's flag and the JSON handler is slog's. Console renders
// base.go:line, dimmed, at the end of the line, because a full path is most of a
// terminal's width and the base name is what a reader greps for.
//
// A caller asserting on the shape must therefore assert per format. There is no
// single rendering to match.
//
// # An error becomes two fields
//
//	log.Error("poll failed", "cause", err)
//	→ error="reach feed: dial timeout"   err_kind="unavailable"
//
// Both, and **err_kind is the one that earns the package.** A line carrying err_kind=unavailable is
// queryable in a way msg="upstream failed" never is, and it joins naturally to
// the status code the edge produced from the same error.
//
// The caller's key is replaced, so the field names are identical wherever an
// error is logged. A component emitting error_kind while another emits err_kind
// passes its own tests while the corpus drifts, and nothing fails.
//
// The logged message is the **full chain**, not [errors.Message]. A log is where
// the cause belongs; the caller-safe form is for a response. Those are two
// different audiences and this is the one that gets everything.
//
// # What the console handler guarantees
//
// Five rules that were unstated until a mutation run found nothing testing
// them. Each was true of the implementation; none was true of this document,
// which is why the suite derived from it did not check any of them.
//
// **A record AT the configured level is emitted.** Level is a floor, not a
// threshold to exceed: Level=Info emits Info. The JSON path gets this from slog
// itself, so a test written against JSON alone proves nothing about the
// console.
//
// **Anything but a bare printable value is quoted.** A value is rendered as-is
// only when it is non-empty, is valid UTF-8, and every rune in it is printable
// and is not a space, a quote or an equals sign; everything else goes through
// strconv.Quote.
//
// Valid UTF-8 is named separately because ranging over a string does not report
// an invalid byte — it yields U+FFFD, which is printable, so a per-rune check
// alone passes the raw byte straight through. The string is validated first.
//
// The parsing half is obvious: otherwise key=value is unparseable at exactly the
// moment it matters, and a message with a space in it silently becomes two
// fields to anyone reading, and to any tool.
//
// **The rule is an allowlist because the other half is a security control.** A
// value can arrive from a source that did not write this code — a header, a
// tool's output, a filename on a scanned host — and a console handler emits
// colour, which means it emits escape sequences to a terminal. A denylist of
// separators lets a carriage return overwrite the line just printed, and lets
// ESC drive the terminal directly; both forge log output in the one record whose
// value is that it can be trusted. Listing what is safe cannot be outgrown by an
// input nobody thought of.
//
// **[New] panics on a nil [Config.Clock].** No environment can produce it and no
// operator can fix it; a logger with no clock cannot stamp a line, and failing
// at construction beats failing on the first record. [Config.Output] defaults to
// os.Stdout instead of panicking, because there is an obvious right answer and
// no ambiguity to preserve.
//
// **WithAttrs and WithGroup do not mutate the receiver.** Each returns a handler
// with its own copy of the attribute list. A shared slice means a child's
// attributes appear on a parent's records — a bug that surfaces only when both
// are used, long after either was written.
//
// **WithGroup qualifies everything added after it.** After WithGroup("db"), an
// attribute logged as host renders as db.host, and groups nest by
// concatenation. Without it a group is decoration and two subsystems logging
// host collide in the same field.
//
// # Console is the default
//
// [FormatConsole] is the zero value because a fresh clone starts at `make dev`
// and JSON in a terminal is unreadable. [FormatJSON] is one object per line, for
// anything that ships logs somewhere.
//
// Colour is [ColorAuto]: on only when the output is a terminal and NO_COLOR is
// unset or empty. That behaves correctly unattended, which is the case that
// matters — a redirected file gets no escape codes.
//
// Reading NO_COLOR is an environment access outside the composition root, which
// pkg/env otherwise owns. Named rather than hidden: it is a terminal capability
// read alongside isatty, not configuration, and every command-line tool does it.
// An empty NO_COLOR does not disable colour — the specification says the
// variable must be present and non-empty, and treating "" as set would surprise
// anyone who exports it blank.
//
// # Deliberately absent
//
// A memory handler. Tests here use a buffer and the JSON handler; nothing
// outside needs to assert on records yet. It arrives with the first consumer
// that does.
//
// Sampling and asynchronous delivery. Both are decorating handlers when they are
// wanted, and neither is a library.
//
// A level type of our own. slog.Level is already an ordered integer with a
// closed set of names, and a second vocabulary would need keeping in sync with
// it for no reader. [ParseLevel] maps a string onto it and nothing else.
//
// [Nop] returns a logger that discards everything — for a test that does not
// care, and for a component constructed without one. A nil *slog.Logger panics
// on use; this does not.
package logger
