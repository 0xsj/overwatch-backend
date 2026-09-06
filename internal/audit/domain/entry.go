package domain

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

const SubjectKindAccount = "account"

type Scope uint8

const (
	ScopeSystem Scope = iota
	ScopeAccount
	ScopeWorkspace
)

func (s Scope) String() string {
	switch s {
	case ScopeAccount:
		return "account"
	case ScopeWorkspace:
		return "workspace"
	default:
		return "system"
	}
}

func ParseScope(s string) (Scope, error) {
	switch s {
	case "system":
		return ScopeSystem, nil
	case "account":
		return ScopeAccount, nil
	case "workspace":
		return ScopeWorkspace, nil
	default:
		return ScopeSystem, ErrScopeUnknown
	}
}

func ScopeOf(e events.Event) Scope {
	if e.Provenance.Tenant() != "" {
		return ScopeWorkspace
	}
	if e.SubjectKind() == SubjectKindAccount {
		return ScopeAccount
	}
	return ScopeSystem
}

type Entry struct {
	ID          id.ID
	EventID     id.ID
	Scope       Scope
	Action      string
	Subject     string
	Actor       string
	OnBehalfOf  string
	WorkspaceID string
	Correlation id.ID
	Causation   id.ID
	Detail      json.RawMessage
	OccurredAt  time.Time
	RecordedAt  time.Time
}

var ErrNotADecision = errors.New(errors.Invalid, "the event is not a decision and earns no entry")

func FromEvent(newID id.ID, e events.Event, at time.Time) (Entry, error) {
	if !e.Decision {
		return Entry{}, ErrNotADecision
	}
	if newID.IsZero() || e.ID.IsZero() {
		return Entry{}, ErrIDRequired
	}
	if at.IsZero() || e.OccurredAt.IsZero() {
		return Entry{}, ErrTimeRequired
	}
	actor := e.Provenance.Actor().String()
	if strings.TrimSpace(actor) == "" {
		return Entry{}, ErrActorRequired
	}
	scope := ScopeOf(e)
	workspace := ""
	if scope == ScopeWorkspace {
		workspace = e.Provenance.Tenant()
	}
	onBehalfOf := ""
	if e.Provenance.Delegated() {
		onBehalfOf = e.Provenance.OnBehalfOf().String()
	}
	detail := e.Payload
	if len(detail) == 0 {
		detail = json.RawMessage("{}")
	}
	return Entry{
		ID:          newID,
		EventID:     e.ID,
		Scope:       scope,
		Action:      e.Name,
		Subject:     e.Subject,
		Actor:       actor,
		OnBehalfOf:  onBehalfOf,
		WorkspaceID: workspace,
		Correlation: e.Provenance.Correlation(),
		Causation:   e.Provenance.Causation(),
		Detail:      detail,
		OccurredAt:  e.OccurredAt,
		RecordedAt:  at,
	}, nil
}
