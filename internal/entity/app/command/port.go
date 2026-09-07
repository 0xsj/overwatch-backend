package command

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/entity/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Repository interface {
	CreateEntity(ctx context.Context, e domain.Entity) error
	EntityByID(ctx context.Context, workspace, want id.ID) (domain.Entity, error)
	RootFor(ctx context.Context, target id.ID) (domain.Entity, error)
	JudgeEntity(ctx context.Context, e domain.Entity) error

	Upsert(ctx context.Context, f domain.Fragment) (domain.Fragment, bool, error)
	FragmentByID(ctx context.Context, workspace, want id.ID) (domain.Fragment, error)
	JudgeFragment(ctx context.Context, f domain.Fragment) error
	MarkRead(ctx context.Context, f domain.Fragment) error

	Attribute(ctx context.Context, a domain.Attribution) error
	AttributionByID(ctx context.Context, workspace, want id.ID) (domain.Attribution, error)
	Decide(ctx context.Context, a domain.Attribution) error
}

// Subjects is the port into `observation`. The subscriber is woken by an event
// naming an invocation, and this is how it learns what was said.
//
// It asks for DISTINCT SUBJECTS rather than observations, because a fragment is
// a subject and pulling ten thousand rows to group them in Go would move the
// group-by out of the database for nothing.
type Subjects interface {
	ForInvocation(ctx context.Context, workspace, invocation id.ID) ([]Subject, error)
}

type Subject struct {
	Kind     string
	Value    string
	Count    int
	LastSeen time.Time
}

// Runs resolves which TARGET an invocation was against — invocation → run →
// target. It is the join `0036` names as the thing keeping attributions on the
// right client, and it is one join away from being wrong.
type Runs interface {
	// TargetOf also answers the rule that PERMITTED the invocation, because the
	// two come from the same walk and a spawn-gated fragment's attribution is
	// based on exactly that rule.
	TargetOf(ctx context.Context, workspace, invocation id.ID) (target, permittedBy id.ID, err error)
}

// Claims answers whether a subject is ATTRIBUTABLE to the target, and it asks
// WHICHEVER GATE THE KIND BELONGS TO — decisions/0036 §5.
//
// `0010` makes the two gates take disjoint kind sets, and `0009`'s targetable
// set overlaps the CLAIM gate's in only `{repo, email}`. So asking the claim
// gate about a host is asking a question that gate has no rule for, and a host
// could never be an asset — while 0009's own worked example is twelve hosts.
//
//	SPAWN-GATED kind   attributed because the RUN was aimed at this target and
//	                   permitted by a spawn rule — `permittedBy`, which the
//	                   invocation already records
//	CLAIM-GATED kind   attributed if a claim rule covers it
//
// `permittedBy` is zero when nothing permitted the spawn, which is unreachable
// today and is passed anyway: the day a manual ingest arrives it is the only
// thing between an unscoped artifact and a client's asset list.
type Claims interface {
	MayClaim(ctx context.Context, workspace, target id.ID,
		kind, value string, permittedBy id.ID) (domain.Claim, error)
}

type Minter interface{ NewID() id.ID }

type Clock interface{ Now() time.Time }
