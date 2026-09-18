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

	// Derivations answers BOTH directions — decisions/0040. The canvas draws
	// outward from a fragment and does not care which end it is on.
	FragmentFor(ctx context.Context, workspace id.ID, kind, value string) (domain.Fragment, bool, error)

	// The two canvas reads — decisions/0044. Both are BATCHED over the drawn
	// set: one query for the whole picture rather than one per node.
	RootsPerFragment(ctx context.Context, workspace id.ID, fragments []id.ID) (map[id.ID]int, error)
	DerivationsAmong(ctx context.Context, workspace id.ID, fragments []id.ID) ([]domain.Derivation, error)
	Derivations(ctx context.Context, workspace, fragment id.ID, limit int) ([]domain.Derivation, error)
	Unresolved(ctx context.Context, workspace, invocation id.ID) ([]domain.Unresolved, error)

	Assets(ctx context.Context, workspace, target id.ID, limit int) ([]domain.Asset, error)
	AllAssets(ctx context.Context, workspace, target id.ID) ([]domain.Asset, error)
	AcceptedFragmentsForTarget(ctx context.Context, workspace, target id.ID) ([]id.ID, error)
}

const (
	DefaultPage = 200
	MaxPage     = 1000
)

// Permits is the port into `scope`'s SPAWN GATE, asked at read time —
// decisions/0044 §3.
//
// **It answers "is this permitted NOW", which is a different question from the
// one the attribution answers.** A fragment is attributed because a rule
// permitted the run that found it (`0036` §5), and an attribution is never
// re-evaluated; `0030` supersedes a rule rather than editing it, so a host
// attributed in January under a rule narrowed in March is still attributed and
// is no longer permitted. That is `CLAUDE.md`'s first pair — *attributed vs
// permitted* — and this is the only place both are shown at once.
//
// It takes the WHOLE SET, because `scope` reads its live rules once and decides
// in memory: a call per node would re-read the same rules per node.
type Permits interface {
	Permitted(ctx context.Context, workspace, target id.ID, of []Subject) (map[Subject]bool, error)
}

// Subject is one thing to ask the gate about, and it is comparable so it can key
// a map.
type Subject struct {
	Kind  string
	Value string
}

type Graph struct {
	reader Reader
	scope  Permits
	// The two ports COVERAGE declares — decisions/0037 §4. They are on Graph
	// rather than a second type because a coverage report is a read over the
	// same assets, and two readers over one table is two places to filter.
	checks  Checks
	checked Checked
}

func NewGraph(reader Reader, scope Permits, checks Checks, checked Checked) *Graph {
	if reader == nil || scope == nil || checks == nil || checked == nil {
		panic("entity: NewGraph with a nil dependency")
	}
	return &Graph{reader: reader, scope: scope, checks: checks, checked: checked}
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

// AllAssets is the complete set for deliverables. Interactive lists use Assets.
func (g *Graph) AllAssets(ctx context.Context, workspace, target id.ID) ([]domain.Asset, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	return g.reader.AllAssets(ctx, workspace, target)
}

// AcceptedFragmentsForTarget includes every fragment kind attributed to this
// target. A report's findings must not inherit a workspace-wide board filter.
func (g *Graph) AcceptedFragmentsForTarget(ctx context.Context, workspace, target id.ID) ([]id.ID, error) {
	if workspace.IsZero() || target.IsZero() {
		return nil, domain.ErrIDRequired
	}
	return g.reader.AcceptedFragmentsForTarget(ctx, workspace, target)
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

// FragmentFor is the folded lookup, exposed for READERS. `finding` resolves what
// a problem is on through it, and it is deliberately the same door a derivation
// resolves its `from` through — one fold, so "the same host" means one thing
// everywhere.
//
// NOT FOUND is an ordinary answer, not an error: a scanner can name a value no
// run ever observed, and the caller records that rather than failing.
func (g *Graph) FragmentFor(ctx context.Context, workspace id.ID, kind, value string) (domain.Fragment, bool, error) {
	if workspace.IsZero() || kind == "" || value == "" {
		return domain.Fragment{}, false, nil
	}
	return g.reader.FragmentFor(ctx, workspace, kind, value)
}

// Derivations is what one fragment was read out of, and what was read out of
// it — `0003`'s second edge kind, both directions in one read.
func (g *Graph) Derivations(ctx context.Context, workspace, fragment id.ID, limit int) ([]domain.Derivation, error) {
	if workspace.IsZero() || fragment.IsZero() {
		return nil, domain.ErrIDRequired
	}
	return g.reader.Derivations(ctx, workspace, fragment, page(limit))
}

// Unresolved is what an invocation cited and nothing could be found for —
// decisions/0040 §5. It is READ rather than only written, because a count
// nobody can see is the silent drop this design refused.
func (g *Graph) Unresolved(ctx context.Context, workspace, invocation id.ID) ([]domain.Unresolved, error) {
	if workspace.IsZero() || invocation.IsZero() {
		return nil, domain.ErrIDRequired
	}
	return g.reader.Unresolved(ctx, workspace, invocation)
}

// Node is one fragment as the canvas draws it — the client's `GraphNode`, which
// carries a count and a date rather than the observations themselves.
type Node struct {
	Fragment domain.Fragment
	Edge     domain.Attribution

	// SeenElsewhere is `0044` §2: this fragment is attributed to more than one
	// ROOT in this engagement — one client's two subsidiaries sharing a host.
	//
	// **Never across workspaces.** A fragment does not exist across
	// engagements, and a flag that said otherwise would disclose that another
	// engagement exists — which `0019`'s per-client NDA argument forbids.
	SeenElsewhere bool

	// InScope is the SPAWN GATE's answer NOW, which is `CLAUDE.md`'s first pair
	// beside its other half. False on an attributed node means *attributed and
	// not permitted*: the rule that let the run find it has since been narrowed,
	// and `0030` supersedes rather than edits so the attribution stands.
	InScope bool
}

// Drawn counts what was DRAWN, not the whole graph — `0044` §4. A count
// describing the estate beside a picture of half of it is the one way this
// screen can lie, and `Canvas.Truncated` is what says the two differ.
type Drawn struct {
	Accepted    int
	Proposed    int
	Rejected    int
	Derivations int
}

// Canvas is the entity graph: a root, its fragments, and the claim on each.
//
// **The root is NOT among the nodes.** It is not a fragment — every attribution
// runs from it to one — and the client's fixture already draws it apart for
// exactly that reason.
type Canvas struct {
	Root  domain.Entity
	Nodes []Node

	// Derivations is `0003`'s SECOND EDGE KIND, and it is its own list because
	// an edge joins TWO nodes — hanging it off one would make the reader guess
	// which end they hold. Both ends are always on this canvas: an edge to a
	// node that is not drawn implies the picture is complete and the reader
	// cannot see why the line stops.
	Derivations []domain.Derivation

	Summary Drawn

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
	fragments := make([]id.ID, 0, len(edges))
	for _, edge := range edges {
		fragment, err := g.reader.FragmentByID(ctx, workspace, edge.FragmentID)
		if err != nil {
			return Canvas{}, err
		}
		out.Nodes = append(out.Nodes, Node{Fragment: fragment, Edge: edge})
		fragments = append(fragments, fragment.ID)
		switch edge.State {
		case domain.Accepted:
			out.Summary.Accepted++
		case domain.Proposed:
			out.Summary.Proposed++
		case domain.Rejected:
			out.Summary.Rejected++
		}
	}

	// BOTH BATCHED over the drawn set — one query each for the whole picture,
	// not one per node.
	roots, err := g.reader.RootsPerFragment(ctx, workspace, fragments)
	if err != nil {
		return Canvas{}, err
	}
	// THE OTHER EDGE KIND — 0003's second, and both ends are on this canvas by
	// construction because that is what the query asks for.
	out.Derivations, err = g.reader.DerivationsAmong(ctx, workspace, fragments)
	if err != nil {
		return Canvas{}, err
	}
	out.Summary.Derivations = len(out.Derivations)

	// THE SPAWN GATE, asked NOW — 0044 §3, and the only place `attributed` and
	// `permitted` are shown side by side.
	//
	// It is one call for the whole set: `scope` reads its live rules once and
	// decides in memory, so asking per node would re-read the same rules per
	// node.
	subjects := make([]Subject, 0, len(out.Nodes))
	for _, n := range out.Nodes {
		subjects = append(subjects, Subject{Kind: n.Fragment.Kind, Value: n.Fragment.Value})
	}
	permitted, err := g.scope.Permitted(ctx, workspace, root.TargetID, subjects)
	if err != nil {
		return Canvas{}, err
	}

	for n := range out.Nodes {
		// **MORE THAN ONE ROOT**, not more than one attribution: two accepted
		// claims from the SAME root is one root, and would not be "elsewhere".
		out.Nodes[n].SeenElsewhere = roots[out.Nodes[n].Fragment.ID] > 1
		out.Nodes[n].InScope = permitted[Subject{
			Kind: out.Nodes[n].Fragment.Kind, Value: out.Nodes[n].Fragment.Value,
		}]
	}
	return out, nil
}
