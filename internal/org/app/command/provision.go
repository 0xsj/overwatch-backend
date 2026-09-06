package command

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type Service struct {
	repo      Repository
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewService(repo Repository, publisher events.Publisher, ids Minter, clock Clock) *Service {
	if repo == nil || publisher == nil || ids == nil || clock == nil {
		panic("org: NewService with a nil dependency")
	}
	return &Service{repo: repo, publisher: publisher, ids: ids, clock: clock}
}

type Provisioned struct {
	Org    domain.Org
	Member domain.Member
}

func (s *Service) Provision(ctx context.Context, owner id.ID, name string) (Provisioned, error) {
	at := s.clock.Now()

	org, err := domain.NewOrg(s.ids.NewID(), name, at)
	if err != nil {
		return Provisioned{}, fmt.Errorf("org: provision: %w", err)
	}
	member, err := domain.NewMember(s.ids.NewID(), org.ID, owner, domain.RoleOwner, at)
	if err != nil {
		return Provisioned{}, fmt.Errorf("org: provision: %w", err)
	}

	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, s.ids)
	}
	created, err := events.New(s.ids, s.clock, domain.EventOrgCreated,
		domain.SubjectKind+":"+org.ID.String(), prov,
		domain.Created{OrgID: org.ID.String(), OwnerID: owner.String()})
	if err != nil {
		return Provisioned{}, fmt.Errorf("org: provision: %w", err)
	}

	if err := s.repo.CreateOrg(ctx, org); err != nil {
		return Provisioned{}, fmt.Errorf("org: provision: %w", err)
	}
	if err := s.repo.AddMember(ctx, member); err != nil {
		return Provisioned{}, fmt.Errorf("org: provision: %w", err)
	}
	if err := s.publisher.Publish(ctx, created); err != nil {
		return Provisioned{}, fmt.Errorf("org: provision: %w", err)
	}
	return Provisioned{Org: org, Member: member}, nil
}
