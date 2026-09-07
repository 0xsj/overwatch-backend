package command

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// RootCreated is `entity`'s event, named as a LITERAL. The two are peers and
// `U1` refuses the import — the same contract on a string that org's granter
// subscriber carries, and the same risk: a rename over there is a silent
// no-op here.
const RootCreated = "entity.root.created"

// Roots fills in the column decisions/0029 declared and left always-zero.
//
// Two subscribers rather than one cross-schema write: `entity` creates the root
// on `target.added` and announces it, and this writes the id down. That is the
// shape `0007` and `0020` established for `workspace.opened → org grants admin`.
type Roots interface {
	SetRoot(ctx context.Context, target, root id.ID) error
}

type RootSubscriber struct{ repo Roots }

func NewRootSubscriber(repo Roots) *RootSubscriber {
	if repo == nil {
		panic("target: NewRootSubscriber with a nil repository")
	}
	return &RootSubscriber{repo: repo}
}

type rootCreated struct {
	EntityID string `json:"entity_id"`
	TargetID string `json:"target_id"`
}

// Handle is IDEMPOTENT. The outbox delivers at least once, and the store's
// `root_entity_id is null` predicate makes a redelivery affect no rows — so
// there is nothing here to check and nothing to refuse.
func (s *RootSubscriber) Handle(ctx context.Context, e events.Event) error {
	if e.Name != RootCreated {
		return nil
	}
	var payload rootCreated
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return fmt.Errorf("target: decode %s: %w", e.Name, err)
	}
	target, err := id.Parse(payload.TargetID)
	if err != nil {
		return fmt.Errorf("target: %s has no readable target: %w", e.Name, err)
	}
	root, err := id.Parse(payload.EntityID)
	if err != nil {
		return fmt.Errorf("target: %s has no readable entity: %w", e.Name, err)
	}
	return s.repo.SetRoot(ctx, target, root)
}
