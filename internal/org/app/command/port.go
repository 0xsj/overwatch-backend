package command

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Repository interface {
	CreateOrg(ctx context.Context, o domain.Org) error
	AddMember(ctx context.Context, m domain.Member) error
}

type Minter interface {
	NewID() id.ID
}

type Clock interface {
	Now() time.Time
}
