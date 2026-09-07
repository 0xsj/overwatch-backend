package query

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/observation/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Lineage is PRODUCT.md's central claim, as a query:
//
//	"Every value walks backwards to the parser version, the raw bytes, the
//	 exact command, and the scope rule that allowed the command to run."
//
// Four steps, and each one can be MISSING for a reason worth stating rather than
// rendering as an error:
//
//	the mapping   gone only if a version was deleted, which nothing does
//	the artifact  gone only if the row was removed; the BYTES are never deleted
//	the invocation always present — an observation cannot exist without one
//	the rule      ABSENT when nothing refused and nothing had to permit by name
//
// The last is the interesting one. A spawn that was permitted cites the rule
// that permitted it; a run in a workspace whose rules were later superseded
// still cites the superseded row, which is why `0030` keeps it.
type Lineage struct {
	Observation domain.Observation

	Mapping    *MappingStep
	Artifact   *ArtifactStep
	Invocation *InvocationStep
	Rule       *RuleStep
}

// MappingStep is the PARSER VERSION — what read this value, and how.
type MappingStep struct {
	MappingID  id.ID
	Field      string
	Expression string
	Version    int
	State      string
}

// ArtifactStep is the RAW BYTES — by hash, so a reader can re-check them.
type ArtifactStep struct {
	ArtifactID id.ID
	Stream     string
	Hash       string
	Bytes      int64
	Truncated  bool
}

// InvocationStep is the EXACT COMMAND, verbatim.
type InvocationStep struct {
	InvocationID id.ID
	RunID        id.ID
	ToolID       id.ID
	Argv         []string
	Binary       string
	Phase        string
	ExitCode     *int
	StartedAt    time.Time
}

// RuleStep is WHY THE COMMAND WAS ALLOWED TO RUN.
type RuleStep struct {
	RuleID     id.ID
	Pattern    string
	Polarity   string
	Gate       string
	Superseded bool
}

// Mappings, Runs and Rules are the three ports this projection declares. Each
// answers nil-and-no-error when the step is genuinely absent, because a missing
// step is a fact about the lineage rather than a failure to read it.
type Mappings interface {
	Step(ctx context.Context, org, mapping id.ID) (*MappingStep, error)
}

type Runs interface {
	Steps(ctx context.Context, workspace, invocation, artifact id.ID) (*InvocationStep, *ArtifactStep, id.ID, error)
}

type Rules interface {
	Step(ctx context.Context, workspace, rule id.ID) (*RuleStep, error)
}

// Orgs resolves the org a workspace belongs to, because a mapping is org-scoped
// (0031) and an observation is workspace-scoped. The same join `run` cannot
// avoid, for the same reason.
type Orgs interface {
	OrgOf(ctx context.Context, workspace id.ID) (id.ID, error)
}
