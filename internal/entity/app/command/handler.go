package command

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// The two event names this domain listens for. They are LITERALS rather than
// imports of `target`'s and `observation`'s constants, because those are peers
// and `U1` refuses the edge — the same contract on a name that org's granter
// subscriber already carries.
const (
	TargetAdded         = "target.added"
	ObservationsCreated = "extract.observation.created"
)

type targetAdded struct {
	TargetID    string `json:"target_id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
}

type observationsCreated struct {
	WorkspaceID  string `json:"workspace_id"`
	InvocationID string `json:"invocation_id"`
}

type Subscriber struct {
	assembler *Assembler
	ids       Minter
}

func NewSubscriber(assembler *Assembler, ids Minter) *Subscriber {
	if assembler == nil || ids == nil {
		panic("entity: NewSubscriber with a nil dependency")
	}
	return &Subscriber{assembler: assembler, ids: ids}
}

// Handle is the one entry point for both subscriptions. It reads what it needs
// out of the PAYLOAD rather than off the actor or the tenant, because a
// subscriber must not have to trust that a middleware was wired —
// decisions/0013.
func (s *Subscriber) Handle(ctx context.Context, e events.Event) error {
	switch e.Name {
	case TargetAdded:
		return s.root(ctx, e)
	case ObservationsCreated:
		return s.observed(ctx, e)
	default:
		return nil
	}
}

func (s *Subscriber) root(ctx context.Context, e events.Event) error {
	var payload targetAdded
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return fmt.Errorf("entity: decode %s: %w", e.Name, err)
	}
	target, err := id.Parse(payload.TargetID)
	if err != nil {
		return fmt.Errorf("entity: %s has no readable target: %w", e.Name, err)
	}
	workspace, err := id.Parse(payload.WorkspaceID)
	if err != nil {
		return fmt.Errorf("entity: %s has no readable workspace: %w", e.Name, err)
	}
	caused, err := s.caused(e)
	if err != nil {
		return err
	}
	return s.assembler.RootFor(provenance.NewContext(ctx, caused), workspace, target,
		payload.Kind, payload.Name)
}

func (s *Subscriber) observed(ctx context.Context, e events.Event) error {
	var payload observationsCreated
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return fmt.Errorf("entity: decode %s: %w", e.Name, err)
	}
	workspace, err := id.Parse(payload.WorkspaceID)
	if err != nil {
		return fmt.Errorf("entity: %s has no readable workspace: %w", e.Name, err)
	}
	invocation, err := id.Parse(payload.InvocationID)
	if err != nil {
		return fmt.Errorf("entity: %s has no readable invocation: %w", e.Name, err)
	}
	caused, err := s.caused(e)
	if err != nil {
		return err
	}
	_, err = s.assembler.Observed(provenance.NewContext(ctx, caused), workspace, invocation)
	return err
}

// caused keeps the chain: what this subscriber does was caused by the event it
// received, and a correlation that stops here makes the run log a set of
// unrelated lines.
func (s *Subscriber) caused(e events.Event) (provenance.Provenance, error) {
	if e.Provenance.IsZero() {
		return provenance.Provenance{}, fmt.Errorf("entity: %s carries no provenance", e.Name)
	}
	return e.Provenance.DeriveFrom(s.ids, e.ID)
}
