package command

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/workspace/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

const DefaultName = "Personal"

type Service struct {
	repo      Repository
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewService(repo Repository, publisher events.Publisher, ids Minter, clock Clock) *Service {
	if repo == nil || publisher == nil || ids == nil || clock == nil {
		panic("workspace: NewService with a nil dependency")
	}
	return &Service{repo: repo, publisher: publisher, ids: ids, clock: clock}
}

// Provision creates a workspace for an org. `cause` is the event that asked for
// it and is what makes a redelivery a no-op — decisions/0017.
func (s *Service) Provision(ctx context.Context, org id.ID, name string, cause id.ID) (domain.Workspace, error) {
	if name == "" {
		name = DefaultName
	}
	space, err := domain.New(s.ids.NewID(), org, name, s.clock.Now())
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("workspace: provision: %w", err)
	}

	// The caller owns the causal chain. A subscriber installs a provenance
	// derived from the event it is handling; minting one here would silently
	// start a new chain and the journal could no longer join what one act did.
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, s.ids)
	}
	tenanted, err := prov.WithTenant(space.ID.String())
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("workspace: provision: %w", err)
	}
	created, err := events.New(s.ids, s.clock, domain.EventWorkspaceCreated,
		domain.SubjectKind+":"+space.ID.String(), tenanted,
		domain.Created{WorkspaceID: space.ID.String(), OrgID: org.String()})
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("workspace: provision: %w", err)
	}

	space.SourceEvent = cause

	if err := s.repo.Create(ctx, space); err != nil {
		return domain.Workspace{}, fmt.Errorf("workspace: provision: %w", err)
	}
	if err := s.publisher.Publish(ctx, created); err != nil {
		return domain.Workspace{}, fmt.Errorf("workspace: provision: %w", err)
	}
	return space, nil
}
