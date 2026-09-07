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

// Provision creates a workspace for an org, from the chain. `cause` is the event
// that asked for it and is what makes a redelivery a no-op — decisions/0017.
//
// It emits WORK and no decision. Nobody chose this: it is what happens because
// somebody registered, and an audit trail listing it is listing machinery
// rather than people — decisions/0014 and 0020.
func (s *Service) Provision(ctx context.Context, org, by id.ID, name string, cause id.ID) (domain.Workspace, error) {
	if name == "" {
		name = DefaultName
	}
	space, err := domain.New(s.ids.NewID(), org, by, name, s.clock.Now())
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

// Open is a PERSON creating an engagement, and it is the same work plus a
// decision — decisions/0020. The two events share one correlation, so the pair
// is joinable: the journal answers what happened and audit answers who chose it.
//
// It writes no grant. The opener needs `admin` on what they just made and this
// package may not touch org's tables, so org subscribes to workspace.opened and
// writes it — a fourth link on the pattern decisions/0017 established. The
// window where an admin has opened a workspace they cannot yet see is the
// registration window again, and closes the same way.
func (s *Service) Open(ctx context.Context, org, by id.ID, name string) (domain.Workspace, error) {
	if by.IsZero() {
		return domain.Workspace{}, fmt.Errorf("workspace: open: %w", domain.ErrIDRequired)
	}
	space, err := domain.New(s.ids.NewID(), org, by, name, s.clock.Now())
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("workspace: open: %w", err)
	}

	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, s.ids)
	}
	tenanted, err := prov.WithTenant(space.ID.String())
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("workspace: open: %w", err)
	}

	subject := domain.SubjectKind + ":" + space.ID.String()
	created, err := events.New(s.ids, s.clock, domain.EventWorkspaceCreated, subject, tenanted,
		domain.Created{WorkspaceID: space.ID.String(), OrgID: org.String()})
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("workspace: open: %w", err)
	}
	opened, err := events.NewDecision(s.ids, s.clock, domain.EventWorkspaceOpened, subject, tenanted,
		domain.Opened{
			WorkspaceID: space.ID.String(),
			OrgID:       org.String(),
			OpenedBy:    by.String(),
		})
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("workspace: open: %w", err)
	}

	if err := s.repo.Create(ctx, space); err != nil {
		return domain.Workspace{}, fmt.Errorf("workspace: open: %w", err)
	}
	// Both, in one call, so they land in one transaction and a crash between
	// them is not a workspace that exists with nobody recorded as opening it.
	if err := s.publisher.Publish(ctx, created, opened); err != nil {
		return domain.Workspace{}, fmt.Errorf("workspace: open: %w", err)
	}
	return space, nil
}
