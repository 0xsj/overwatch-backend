package command

import (
	"context"
	"io"
	"time"

	"github.com/0xsj/overwatch-backend/internal/run/domain"
	"github.com/0xsj/overwatch-backend/pkg/execx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Repository interface {
	Create(ctx context.Context, r domain.Run) error
	ByID(ctx context.Context, workspace, want id.ID) (domain.Run, error)
	Finish(ctx context.Context, r domain.Run) error
	Claim(ctx context.Context, batch int) ([]domain.Run, error)

	PlanInvocation(ctx context.Context, i domain.Invocation) error
	SaveInvocation(ctx context.Context, i domain.Invocation) error
	Invocations(ctx context.Context, run id.ID) ([]domain.Invocation, error)

	AddArtifact(ctx context.Context, a domain.Artifact) error
}

// Chains is the port into `check`. It answers with run's OWN [domain.Step],
// already in TOPOLOGICAL ORDER, because `check` holds the graph and the colour
// walk — a second ordering one module away is a second answer to "what order did
// this happen in", which is the axis a run is read along.
type Chains interface {
	// Steps also answers whether the check exists and is runnable. `loud` is
	// true when ANY step's tool is loud, which is what raises the gate.
	Steps(ctx context.Context, workspace, check id.ID) (steps []domain.Step, loud bool, err error)
}

// Targets is the port into `target`. A run needs one string — what the source
// steps are seeded with — and asking for more would let this package start
// reasoning about a target's kind, which is `scope`'s job.
type Targets interface {
	Name(ctx context.Context, workspace, target id.ID) (string, error)
}

// Spawns is the port into `scope`'s gate, in run's vocabulary. The ROOT does the
// vocabulary translation, and a kind it cannot translate answers `Permitted:
// false` with no rule — decisions/0010's "nothing is in scope until a rule says
// so", failing closed.
type Spawns interface {
	// INTENSITY is not optional. `0010` qualifies a spawn rule by the tool
	// intensities it permits, and a gate asked without one answers as though
	// every rule permitted every tool.
	MaySpawn(ctx context.Context, workspace, target id.ID, kind, value, intensity string) (domain.Gate, error)
}

// Tools answers what a run needs to grade an exit code — decisions/0033 §3.
// nuclei exits 1 when it finds nothing, and the list of codes that mean success
// is the tool's, not this package's.
type Tools interface {
	Succeeded(ctx context.Context, org, tool id.ID, exit int) (bool, error)
	// MediaType is what the bytes are SAID to be, from the argv. It is never
	// sniffed: guessing from content is how a record acquires a claim nobody
	// made.
	MediaType(ctx context.Context, org, tool id.ID) (string, error)
}

// Workspaces resolves the org a workspace belongs to, because `check` and `tool`
// are org-scoped (0031) and a run is workspace-scoped. It is the one join this
// package cannot avoid.
type Workspaces interface {
	OrgOf(ctx context.Context, workspace id.ID) (id.ID, error)
}

// Spawner is pkg/execx, narrowed. It is a port rather than a direct call so the
// executor is testable without spawning processes — and so a future remote
// runner is a swap rather than a rewrite.
type Spawner interface {
	Spawn(ctx context.Context, argv []string, p execx.Policy) (execx.Result, error)
}

// Blobs is pkg/blob, narrowed to the one thing an executor does with it. It
// returns the content address, which is what the artifact row records.
type Blobs interface {
	Put(ctx context.Context, r io.Reader) (hash string, size int64, err error)
}

// Extracts is the port into `observation` — decisions/0035 §6. `run` may not
// import it (peers), so the composition root adapts it.
//
// It takes the BYTES rather than the artifact id, because the executor already
// holds them in memory and re-reading from the blob store to parse what was just
// written is a round trip for nothing.
//
// **`ObservedAt` is the invocation's start**, passed in rather than read off a
// clock, so a re-extraction cannot make an old reading look fresh.
type Extracts interface {
	Extract(ctx context.Context, in Extraction) (observations int, err error)
}

type Extraction struct {
	WorkspaceID  id.ID
	OrgID        id.ID
	InvocationID id.ID
	ArtifactID   id.ID
	ToolID       id.ID
	Body         []byte
	ObservedAt   time.Time
}

type Transactor interface {
	InTx(ctx context.Context, fn func(context.Context) error) error
}

type Minter interface{ NewID() id.ID }

type Clock interface{ Now() time.Time }
