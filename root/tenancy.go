package root

import (
	"context"

	identitycmd "github.com/0xsj/overwatch-backend/internal/identity/app/command"
	orgcmd "github.com/0xsj/overwatch-backend/internal/org/app/command"
	workspacecmd "github.com/0xsj/overwatch-backend/internal/workspace/app/command"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type tenancy struct {
	orgs       *orgcmd.Service
	workspaces *workspacecmd.Service
}

func (t *tenancy) Provision(ctx context.Context, owner id.ID, name string) (identitycmd.Tenancy, error) {
	org, err := t.orgs.Provision(ctx, owner, name)
	if err != nil {
		return identitycmd.Tenancy{}, err
	}
	space, err := t.workspaces.Provision(ctx, org.Org.ID, "")
	if err != nil {
		return identitycmd.Tenancy{}, err
	}
	return identitycmd.Tenancy{OrgID: org.Org.ID, WorkspaceID: space.ID}, nil
}
