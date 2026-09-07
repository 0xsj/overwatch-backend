package query

import (
	"context"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/target/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	Live(ctx context.Context, workspace id.ID) ([]domain.Target, error)
	All(ctx context.Context, workspace id.ID) ([]domain.Target, error)
	ByID(ctx context.Context, workspace, want id.ID) (domain.Target, error)
}

// Summary is a view. It omits the version — a write-side concern — and
// `created_by`, which is a record rather than something a list needs.
type Summary struct {
	ID        id.ID
	Name      string
	Kind      domain.Kind
	Archived  bool
	CreatedAt time.Time
}

type Targets struct{ reader Reader }

func NewTargets(reader Reader) *Targets {
	if reader == nil {
		panic("target: NewTargets query with a nil reader")
	}
	return &Targets{reader: reader}
}

func (t *Targets) Live(ctx context.Context, workspace id.ID) ([]Summary, error) {
	return t.list(ctx, workspace, false)
}

func (t *Targets) All(ctx context.Context, workspace id.ID) ([]Summary, error) {
	return t.list(ctx, workspace, true)
}

func (t *Targets) ByID(ctx context.Context, workspace, want id.ID) (domain.Target, error) {
	if workspace.IsZero() || want.IsZero() {
		return domain.Target{}, domain.ErrIDRequired
	}
	return t.reader.ByID(ctx, workspace, want)
}

func (t *Targets) list(ctx context.Context, workspace id.ID, all bool) ([]Summary, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	read := t.reader.Live
	if all {
		read = t.reader.All
	}
	found, err := read(ctx, workspace)
	if err != nil {
		return nil, fmt.Errorf("target: list: %w", err)
	}
	out := make([]Summary, 0, len(found))
	for _, one := range found {
		out = append(out, Summary{
			ID: one.ID, Name: one.Name, Kind: one.Kind,
			Archived: one.Archived(), CreatedAt: one.CreatedAt,
		})
	}
	return out, nil
}
