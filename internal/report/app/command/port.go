package command

import (
	"context"
	"io"
	"time"

	"github.com/0xsj/overwatch-backend/internal/report/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Repository interface {
	Create(ctx context.Context, r domain.Report) error
	ByID(ctx context.Context, workspace, want id.ID) (domain.Report, error)
	Save(ctx context.Context, r domain.Report) error

	AddRevision(ctx context.Context, rev domain.Revision) error
	NextRevision(ctx context.Context, report id.ID) (int, error)
}

// Sections supplies the data behind each report section. The composition root
// adapts these report-owned types from the domains that hold the source records.
// Every method must return the complete selected set or an error; interactive
// page limits must not silently shorten an issued document.
type Sections interface {
	// Scope is the METHOD section: what the engagement was allowed to touch. A
	// superseded rule is still returned — 0030 keeps it forever precisely so a
	// citation made at the time still resolves.
	Scope(ctx context.Context, workspace, target id.ID) ([]domain.Rule, error)

	Attribution(ctx context.Context, workspace, target id.ID) ([]domain.Claim, error)
	Assets(ctx context.Context, workspace, target id.ID) ([]domain.Asset, error)

	// Findings answers the board. Dismissed ones are EXCLUDED from the rows and
	// counted separately, because "1 dismissed and excluded" is a sentence and a
	// silently shorter list is not.
	Findings(ctx context.Context, workspace, target id.ID) (open []domain.Finding, dismissed int, err error)

	Coverage(ctx context.Context, workspace, target id.ID) ([]domain.Cell, error)

	// Notes is the ENGAGEMENT SUMMARY — the subjectless notes only.
	// `0043` §3: a section that renders notes is a record of who said what and
	// when, which is the standard the other seven meet.
	Notes(ctx context.Context, workspace, target id.ID) ([]domain.Note, error)

	// Invocations is the SCOPE PROOF — what did not run.
	Invocations(ctx context.Context, workspace, target id.ID) ([]domain.Invocation, error)
	Artifacts(ctx context.Context, workspace, target id.ID) ([]domain.Artifact, error)
}

// Blobs is `pkg/blob`, narrowed. An issued report is stored exactly the way an
// artifact is, and for the same reason: the hash is what makes "the client
// received exactly this" checkable rather than asserted.
type Blobs interface {
	Put(ctx context.Context, r io.Reader) (hash string, size int64, err error)
}

type Transactor interface {
	InTx(ctx context.Context, fn func(context.Context) error) error
	// InSnapshot must provide repeatable reads across all section adapters.
	InSnapshot(ctx context.Context, fn func(context.Context) error) error
}

type Minter interface{ NewID() id.ID }

type Clock interface{ Now() time.Time }
