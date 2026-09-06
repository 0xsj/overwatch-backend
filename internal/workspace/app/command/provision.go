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

func (s *Service) Provision(ctx context.Context, org id.ID, name string) (domain.Workspace, error) {
	if name == "" {
		name = DefaultName
	}
	space, err := domain.New(s.ids.NewID(), org, name, s.clock.Now())
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("workspace: provision: %w", err)
	}

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

	if err := s.repo.Create(ctx, space); err != nil {
		return domain.Workspace{}, fmt.Errorf("workspace: provision: %w", err)
	}
	if err := s.publisher.Publish(ctx, created); err != nil {
		return domain.Workspace{}, fmt.Errorf("workspace: provision: %w", err)
	}
	return space, nil
}
