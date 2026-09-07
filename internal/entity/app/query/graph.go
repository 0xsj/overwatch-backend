package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/entity/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	EntityByID(ctx context.Context, workspace, want id.ID) (domain.Entity, error)
	RootFor(ctx context.Context, target id.ID) (domain.Entity, error)
	Entities(ctx context.Context, workspace id.ID, limit int) ([]domain.Entity, error)

	FragmentByID(ctx context.Context, workspace, want id.ID) (domain.Fragment, error)
	Fragments(ctx context.Context, workspace id.ID, kind string, limit int) ([]domain.Fragment, error)

	AttributionsFor(ctx context.Context, fragment id.ID) ([]domain.Attribution, error)
	AttributionsOn(ctx context.Context, entity id.ID, state string, limit int) ([]domain.Attribution, error)

	Assets(ctx context.Context, workspace, target id.ID, limit int) ([]domain.Asset, error)
}

const (
	DefaultPage = 200
	MaxPage     = 1000
)

type Graph struct {
	reader Reader
	// The two ports COVERAGE declares — decisions/0037 §4. They are on Graph
	// rather than a second type because a coverage report is a read over the
	// same assets, and two readers over one table is two places to filter.
	checks  Checks
	checked Checked
}

func NewGraph(reader Reader, checks Checks, checked Checked) *Graph {
	if reader == nil || checks == nil || checked == nil {
		panic("entity: NewGraph with a nil dependency")
	}
	return &Graph{reader: reader, checks: checks, checked: checked}
}

func page(limit int) int {
	if limit <= 0 {
		return DefaultPage
	}
	if limit > MaxPage {
		return MaxPage
	}
	return limit
}

// Assets reads the VIEW. Every caller that means "asset" goes through here, so
// the predicate — a targetable kind with an accepted attribution to the target's
// root entity — lives in exactly one place.
func (g *Graph) Assets(ctx context.Context, workspace, target id.ID, limit int) ([]domain.Asset, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	return g.reader.Assets(ctx, workspace, target, page(limit))
}

// Fragments is EVERYTHING observed, asset or not. The difference between this
// and Assets is the whole of 0009: a fragment nothing attributed is a real
// record of something a source said, and it is not on the asset list.
func (g *Graph) Fragments(ctx context.Context, workspace id.ID, kind string, limit int) ([]domain.Fragment, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	return g.reader.Fragments(ctx, workspace, kind, page(limit))
}

func (g *Graph) Fragment(ctx context.Context, workspace, want id.ID) (domain.Fragment, error) {
	if workspace.IsZero() || want.IsZero() {
		return domain.Fragment{}, domain.ErrIDRequired
	}
	return g.reader.FragmentByID(ctx, workspace, want)
}

func (g *Graph) Entities(ctx context.Context, workspace id.ID, limit int) ([]domain.Entity, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	return g.reader.Entities(ctx, workspace, page(limit))
}

// Attributions on one fragment answers the drawer's WHY THIS IS ATTRIBUTED
// section: who claimed it, on what basis, and whether anybody has agreed.
func (g *Graph) Attributions(ctx context.Context, workspace, fragment id.ID) ([]domain.Attribution, error) {
	if _, err := g.Fragment(ctx, workspace, fragment); err != nil {
		return nil, err
	}
	return g.reader.AttributionsFor(ctx, fragment)
}

// Node is one fragment as the canvas draws it — the client's `GraphNode`, which
// carries a count and a date rather than the observations themselves.
type Node struct {
	Fragment domain.Fragment
	Edge     domain.Attribution
}

// Canvas is the entity graph: a root, its fragments, and the claim on each.
//
// **The root is NOT among the nodes.** It is not a fragment — every attribution
// runs from it to one — and the client's fixture already draws it apart for
// exactly that reason.
type Canvas struct {
	Root  domain.Entity
	Nodes []Node

	// Truncated says the limit was reached. A canvas that silently drew half a
	// graph would look like a smaller estate, which is the one way this screen
	// can lie.
	Truncated bool
}

// Around assembles the canvas for one entity. It reads the attributions and then
// their fragments, which is a read per node — acceptable at the page size this
// caps to, and the shape to revisit when a root has thousands.
func (g *Graph) Around(ctx context.Context, workspace, entity id.ID, state string, limit int) (Canvas, error) {
	root, err := g.reader.EntityByID(ctx, workspace, entity)
	if err != nil {
		return Canvas{}, err
	}
	size := page(limit)
	edges, err := g.reader.AttributionsOn(ctx, entity, state, size+1)
	if err != nil {
		return Canvas{}, err
	}
	out := Canvas{Root: root, Nodes: make([]Node, 0, len(edges))}
	if len(edges) > size {
		edges, out.Truncated = edges[:size], true
	}
	for _, edge := range edges {
		fragment, err := g.reader.FragmentByID(ctx, workspace, edge.FragmentID)
		if err != nil {
			return Canvas{}, err
		}
		out.Nodes = append(out.Nodes, Node{Fragment: fragment, Edge: edge})
	}
	return out, nil
}
