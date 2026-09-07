package domain

import (
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Step is one node of a chain: a tool, and where somebody put it.
type Step struct {
	ID      id.ID
	CheckID id.ID
	ToolID  id.ID

	// Pin is where a person dragged this node, and PinnedAt says whether they
	// ever did. A drag is a constraint somebody stated, so an unpinned step is
	// laid out and a pinned one is honoured — which means (0, 0) has to be
	// distinguishable from "never moved", and a second field is the only way.
	X, Y   int
	Pinned bool
}

// Flow is data moving from one step to another, and it is the ONLY edge kind
// here.
//
// Worth stating because the entity canvas has two and only one of them is a
// claim — decisions/0003. Nothing in a chain is a claim about the world: an edge
// means "this tool's output was fed to that tool", which is a thing that
// happened to bytes rather than something anybody asserted.
type Flow struct {
	CheckID id.ID
	From    id.ID
	To      id.ID
}

// Chain is a check's steps and flows together, because neither is valid alone:
// a flow names two steps, and validating one without the other is validating
// half a graph.
type Chain struct {
	Steps []Step
	Flows []Flow
}

// Empty is the human check — `READ BY YOU` has no chain at all. It is not an
// error and it is not an unfinished check.
func (c Chain) Empty() bool { return len(c.Steps) == 0 }

// Validate holds the three rules a chain owes. It does NOT check that an edge is
// type-legal — that the upstream tool's `produces` is the downstream's
// `consumes` — because this package cannot see `tool` and must not: the two are
// peers. decisions/0032 leaves that validation unbuilt rather than approximated,
// on the ground that nothing runs yet and a check nobody executes cannot be
// wrong about what it would feed.
func (c Chain) Validate() error {
	known := make(map[id.ID]bool, len(c.Steps))
	for _, s := range c.Steps {
		if s.ID.IsZero() {
			return ErrIDRequired
		}
		if s.ToolID.IsZero() {
			return ErrToolRequired
		}
		known[s.ID] = true
	}

	seen := make(map[[2]id.ID]bool, len(c.Flows))
	out := make(map[id.ID][]id.ID, len(c.Steps))
	for _, f := range c.Flows {
		if !known[f.From] || !known[f.To] {
			return ErrStepUnknown
		}
		if f.From == f.To {
			return ErrFlowToSelf
		}
		edge := [2]id.ID{f.From, f.To}
		if seen[edge] {
			return ErrFlowDuplicate
		}
		seen[edge] = true
		out[f.From] = append(out[f.From], f.To)
	}
	return acyclic(known, out)
}

// acyclic is a depth-first walk with three colours. A cycle is refused rather
// than tolerated because a chain is executed by walking it, and a runner that
// meets one either loops forever or stops somewhere arbitrary — and the second
// is worse, because it produces a run that looks complete.
func acyclic(known map[id.ID]bool, out map[id.ID][]id.ID) error {
	const (
		white = 0 // unvisited
		grey  = 1 // on the current path
		black = 2 // finished
	)
	colour := make(map[id.ID]int, len(known))

	var walk func(id.ID) error
	walk = func(at id.ID) error {
		colour[at] = grey
		for _, next := range out[at] {
			switch colour[next] {
			case grey:
				return ErrChainCyclic
			case white:
				if err := walk(next); err != nil {
					return err
				}
			}
		}
		colour[at] = black
		return nil
	}

	for step := range known {
		if colour[step] == white {
			if err := walk(step); err != nil {
				return err
			}
		}
	}
	return nil
}

// Sources are the steps nothing feeds. They are seeded from the target's scope
// rather than from another tool, and a chain with steps and no source can never
// start — but that is the RUNNER's complaint and not this package's, because a
// half-drawn graph in an editor is a normal thing to save.
func (c Chain) Sources() []Step {
	fed := make(map[id.ID]bool, len(c.Flows))
	for _, f := range c.Flows {
		fed[f.To] = true
	}
	out := make([]Step, 0, len(c.Steps))
	for _, s := range c.Steps {
		if !fed[s.ID] {
			out = append(out, s)
		}
	}
	return out
}

// Usage names a check that runs a tool. It is what a caller about to archive a
// tool needs — the ids alone would make the refusal unreadable, and the whole
// point of naming them is that the fix is the reader's.
type Usage struct {
	CheckID id.ID
	Name    string
}

// Order is the chain in the sequence a runner must walk it: every step after
// everything that feeds it. It exists here rather than in `run` because this
// package already holds the graph and the colour walk, and a second
// implementation one module away is a second answer to "what order did this
// happen in" — which is the axis a run is read along.
//
// It answers ErrChainCyclic on a cycle rather than an arbitrary order, so a
// caller cannot walk a graph [Chain.Validate] would have refused.
func (c Chain) Order() ([]Step, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	by := make(map[id.ID]Step, len(c.Steps))
	for _, s := range c.Steps {
		by[s.ID] = s
	}
	incoming := make(map[id.ID]int, len(c.Steps))
	out := make(map[id.ID][]id.ID, len(c.Steps))
	for _, f := range c.Flows {
		incoming[f.To]++
		out[f.From] = append(out[f.From], f.To)
	}

	// Seeded from Sources so the order is STABLE: Sources preserves the stored
	// order of Steps, and an unstable topological order would reshuffle a run's
	// sequence numbers between two identical plans.
	queue := make([]id.ID, 0, len(c.Steps))
	for _, s := range c.Sources() {
		queue = append(queue, s.ID)
	}
	ordered := make([]Step, 0, len(c.Steps))
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		ordered = append(ordered, by[at])
		for _, next := range out[at] {
			incoming[next]--
			if incoming[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if len(ordered) != len(c.Steps) {
		// Unreachable while Validate refuses cycles, and kept because the day
		// somebody calls this without validating, a short list is a silently
		// truncated run rather than an error.
		return nil, ErrChainCyclic
	}
	return ordered, nil
}

// Feeds answers which steps a step feeds. A runner needs it to say what did not
// arrive when a downstream step is skipped.
func (c Chain) Feeds(from id.ID) []id.ID {
	out := make([]id.ID, 0)
	for _, f := range c.Flows {
		if f.From == from {
			out = append(out, f.To)
		}
	}
	return out
}
