package root

import (
	"context"
	"io"

	checkquery "github.com/0xsj/overwatch-backend/internal/check/app/query"
	rundomain "github.com/0xsj/overwatch-backend/internal/run/domain"
	scopequery "github.com/0xsj/overwatch-backend/internal/scope/app/query"
	scopedomain "github.com/0xsj/overwatch-backend/internal/scope/domain"
	targetquery "github.com/0xsj/overwatch-backend/internal/target/app/query"
	toolquery "github.com/0xsj/overwatch-backend/internal/tool/app/query"
	tooldomain "github.com/0xsj/overwatch-backend/internal/tool/domain"
	workspacequery "github.com/0xsj/overwatch-backend/internal/workspace/app/query"
	"github.com/0xsj/overwatch-backend/pkg/blob"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/execx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// This file is where `run` meets the five domains it may not import. Every one
// of them is a peer, `U1` refuses the edge, and the composition root is the one
// place two domains are allowed to know about each other.

// chains resolves a check into run's own ordered steps. The ORDER comes from
// `check`, which holds the graph — a second topological sort one module away is
// a second answer to "what order did this happen in", which is the axis a run is
// read along.
type chains struct {
	checks     *checkquery.Checks
	tools      *toolquery.Tools
	workspaces *workspacequery.Workspaces
}

func (c chains) Steps(ctx context.Context, workspace, check id.ID) ([]rundomain.Step, bool, error) {
	org, _, err := c.workspaces.OrgOf(ctx, workspace)
	if err != nil {
		return nil, false, err
	}
	chain, err := c.checks.Chain(ctx, org, check)
	if err != nil {
		return nil, false, err
	}
	if chain.Empty() {
		// The HUMAN check. `READ BY YOU` has no chain because a person reading
		// is the whole act, and there is nothing here to spawn.
		return nil, false, rundomain.ErrChainEmpty
	}
	ordered, err := chain.Order()
	if err != nil {
		return nil, false, err
	}
	fed := map[id.ID]bool{}
	for _, f := range chain.Flows {
		fed[f.To] = true
	}

	steps := make([]rundomain.Step, 0, len(ordered))
	loud := false
	for _, s := range ordered {
		t, err := c.tools.ByID(ctx, org, s.ToolID)
		if err != nil {
			return nil, false, err
		}
		if t.Intensity == tooldomain.IntensityLoud {
			loud = true
		}
		steps = append(steps, rundomain.Step{
			StepID: s.ID, ToolID: s.ToolID, Template: t.Argv,
			Source: !fed[s.ID], Loud: t.Intensity == tooldomain.IntensityLoud,
			Kind: spawnKindOf(t), Intensity: t.Intensity.String(),
		})
	}
	return steps, loud, nil
}

// spawnKindOf is what is LEFT of a translation function after decisions/0034.
//
// It used to map `tool.Feed` into `scope.Kind` across two vocabularies that
// disagreed — `domain` became `host`, and `asn`, `url` and `finding` had no
// answer at all and fell through to empty, which failed closed and meant a
// URL-consuming tool could never be permitted by any rule that could be written.
//
// The two lists are now one (`scope` holds the canonical copy), so the spelling
// crosses the boundary unchanged and this is a lookup rather than a translation.
// **The only value with no scope kind is `finding`**, which is correct: a
// finding is not a fragment, nothing is spawned against one, and asking the gate
// about it answers empty and refuses.
//
// The SOURCE-STEP fallback stays and is the one piece of real logic here: a
// source consumes nothing and is seeded from the target, so the gate is asked
// about what it would PRODUCE.
//
// **AND THAT IS NOT QUITE THE RIGHT QUESTION.** At a source step the thing being
// touched is the TARGET, not the tool's output, so the gate should be asked
// about the target's own fragment kind — which does not exist: `target.Kind` is
// `organisation | person`, the root ENTITY kind, and a target carries no seed
// fragment kind at all.
//
// Today it works because every target's name is host-shaped and the rules people
// write name the same string. It will be wrong the first time a target is an ASN
// and a source tool produces hosts. Predates decisions/0034 — 0034 only made it
// visible by letting `asn` and `url` be asked about — and it is owed, in the
// flows draft.
func spawnKindOf(t tooldomain.Tool) string {
	feed := t.Consumes
	if feed == tooldomain.FeedNone {
		feed = t.Produces
	}
	if feed == tooldomain.FeedNone || feed == tooldomain.FeedFinding {
		return ""
	}
	return feed.String()
}

// targets hands run the one string it needs. Asking for more would let `run`
// reason about a target's kind, which is `scope`'s job.
type targets struct{ targets *targetquery.Targets }

func (t targets) Name(ctx context.Context, workspace, target id.ID) (string, error) {
	found, err := t.targets.ByID(ctx, workspace, target)
	if err != nil {
		return "", err
	}
	return found.Name, nil
}

// spawns is the scope gate, translated into run's vocabulary.
type spawns struct{ rules *scopequery.Rules }

func (s spawns) MaySpawn(ctx context.Context, workspace, target id.ID,
	kind, value, intensity string) (rundomain.Gate, error) {
	if kind == "" {
		// FAIL CLOSED. Nothing translated, so nothing can be asked, so nothing
		// is permitted — and the reason says which vocabulary ran out rather
		// than pretending a rule decided.
		return rundomain.Gate{
			Reason: "no scope vocabulary for what this tool touches, so no rule can permit it",
		}, nil
	}
	parsed, err := scopedomain.ParseKind(kind)
	if err != nil {
		return rundomain.Gate{Reason: err.Error()}, nil
	}
	// THE INTENSITY IS PART OF THE QUESTION — 0010: "a range in scope for
	// passive collection is not thereby in scope for a loud scan." Asking
	// without it made a rule permitting only `passive` permit a `loud` tool,
	// which is a hole in the scope model rather than a rough edge.
	//
	// The two packages spell these enums identically and are forbidden from
	// sharing the type, so an unparseable intensity FAILS CLOSED rather than
	// defaulting — a default here would silently be `passive`, which is the
	// most permissive value and therefore the worst possible guess.
	loudness, err := scopedomain.ParseIntensity(intensity)
	if err != nil {
		return rundomain.Gate{
			Reason: "this tool's intensity is not one scope can qualify, so no rule can permit it",
		}, nil
	}
	decision, err := s.rules.Decide(ctx, workspace, target, scopedomain.GateSpawn,
		scopedomain.Candidate{Kind: parsed, Value: value, Intensity: loudness})
	if err != nil {
		return rundomain.Gate{}, err
	}
	switch decision.Verdict {
	case scopedomain.Permitted:
		return rundomain.Gate{Permitted: true, Rule: decision.Winner.ID}, nil
	case scopedomain.Refused:
		return rundomain.Gate{
			Rule:   decision.Winner.ID,
			Reason: "rule " + decision.Winner.Pattern + " excludes " + value,
		}, nil
	default:
		// NOT IN SCOPE — no rule matched at all. There is no rule to cite, and
		// that is a different fact from one having excluded it.
		return rundomain.Gate{
			Reason: "nothing in this target's scope permits " + value,
		}, nil
	}
}

// toolbox already satisfies check's port; these two methods add run's.
func (t toolbox) Succeeded(ctx context.Context, org, tool id.ID, exit int) (bool, error) {
	found, err := t.tools.ByID(ctx, org, tool)
	if err != nil {
		// A tool archived or deleted since the plan was written must not turn
		// every exit code into a success. Fail the invocation instead.
		if errors.IsKind(err, errors.NotFound) {
			return false, nil
		}
		return false, err
	}
	return found.Succeeded(exit), nil
}

// MediaType is read off the ARGV, never sniffed from the bytes. A tool invoked
// with `-json` produces JSON; that is a fact about the command, and guessing it
// from content is how a record acquires a claim nobody made.
func (t toolbox) MediaType(ctx context.Context, org, tool id.ID) (string, error) {
	found, err := t.tools.ByID(ctx, org, tool)
	if err != nil {
		if errors.IsKind(err, errors.NotFound) {
			return "", nil
		}
		return "", err
	}
	for _, field := range splitFields(found.Argv) {
		switch field {
		case "-json", "--json", "-jsonl", "--jsonl", "-oJ", "-json-export":
			return "application/json", nil
		}
	}
	return "text/plain", nil
}

func splitFields(s string) []string {
	out := make([]string, 0, 8)
	start := -1
	for n, r := range s {
		if r == ' ' || r == '\t' || r == '\n' {
			if start >= 0 {
				out = append(out, s[start:n])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = n
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

// orgOf resolves the org, which is the one join `run` cannot avoid: a run is
// workspace-scoped and the check and tool it names are org-scoped — 0031.
type orgOf struct{ reads *workspacequery.Workspaces }

// OrgOf drops the `closed` half of the answer deliberately. Whether an
// engagement is closed is the ROUTE's question — decisions/0027's two helpers —
// and the executor works on runs that were already authorised when they were
// planned. Refusing here would strand a run whose engagement closed mid-scan
// with processes already spawned and nothing to record them against.
func (w orgOf) OrgOf(ctx context.Context, workspace id.ID) (id.ID, error) {
	org, _, err := w.reads.OrgOf(ctx, workspace)
	return org, err
}

// blobs narrows pkg/blob to the two calls run makes, and turns its Ref into the
// string the artifact row stores. The domain never learns what a Ref is.
type blobs struct{ store *blob.Store }

func (b blobs) Put(ctx context.Context, r io.Reader) (string, int64, error) {
	info, err := b.store.Put(ctx, r)
	if err != nil {
		return "", 0, err
	}
	return info.Ref.String(), info.Size, nil
}

func (b blobs) Open(ctx context.Context, hash string) (io.ReadCloser, error) {
	ref, err := blob.ParseRef(hash)
	if err != nil {
		return nil, err
	}
	return b.store.Open(ctx, ref)
}

// execxSpawner is pkg/execx behind run's port. It is a struct with no fields
// rather than a function value because the port may grow a second method — a
// dry-run, a remote runner — and a func cannot.
type execxSpawner struct{}

func (execxSpawner) Spawn(ctx context.Context, argv []string, p execx.Policy) (execx.Result, error) {
	return execx.Spawn(ctx, argv, p)
}

// schedulable assembles what COULD run, out of three peers — decisions/0038 §4.
//
// **Driven from checks**, which is the smallest set: a firm has six, not six
// thousand targets. The fan-out is then one workspace read per org and one
// target read per workspace.
//
// **That is N+1 and it is accepted rather than hidden.** At the scale this runs
// today it is a handful of queries a minute; at a thousand engagements it is
// not, and the fix is a materialised due-list — which is exactly the shape
// `0037` refused for coverage, and for the same reason: four unrelated writes
// invalidate it.
type schedulable struct {
	checks     *checkquery.Checks
	workspaces *workspacequery.Workspaces
	targets    *targetquery.Targets
}

func (s schedulable) Pairs(ctx context.Context) ([]rundomain.Pair, error) {
	checks, err := s.checks.Schedulable(ctx)
	if err != nil {
		return nil, err
	}
	if len(checks) == 0 {
		return nil, nil
	}

	// One workspace read per ORG and one target read per WORKSPACE, both cached
	// across the tick — a firm with six checks in one org would otherwise read
	// its workspaces six times.
	spaces := map[id.ID][]id.ID{}
	subjects := map[id.ID][]id.ID{}

	out := make([]rundomain.Pair, 0, len(checks))
	for _, check := range checks {
		// DOES IT HAVE A CHAIN? A chainless non-human check is enabled, has an
		// interval, and cannot run — and a failed start never updates
		// `last_started`, so retrying it forever holds the head of the queue.
		// One chain read per schedulable check, which is why the walk is driven
		// from checks in the first place.
		chain, err := s.checks.Chain(ctx, check.OrgID, check.ID)
		if err != nil {
			return nil, err
		}
		if chain.Empty() {
			continue
		}
		workspaces, ok := spaces[check.OrgID]
		if !ok {
			// LIVE workspaces only. A closed engagement cannot be acted in —
			// `0027` — so scheduling a run into one would produce a refusal
			// every interval forever.
			found, err := s.workspaces.InOrg(ctx, check.OrgID)
			if err != nil {
				return nil, err
			}
			workspaces = make([]id.ID, 0, len(found))
			for _, w := range found {
				workspaces = append(workspaces, w.ID)
			}
			spaces[check.OrgID] = workspaces
		}

		for _, workspace := range workspaces {
			targets, ok := subjects[workspace]
			if !ok {
				// LIVE targets only, for the reason above one level down.
				found, err := s.targets.Live(ctx, workspace)
				if err != nil {
					return nil, err
				}
				targets = make([]id.ID, 0, len(found))
				for _, t := range found {
					targets = append(targets, t.ID)
				}
				subjects[workspace] = targets
			}
			for _, target := range targets {
				out = append(out, rundomain.Pair{
					WorkspaceID: workspace, TargetID: target, CheckID: check.ID,
					CheckName: check.Name, Interval: check.Interval,
					Enabled: check.Enabled, Human: check.Human, HasChain: true,
				})
			}
		}
	}
	return out, nil
}
