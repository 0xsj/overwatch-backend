package provenance

import (
	"encoding/json"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type wire struct {
	Request     string `json:"request_id"`
	Correlation string `json:"correlation_id,omitempty"`
	Causation   string `json:"causation_id,omitempty"`
	Origin      string `json:"origin,omitempty"`
	Depth       uint8  `json:"depth"`
	Attempt     uint16 `json:"attempt"`
	Actor       string `json:"actor,omitempty"`
	OnBehalfOf  string `json:"on_behalf_of,omitempty"`
	Tenant      string `json:"tenant,omitempty"`
	Traceparent string `json:"traceparent,omitempty"`
}

func (p Provenance) MarshalJSON() ([]byte, error) {
	if p.IsZero() {
		return nil, errors.New(errors.Internal, "provenance: marshalling the zero Provenance")
	}
	w := wire{
		Request: p.request.String(),
		Depth:   p.depth,
		Attempt: p.attempt,
		Tenant:  p.tenant, Traceparent: p.traceparent,
	}
	if !p.correlation.IsZero() {
		w.Correlation = p.correlation.String()
	}
	if !p.causation.IsZero() {
		w.Causation = p.causation.String()
	}
	if p.origin != OriginUnknown {
		w.Origin = p.origin.String()
	}
	if !p.actor.IsZero() {
		w.Actor = p.actor.String()
	}
	if !p.onBehalfOf.IsZero() {
		w.OnBehalfOf = p.onBehalfOf.String()
	}
	return json.Marshal(w)
}

func (p *Provenance) UnmarshalJSON(b []byte) error {
	var w wire
	if err := json.Unmarshal(b, &w); err != nil {
		return errors.Wrap(err, errors.Internal, "provenance: unreadable record")
	}
	var out Provenance
	req, err := id.Parse(w.Request)
	if err != nil || req.IsZero() {
		return errors.Newf(errors.Internal, "provenance: request_id %q is not an identifier", w.Request)
	}
	out.request = req
	if w.Correlation != "" {
		v, err := id.Parse(w.Correlation)
		if err != nil {
			return errors.Newf(errors.Internal, "provenance: correlation_id %q is not an identifier", w.Correlation)
		}
		out.correlation = v
	}
	if w.Causation != "" {
		v, err := id.Parse(w.Causation)
		if err != nil {
			return errors.Newf(errors.Internal, "provenance: causation_id %q is not an identifier", w.Causation)
		}
		out.causation = v
	}
	o, ok := ParseOrigin(w.Origin)
	if !ok || !o.known() {
		return errors.Newf(errors.Internal,
			"provenance: origin %q is not one of Origins", w.Origin)
	}
	out.origin = o
	if w.Depth > MaxDepth {
		return errors.Newf(errors.Internal, "provenance: depth %d is past MaxDepth", w.Depth)
	}
	out.depth = w.Depth
	if w.Attempt == 0 {
		return errors.New(errors.Internal, "provenance: attempt 0 — the first attempt is 1")
	}
	out.attempt = w.Attempt
	if w.Actor != "" {
		a, err := ParseActor(w.Actor)
		if err != nil {
			return err
		}
		out.actor = a
	}
	if w.OnBehalfOf != "" {
		a, err := ParseActor(w.OnBehalfOf)
		if err != nil {
			return err
		}
		out.onBehalfOf = a
	}
	if w.Tenant != "" {
		if !validID(w.Tenant) {
			return errors.Newf(errors.Internal, "provenance: tenant %q is not an identifier", w.Tenant)
		}
		out.tenant = w.Tenant
	}
	if w.Traceparent != "" {
		if !validTraceparent(w.Traceparent) {
			return errors.Newf(errors.Internal, "provenance: traceparent %q is malformed", w.Traceparent)
		}
		out.traceparent = w.Traceparent
	}
	*p = out
	return nil
}
