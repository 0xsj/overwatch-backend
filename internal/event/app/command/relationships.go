package command

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/event/domain"
	reviewdomain "github.com/0xsj/overwatch-backend/internal/review/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type RelationshipRepository interface {
	CreateRelationship(context.Context, domain.Relationship) error
	RelationshipByID(context.Context, id.ID, id.ID) (domain.Relationship, error)
	SaveRelationship(context.Context, domain.Relationship) error
}

type RelationshipEvidence interface {
	EvidenceByID(context.Context, id.ID, id.ID) (reviewdomain.Evidence, error)
}

type Relationships struct {
	repo      RelationshipRepository
	events    EventReader
	evidence  RelationshipEvidence
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewRelationships(repo RelationshipRepository, eventReader EventReader, evidence RelationshipEvidence, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Relationships {
	if repo == nil || eventReader == nil || evidence == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("event: NewRelationships with a nil dependency")
	}
	return &Relationships{repo: repo, events: eventReader, evidence: evidence, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (r *Relationships) Create(ctx context.Context, workspace, author, from, to id.ID, kind domain.RelationshipKind, rationale string) (domain.Relationship, error) {
	return r.CreateWithEvidence(ctx, workspace, author, from, to, kind, rationale, nil, nil)
}

func (r *Relationships) CreateWithEvidence(ctx context.Context, workspace, author, from, to id.ID, kind domain.RelationshipKind, rationale string, supporting, opposing []id.ID) (domain.Relationship, error) {
	if _, err := r.events.ByID(ctx, workspace, from); err != nil {
		return domain.Relationship{}, err
	}
	if _, err := r.events.ByID(ctx, workspace, to); err != nil {
		return domain.Relationship{}, err
	}
	for _, observation := range append(append([]id.ID{}, supporting...), opposing...) {
		if _, err := r.evidence.EvidenceByID(ctx, workspace, observation); err != nil {
			return domain.Relationship{}, err
		}
	}
	fresh, err := domain.NewRelationshipWithEvidence(r.ids.NewID(), workspace, from, to, author, kind, rationale, supporting, opposing, r.clock.Now())
	if err != nil {
		return domain.Relationship{}, err
	}
	if err := r.tx.InTx(ctx, func(ctx context.Context) error {
		if err := r.repo.CreateRelationship(ctx, fresh); err != nil {
			return err
		}
		return r.publish(ctx, "timeline.event.relationship.recorded", fresh)
	}); err != nil {
		return domain.Relationship{}, err
	}
	return fresh, nil
}

func (r *Relationships) Review(ctx context.Context, workspace, relationship, reviewer id.ID, state domain.RelationshipState, note string) (domain.Relationship, error) {
	var saved domain.Relationship
	if err := r.tx.InTx(ctx, func(ctx context.Context) error {
		current, err := r.repo.RelationshipByID(ctx, workspace, relationship)
		if err != nil {
			return err
		}
		saved, err = current.Review(reviewer, state, note, r.clock.Now())
		if err != nil {
			return err
		}
		if err := r.repo.SaveRelationship(ctx, saved); err != nil {
			return err
		}
		return r.publish(ctx, "timeline.event.relationship.reviewed", saved)
	}); err != nil {
		return domain.Relationship{}, err
	}
	return saved, nil
}

func (r *Relationships) publish(ctx context.Context, name string, relationship domain.Relationship) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, r.ids)
	}
	prov, err := prov.WithTenant(relationship.WorkspaceID.String())
	if err != nil {
		return err
	}
	event, err := events.NewDecision(r.ids, r.clock, name, "workspace:"+relationship.WorkspaceID.String(), prov, map[string]string{
		"workspace_id": relationship.WorkspaceID.String(), "relationship_id": relationship.ID.String(), "from_event_id": relationship.FromEventID.String(), "to_event_id": relationship.ToEventID.String(), "kind": string(relationship.Kind), "state": string(relationship.State), "updated_by": relationship.UpdatedBy.String(),
	})
	if err != nil {
		return err
	}
	return r.publisher.Publish(ctx, event)
}
