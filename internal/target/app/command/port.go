package command

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/target/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Repository is the write side. Every method takes a WORKSPACE — not because a
// caller might want to filter, but because a target read without one is another
// engagement's row, and a signature that cannot express the mistake is worth
// more than a rule about remembering.
type Repository interface {
	Create(ctx context.Context, t domain.Target) error
	ByID(ctx context.Context, workspace, want id.ID) (domain.Target, error)
	Save(ctx context.Context, t domain.Target) error
}

type Minter interface{ NewID() id.ID }

type Clock interface{ Now() time.Time }
