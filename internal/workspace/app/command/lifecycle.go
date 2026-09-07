package command

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/workspace/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// Rename changes what an engagement is called.
//
// **It can collide.** `workspace_live_name` is unique over LIVE rows in one org,
// so renaming onto a live sibling answers ErrNameTaken — and renaming onto the
// name of a CLOSED one succeeds, because closing released it.
//
// Authorisation is the caller's: this package cannot see a grant, and says so
// the same way every other query and command here does.
func (s *Service) Rename(ctx context.Context, workspace id.ID, name string) (domain.Workspace, error) {
	held, err := s.repo.ByID(ctx, workspace)
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("workspace: rename: %w", err)
	}
	next, err := held.Rename(name, s.clock.Now())
	if err != nil {
		return domain.Workspace{}, err
	}
	if next.Name == held.Name {
		return held, nil
	}
	if err := s.repo.Save(ctx, next); err != nil {
		return domain.Workspace{}, err
	}
	return next, s.emit(ctx, domain.EventWorkspaceRenamed, next, domain.Renamed{
		WorkspaceID: next.ID.String(), OrgID: next.OrgID.String(),
		From: held.Name, To: next.Name,
	})
}

// Close ends the work and keeps the record — decisions/0027. It does NOT touch
// grants: nobody left, and the people who were on an engagement are precisely
// the ones entitled to read what happened on it afterwards.
func (s *Service) Close(ctx context.Context, workspace id.ID) (domain.Workspace, error) {
	held, err := s.repo.ByID(ctx, workspace)
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("workspace: close: %w", err)
	}
	next, err := held.Archive(s.clock.Now())
	if err != nil {
		return domain.Workspace{}, err
	}
	if err := s.repo.Save(ctx, next); err != nil {
		return domain.Workspace{}, err
	}
	return next, s.emit(ctx, domain.EventWorkspaceArchived, next, domain.Archived{
		WorkspaceID: next.ID.String(), OrgID: next.OrgID.String(),
	})
}

// Reopen brings it back.
//
// **The name is the failure worth expecting.** Closing released it, so another
// engagement may hold it now — the store answers ErrNameTaken and the caller
// renames first. Renaming silently here would change a client's record without
// saying so.
func (s *Service) Reopen(ctx context.Context, workspace id.ID) (domain.Workspace, error) {
	held, err := s.repo.ByID(ctx, workspace)
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("workspace: reopen: %w", err)
	}
	next, err := held.Reopen(s.clock.Now())
	if err != nil {
		return domain.Workspace{}, err
	}
	if err := s.repo.Save(ctx, next); err != nil {
		return domain.Workspace{}, err
	}
	return next, s.emit(ctx, domain.EventWorkspaceReopened, next, domain.Reopened{
		WorkspaceID: next.ID.String(), OrgID: next.OrgID.String(), Name: next.Name,
	})
}

// emit tenants to the workspace, so all three land on the engagement's OWN log —
// which is the log somebody reads after it closes.
func (s *Service) emit(ctx context.Context, name string, w domain.Workspace, payload any) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, s.ids)
	}
	tenanted, err := prov.WithTenant(w.ID.String())
	if err != nil {
		return fmt.Errorf("workspace: %s: %w", name, err)
	}
	e, err := events.NewDecision(s.ids, s.clock, name,
		domain.SubjectKind+":"+w.ID.String(), tenanted, payload)
	if err != nil {
		return fmt.Errorf("workspace: %s: %w", name, err)
	}
	if err := s.publisher.Publish(ctx, e); err != nil {
		return fmt.Errorf("workspace: %s: %w", name, err)
	}
	return nil
}
