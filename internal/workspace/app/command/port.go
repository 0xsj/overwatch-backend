package command

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/workspace/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Repository interface {
	Create(ctx context.Context, w domain.Workspace) error
}

type Minter interface {
	NewID() id.ID
}

type Clock interface {
	Now() time.Time
}
