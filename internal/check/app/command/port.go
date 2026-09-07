package command

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/check/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Repository takes an ORG on every check method, so a read without one cannot
// be written. Chain takes only the check id: a chain is reached THROUGH a check
// that was already tenant-checked, and threading the org into it would be a
// second check that can silently disagree with the first.
type Repository interface {
	Create(ctx context.Context, c domain.Check) error
	ByID(ctx context.Context, org, want id.ID) (domain.Check, error)
	Save(ctx context.Context, c domain.Check) error

	Chain(ctx context.Context, check id.ID) (domain.Chain, error)
	SaveChain(ctx context.Context, check id.ID, chain domain.Chain) error
}

// Tools is the port `check` borrows from `tool`, and it is declared HERE because
// an interface belongs to its consumer. It asks the narrowest question there is
// — does this tool exist in this org — and deliberately does not ask what the
// tool consumes or produces: validating an edge is 0032's unbuilt half, and a
// port that returns the whole tool invites doing it here by accident.
type Tools interface {
	Exists(ctx context.Context, org, tool id.ID) (bool, error)
}

// Transactor is here because saving a chain is a prune, a clear, and N inserts.
// Outside a transaction a crash in the middle leaves a check with half a graph.
type Transactor interface {
	InTx(ctx context.Context, fn func(context.Context) error) error
}

type Minter interface{ NewID() id.ID }

type Clock interface{ Now() time.Time }
