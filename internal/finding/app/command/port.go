package command

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/finding/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Repository interface {
	// Record is the upsert, and it answers whether this call OPENED the
	// finding — "a new problem" and "we saw it again" are two different events.
	Record(ctx context.Context, f domain.Finding) (domain.Finding, bool, error)
	ByID(ctx context.Context, workspace, want id.ID) (domain.Finding, error)
	Save(ctx context.Context, f domain.Finding) error
	SaveDetail(ctx context.Context, d domain.Detail) error
}

// Fragments is the port into `entity` — a finding is ON a fragment, and `entity`
// is a peer this package may not import.
//
// **It does not CREATE the fragment.** A finding arrives with a `matched-at`
// value the same extraction pass has already turned into a fragment, so the
// lookup is expected to hit; when it does not, the finding is skipped rather
// than hung off a fragment invented here — the same call `0040` §5 made for an
// unresolvable provenance, and for the same reason.
type Fragments interface {
	ForValue(ctx context.Context, workspace id.ID, kind, value string) (id.ID, bool, error)
}

type Transactor interface {
	InTx(ctx context.Context, fn func(context.Context) error) error
}

type Minter interface{ NewID() id.ID }

type Clock interface{ Now() time.Time }
