package provenance

import (
	"log/slog"
	"math"
	"strings"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var ErrDepthExceeded = errors.New(errors.Internal, "provenance: causal chain is deeper than MaxDepth")

type Minter interface {
	NewID() id.ID
}

type Adopted struct {
	Correlation string
	Causation   string
	Traceparent string
}

type Provenance struct {
	request     id.ID
	correlation id.ID
	causation   id.ID
	origin      Origin
	depth       uint8
	attempt     uint16
	actor       Actor
	onBehalfOf  Actor
	tenant      string
	traceparent string
}

func New(origin Origin, m Minter) Provenance {
	if m == nil {
		panic("provenance: New with a nil Minter")
	}
	if !origin.known() {
		panic("provenance: New with an origin outside Origins — a chain with no reason is unreadable")
	}
	root := m.NewID()
	return Provenance{
		request:     root,
		correlation: root,
		origin:      origin,
		attempt:     1,
	}
}

func (p Provenance) Derive(m Minter) (Provenance, error) {
	return p.derive(m, p.request)
}

func (p Provenance) DeriveFrom(m Minter, cause id.ID) (Provenance, error) {
	if cause.IsZero() {
		return Provenance{}, errors.New(errors.Internal,
			"provenance: DeriveFrom with the zero cause")
	}
	return p.derive(m, cause)
}

func (p Provenance) derive(m Minter, cause id.ID) (Provenance, error) {
	if p.IsZero() {
		panic("provenance: derive from the zero Provenance")
	}
	if m == nil {
		panic("provenance: derive with a nil Minter")
	}
	if int(p.depth)+1 > MaxDepth {
		return Provenance{}, ErrDepthExceeded
	}
	c := p
	c.request = m.NewID()
	c.causation = cause
	c.depth = p.depth + 1
	c.attempt = 1
	return c, nil
}

func (p Provenance) Retry() Provenance {
	if p.IsZero() {
		panic("provenance: Retry on the zero Provenance")
	}
	c := p
	if c.attempt < math.MaxUint16 {
		c.attempt++
	}
	return c
}

func (p Provenance) Adopt(a Adopted) Provenance {
	if p.IsZero() {
		panic("provenance: Adopt on the zero Provenance")
	}
	c := p
	if v, err := id.Parse(a.Correlation); err == nil && !v.IsZero() {
		c.correlation = v
	}
	if v, err := id.Parse(a.Causation); err == nil && !v.IsZero() {
		c.causation = v
	}
	if validTraceparent(a.Traceparent) {
		c.traceparent = a.Traceparent
	}
	return c
}

func (p Provenance) WithActor(a Actor) Provenance {
	if p.IsZero() {
		panic("provenance: WithActor on the zero Provenance")
	}
	c := p
	c.actor = a
	return c
}

func (p Provenance) WithOnBehalfOf(a Actor) (Provenance, error) {
	if p.IsZero() {
		panic("provenance: WithOnBehalfOf on the zero Provenance")
	}
	if a.IsZero() {
		return Provenance{}, errors.New(errors.Internal,
			"provenance: acting on behalf of the anonymous actor is meaningless")
	}
	if p.actor.IsZero() {
		return Provenance{}, errors.New(errors.Internal,
			"provenance: an anonymous actor cannot act for somebody")
	}
	if a == p.actor {
		return Provenance{}, errors.New(errors.Internal,
			"provenance: acting on your own behalf is not a delegation")
	}
	c := p
	c.onBehalfOf = a
	return c, nil
}

func (p Provenance) WithTenant(tenant string) (Provenance, error) {
	if p.IsZero() {
		panic("provenance: WithTenant on the zero Provenance")
	}
	if !validID(tenant) {
		return Provenance{}, errors.Newf(errors.Internal,
			"provenance: tenant %q is not 1-%d characters of [A-Za-z0-9._:/-]",
			tenant, MaxIDLength)
	}
	c := p
	c.tenant = tenant
	return c, nil
}

func (p Provenance) Request() id.ID { return p.request }

func (p Provenance) Correlation() id.ID { return p.correlation }

func (p Provenance) Causation() id.ID { return p.causation }

func (p Provenance) Origin() Origin { return p.origin }

func (p Provenance) Depth() uint8 { return p.depth }

func (p Provenance) Attempt() uint16 { return p.attempt }

func (p Provenance) Actor() Actor { return p.actor }

func (p Provenance) OnBehalfOf() Actor { return p.onBehalfOf }

func (p Provenance) Tenant() string { return p.tenant }

func (p Provenance) Traceparent() string { return p.traceparent }

func (p Provenance) Delegated() bool { return !p.onBehalfOf.IsZero() }

func (p Provenance) Root() bool {
	return !p.IsZero() && p.depth == 0 && p.correlation == p.request
}

func (p Provenance) IsZero() bool { return p.request.IsZero() }

func (p Provenance) Attrs() []slog.Attr {
	if p.IsZero() {
		return nil
	}
	out := make([]slog.Attr, 0, 10)
	out = append(out, slog.String(FieldRequest, p.request.String()))
	if !p.correlation.IsZero() {
		out = append(out, slog.String(FieldCorrelation, p.correlation.String()))
	}
	if !p.causation.IsZero() {
		out = append(out, slog.String(FieldCausation, p.causation.String()))
	}
	if p.origin != OriginUnknown {
		out = append(out, slog.String(FieldOrigin, p.origin.String()))
	}
	out = append(out, slog.Uint64(FieldDepth, uint64(p.depth)))
	out = append(out, slog.Uint64(FieldAttempt, uint64(p.attempt)))
	if !p.actor.IsZero() {
		out = append(out, slog.String(FieldActor, p.actor.String()))
	}
	if !p.onBehalfOf.IsZero() {
		out = append(out, slog.String(FieldOnBehalfOf, p.onBehalfOf.String()))
	}
	if p.tenant != "" {
		out = append(out, slog.String(FieldTenant, p.tenant))
	}
	if p.traceparent != "" {
		out = append(out, slog.String(FieldTraceparent, p.traceparent))
	}
	return out
}

func (p Provenance) LogValue() slog.Value {
	return slog.GroupValue(p.Attrs()...)
}

func (p Provenance) String() string {
	attrs := p.Attrs()
	if len(attrs) == 0 {
		return "provenance(zero)"
	}
	var b strings.Builder
	for i, a := range attrs {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(a.Key)
		b.WriteByte('=')
		b.WriteString(a.Value.String())
	}
	return b.String()
}
