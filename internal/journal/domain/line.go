package domain

import (
	"encoding/json"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var (
	ErrIDRequired   = errors.New(errors.Invalid, "an identifier is required")
	ErrTimeRequired = errors.New(errors.Invalid, "an instant is required")
	ErrLineGone     = errors.New(errors.NotFound, "line")
)

type Line struct {
	ID          id.ID
	EventID     id.ID
	Action      string
	Subject     string
	Origin      string
	Actor       string
	OnBehalfOf  string
	WorkspaceID string
	Depth       int
	Attempt     int
	Decision    bool
	Correlation id.ID
	Causation   id.ID
	Detail      json.RawMessage
	OccurredAt  time.Time
	RecordedAt  time.Time
}

func FromEvent(newID id.ID, e events.Event, at time.Time) (Line, error) {
	if newID.IsZero() || e.ID.IsZero() {
		return Line{}, ErrIDRequired
	}
	if at.IsZero() || e.OccurredAt.IsZero() {
		return Line{}, ErrTimeRequired
	}
	onBehalfOf := ""
	if e.Provenance.Delegated() {
		onBehalfOf = e.Provenance.OnBehalfOf().String()
	}
	detail := e.Payload
	if len(detail) == 0 {
		detail = json.RawMessage("{}")
	}
	return Line{
		ID:          newID,
		EventID:     e.ID,
		Action:      e.Name,
		Subject:     e.Subject,
		Origin:      e.Provenance.Origin().String(),
		Actor:       e.Provenance.Actor().String(),
		OnBehalfOf:  onBehalfOf,
		WorkspaceID: e.Provenance.Tenant(),
		Depth:       int(e.Provenance.Depth()),
		Attempt:     int(e.Provenance.Attempt()),
		Decision:    e.Decision,
		Correlation: e.Provenance.Correlation(),
		Causation:   e.Provenance.Causation(),
		Detail:      detail,
		OccurredAt:  e.OccurredAt,
		RecordedAt:  at,
	}, nil
}
