package domain

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	SubjectKindAccount = "account"

	// SubjectKindOrg is a literal rather than an import of org's constant: audit
	// imports no domain, which is what lets it record events from packages that
	// do not exist yet — decisions/0006.
	SubjectKindOrg = "org"
)

type Scope uint8

const (
	ScopeSystem Scope = iota
	ScopeAccount
	ScopeWorkspace
	ScopeOrg
)

func (s Scope) String() string {
	switch s {
	case ScopeAccount:
		return "account"
	case ScopeWorkspace:
		return "workspace"
	case ScopeOrg:
		return "org"
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
	case "org":
		return ScopeOrg, nil
	default:
		return ScopeSystem, ErrScopeUnknown
	}
}

// ScopeOf derives the scope from what the event already carries — decisions/0024.
// Nothing new rides in the envelope: the tenant names a workspace and the
// SUBJECT names everything else, which is what decisions/0013 put it there for.
func ScopeOf(e events.Event) Scope {
	if e.Provenance.Tenant() != "" {
		return ScopeWorkspace
	}
	switch e.SubjectKind() {
	case SubjectKindAccount:
		return ScopeAccount
	case SubjectKindOrg:
		return ScopeOrg
	default:
		return ScopeSystem
	}
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
	OrgID       string
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
	workspace, org := "", ""
	switch scope {
	case ScopeWorkspace:
		workspace = e.Provenance.Tenant()
	case ScopeOrg:
		// The subject's id, exactly as workspace_id is the tenant. An org-scope
		// row carries NO workspace id — the constraint refuses one, and that is
		// what makes this scope safe to read org-wide.
		org = e.SubjectID()
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
		OrgID:       org,
		Correlation: e.Provenance.Correlation(),
		Causation:   e.Provenance.Causation(),
		Detail:      detail,
		OccurredAt:  e.OccurredAt,
		RecordedAt:  at,
	}, nil
}
