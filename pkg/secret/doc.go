// Package secret holds a value that is safe to carry and unsafe to print.
//
// A credential is a TYPE, not a string. The difference is where the safety
// lives: a string is safe wherever somebody remembered, and a [String] is safe
// wherever it goes, including through code that has never heard of it.
//
// This package depends on nothing. That is deliberate — a domain package may
// import only the error module and this one, so a credential must not drag an
// application's configuration type along behind it.
//
// # The four methods, and why one is not enough
//
// Redaction has to be installed once per formatting path, because the standard
// library's formatters do not agree on which interface to consult:
//
//	String        fmt's %v, %s, and text/template
//	GoString      fmt's %#v
//	MarshalText   encoding/json, and every other encoding/* text path
//	LogValue      a slog attribute passed directly
//
// Measured on Go 1.27.1, a value nested inside a struct:
//
//	                    slog JSONHandler    slog TextHandler
//	LogValue only       LEAKS               redacted
//	+ MarshalText       redacted            redacted
//
// The cause is not slog. encoding/json ignores fmt.Stringer, so a type carrying
// String and LogValue and nothing else is safe in text output and leaks in JSON
// — which is the format anything shipping logs actually uses. MarshalJSON is
// redundant once MarshalText exists; json accepts either.
//
// # Reveal is the only way out, and that requires a struct
//
// [String.Reveal] returns the real value. It is the single exit, named so that
// `grep -rn Reveal` enumerates every place a credential becomes an ordinary
// string. A type that redacts everywhere except through an obvious, countable
// door is worth more than one that tries to redact everywhere.
//
// This is why [String] wraps an unexported field instead of being a named
// string type. `type String string` would carry every method below and still
// permit `string(s)`, which is a second exit that compiles, reads as ordinary
// conversion, and cannot be found by grepping for anything. The struct makes
// the claim above true rather than aspirational.
//
// [String.Format] implements fmt.Formatter, which covers every verb. A String
// method alone covers only the verbs fmt routes through fmt.Stringer, so an
// unusual one would print the struct instead of the redaction.
//
// # What this does not do
//
// It does not protect memory. The value is an ordinary string in the process,
// readable in a core dump and not zeroed on drop. Guarding that needs mlock and
// explicit wiping, which is a different package with a much larger cost, and
// nothing here pretends otherwise.
//
// It does not stop [String.Reveal] being logged. Reveal returns a string and a
// string prints. The door is deliberately visible rather than locked.
package secret
