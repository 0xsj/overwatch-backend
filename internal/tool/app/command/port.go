package command

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/tool/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Repository takes an ORG on every method, for the reason target's takes a
// workspace: a tool read without one is another firm's, and a signature that
// cannot express the mistake is worth more than a rule about remembering.
type Repository interface {
	Create(ctx context.Context, t domain.Tool) error
	ByID(ctx context.Context, org, want id.ID) (domain.Tool, error)
	Save(ctx context.Context, t domain.Tool) error

	CreateMapping(ctx context.Context, m domain.Mapping) error
	MappingByID(ctx context.Context, org, want id.ID) (domain.Mapping, error)
	LiveMapping(ctx context.Context, tool id.ID, field string) (domain.Mapping, error)
	NextVersion(ctx context.Context, tool id.ID, field string) (int, error)
	SaveMapping(ctx context.Context, m domain.Mapping) error
}

// Transactor is here because promoting writes TWO rows — retire the incumbent,
// then promote the new one — and the unique index refuses the pair in the other
// order. Outside a transaction a crash between them leaves a field with no live
// mapping; inside one there is no between.
type Transactor interface {
	InTx(ctx context.Context, fn func(context.Context) error) error
}

type Minter interface{ NewID() id.ID }

type Clock interface{ Now() time.Time }
