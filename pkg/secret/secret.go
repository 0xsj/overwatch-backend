package secret

import (
	"fmt"
	"io"
	"log/slog"
)

const Redacted = "[REDACTED]"

type String struct {
	v string
}

func New(v string) String { return String{v: v} }

func (s String) Reveal() string { return s.v }

func (s String) IsZero() bool { return s.v == "" }

func (s String) String() string { return Redacted }

func (s String) GoString() string { return Redacted }

func (s String) Format(f fmt.State, verb rune) { io.WriteString(f, Redacted) }

func (s String) MarshalText() ([]byte, error) { return []byte(Redacted), nil }

func (s String) LogValue() slog.Value { return slog.StringValue(Redacted) }
