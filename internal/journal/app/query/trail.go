package query

import (
	"context"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/journal/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// ErrNoChain is NotFound, and it answers both "no such correlation" and "you may
// see none of it". Distinguishing them would confirm that an act happened.
var ErrNoChain = errors.New(errors.NotFound, "chain")

type Reader interface {
	ForCorrelation(ctx context.Context, correlation id.ID) ([]domain.Line, error)
}

// Step is one hop, as a chain view needs it.
type Step struct {
	ID          id.ID
	Action      string
	Subject     string
	Origin      string
	Actor       string
	OnBehalfOf  string
	WorkspaceID string
	Depth       int
	Attempt     int
	Decision    bool
	Causation   id.ID
	Detail      []byte
	OccurredAt  time.Time
}

type Trail struct{ reader Reader }

func NewTrail(reader Reader) *Trail {
	if reader == nil {
		panic("journal: NewTrail with a nil reader")
	}
	return &Trail{reader: reader}
}

// ForCorrelation is the chain, oldest first — it is read as a story and a story
// reads forwards.
//
// **`visible` decides each step and this package supplies none of it.** A chain
// crosses domains and no single rule covers an account, an org and a workspace,
// so the caller passes the predicate. Handing it a predicate that always returns
// true is legitimate for an operator view and is a disclosure anywhere else.
func (t *Trail) ForCorrelation(ctx context.Context, correlation id.ID,
	visible func(Step) bool) ([]Step, error) {
	if correlation.IsZero() {
		return nil, ErrNoChain
	}
	if visible == nil {
		panic("journal: ForCorrelation with no visibility predicate")
	}
	lines, err := t.reader.ForCorrelation(ctx, correlation)
	if err != nil {
		return nil, fmt.Errorf("journal: chain: %w", err)
	}

	out := make([]Step, 0, len(lines))
	for _, l := range lines {
		step := Step{
			ID:          l.ID,
			Action:      l.Action,
			Subject:     l.Subject,
			Origin:      l.Origin,
			Actor:       l.Actor,
			OnBehalfOf:  l.OnBehalfOf,
			WorkspaceID: l.WorkspaceID,
			Depth:       l.Depth,
			Attempt:     l.Attempt,
			Decision:    l.Decision,
			Causation:   l.Causation,
			Detail:      l.Detail,
			OccurredAt:  l.OccurredAt,
		}
		if !visible(step) {
			continue
		}
		out = append(out, step)
	}
	// Nothing visible and nothing existing answer identically. Saying "there is
	// a chain here you may not read" confirms that the act happened.
	if len(out) == 0 {
		return nil, ErrNoChain
	}
	return out, nil
}
