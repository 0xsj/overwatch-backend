package query

import (
	"context"
	"io"
	"time"

	"github.com/0xsj/overwatch-backend/internal/run/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	ByID(ctx context.Context, workspace, want id.ID) (domain.Run, error)
	Page(ctx context.Context, workspace, target id.ID, before time.Time, beforeID id.ID, limit int) ([]domain.Run, error)
	Invocations(ctx context.Context, run id.ID) ([]domain.Invocation, error)
	InvocationByID(ctx context.Context, workspace, want id.ID) (domain.Invocation, error)
	Artifacts(ctx context.Context, invocation id.ID) ([]domain.Artifact, error)
	ArtifactByID(ctx context.Context, workspace, want id.ID) (domain.Artifact, error)
	Refusals(ctx context.Context, workspace, rule id.ID, limit int) ([]domain.Invocation, error)
	LatestCheckedPerSubject(ctx context.Context, workspace, target id.ID) ([]domain.Checked, error)
	InvocationChecks(ctx context.Context, workspace, target id.ID) ([]domain.InvocationCheck, error)
}

// Bytes is pkg/blob's read side. It is a separate port from the row reader
// because the two answer different questions and fail differently: a missing row
// is a record that never existed, and a missing blob is a record whose evidence
// is gone — which is a much louder problem.
type Bytes interface {
	Open(ctx context.Context, hash string) (io.ReadCloser, error)
}

const (
	DefaultPage = 50
	MaxPage     = 200
)

type Runs struct {
	reader Reader
	bytes  Bytes
}

func NewRuns(reader Reader, bytes Bytes) *Runs {
	if reader == nil || bytes == nil {
		panic("run: NewRuns query with a nil dependency")
	}
	return &Runs{reader: reader, bytes: bytes}
}

func (r *Runs) ByID(ctx context.Context, workspace, want id.ID) (domain.Run, error) {
	if workspace.IsZero() || want.IsZero() {
		return domain.Run{}, domain.ErrIDRequired
	}
	return r.reader.ByID(ctx, workspace, want)
}

// Page is keyset over `(started_at, id)` and answers NO TOTAL. The list only
// grows, an offset walks past rows inserted since, and a count is a second query
// whose answer is stale before it is rendered.
func (r *Runs) Page(ctx context.Context, workspace, target id.ID,
	before time.Time, beforeID id.ID, limit int) ([]domain.Run, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	if limit <= 0 {
		limit = DefaultPage
	}
	if limit > MaxPage {
		limit = MaxPage
	}
	return r.reader.Page(ctx, workspace, target, before, beforeID, limit)
}

// Detail is one run and everything inside it. It is one call because the
// Executions view draws the whole graph at once, and three round trips to fill
// one screen is three chances for the parts to disagree.
type Detail struct {
	Run         domain.Run
	Invocations []domain.Invocation
	Artifacts   map[id.ID][]domain.Artifact
}

func (r *Runs) Detail(ctx context.Context, workspace, want id.ID) (Detail, error) {
	found, err := r.ByID(ctx, workspace, want)
	if err != nil {
		return Detail{}, err
	}
	invocations, err := r.reader.Invocations(ctx, found.ID)
	if err != nil {
		return Detail{}, err
	}
	out := Detail{Run: found, Invocations: invocations, Artifacts: map[id.ID][]domain.Artifact{}}
	for _, i := range invocations {
		artifacts, err := r.reader.Artifacts(ctx, i.ID)
		if err != nil {
			return Detail{}, err
		}
		if len(artifacts) > 0 {
			out.Artifacts[i.ID] = artifacts
		}
	}
	return out, nil
}

// Open hands back the bytes an artifact names, having first read the ROW —
// which is the tenancy check. Reaching pkg/blob with a hash alone would let
// anybody who guessed a content address read another client's evidence, and a
// content address is exactly the kind of thing that leaks.
func (r *Runs) Open(ctx context.Context, workspace, artifact id.ID) (domain.Artifact, io.ReadCloser, error) {
	found, err := r.reader.ArtifactByID(ctx, workspace, artifact)
	if err != nil {
		return domain.Artifact{}, nil, err
	}
	body, err := r.bytes.Open(ctx, found.Hash)
	if err != nil {
		return domain.Artifact{}, nil, err
	}
	return found, body, nil
}

// Refusals answers which spawns a scope rule refused. decisions/0030 keeps a
// rule append-only because three surfaces cite its id; this is the read that
// makes keeping it worth the rows.
func (r *Runs) Refusals(ctx context.Context, workspace, rule id.ID, limit int) ([]domain.Invocation, error) {
	if workspace.IsZero() || rule.IsZero() {
		return nil, domain.ErrIDRequired
	}
	if limit <= 0 {
		limit = DefaultPage
	}
	if limit > MaxPage {
		limit = MaxPage
	}
	return r.reader.Refusals(ctx, workspace, rule, limit)
}

// Invocation and Artifact are point lookups for LINEAGE. Both take the
// engagement and check it — an observation reached its ids through a row the
// caller could already see, and these must not become a way around that.
func (r *Runs) Invocation(ctx context.Context, workspace, want id.ID) (domain.Invocation, error) {
	if workspace.IsZero() || want.IsZero() {
		return domain.Invocation{}, domain.ErrIDRequired
	}
	return r.reader.InvocationByID(ctx, workspace, want)
}

func (r *Runs) Artifact(ctx context.Context, workspace, want id.ID) (domain.Artifact, error) {
	if workspace.IsZero() || want.IsZero() {
		return domain.Artifact{}, domain.ErrIDRequired
	}
	return r.reader.ArtifactByID(ctx, workspace, want)
}

// LatestChecked is what COVERAGE reads — decisions/0037. It is the newest
// FINISHED invocation per (check, subject), which is the only honest answer to
// "when was this asset last looked at by this check".
//
// A REFUSED invocation is deliberately absent: the scope gate said no, so
// nothing looked, and rendering a refusal as coverage would make a wall look
// like a measurement.
func (r *Runs) LatestChecked(ctx context.Context, workspace, target id.ID) ([]domain.Checked, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	return r.reader.LatestCheckedPerSubject(ctx, workspace, target)
}

// InvocationChecks answers which check each invocation belonged to. It is
// coverage's second source: a check has looked at every subject it PRODUCED as
// well as the one it was aimed at.
func (r *Runs) InvocationChecks(ctx context.Context, workspace, target id.ID) ([]domain.InvocationCheck, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	return r.reader.InvocationChecks(ctx, workspace, target)
}
