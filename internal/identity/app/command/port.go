package command

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/identity/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Repository interface {
	CreateAccount(ctx context.Context, a domain.Account) error
	CreateCredential(ctx context.Context, c domain.Credential) error
}

type Tenancy struct {
	OrgID       id.ID
	WorkspaceID id.ID
}

type Provisioner interface {
	Provision(ctx context.Context, owner id.ID, name string) (Tenancy, error)
}

type Transactor interface {
	InTx(ctx context.Context, fn func(context.Context) error) error
}

type Hasher interface {
	Hash(password string) (string, error)
}

type Minter interface {
	NewID() id.ID
}

type Clock interface {
	Now() time.Time
}
