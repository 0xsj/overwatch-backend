package provenance

import (
	"context"
	"log/slog"

	"github.com/0xsj/overwatch-backend/pkg/errors"
)

type contextKey struct{}

func NewContext(ctx context.Context, p Provenance) context.Context {
	if p.IsZero() {
		panic("provenance: NewContext with the zero Provenance")
	}
	return context.WithValue(ctx, contextKey{}, p)
}

func Current(ctx context.Context) (Provenance, bool) {
	p, ok := ctx.Value(contextKey{}).(Provenance)
	if !ok || p.IsZero() {
		return Provenance{}, false
	}
	return p, true
}

func Require(ctx context.Context) (Provenance, error) {
	p, ok := Current(ctx)
	if !ok {
		return Provenance{}, errors.New(errors.Internal, "provenance: none in context")
	}
	return p, nil
}

func Attrs(ctx context.Context) []slog.Attr {
	p, ok := Current(ctx)
	if !ok {
		return nil
	}
	return p.Attrs()
}
