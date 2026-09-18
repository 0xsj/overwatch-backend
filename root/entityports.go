package root

import (
	"context"
	"strings"
	"time"

	checkquery "github.com/0xsj/overwatch-backend/internal/check/app/query"
	checkdomain "github.com/0xsj/overwatch-backend/internal/check/domain"
	entcmd "github.com/0xsj/overwatch-backend/internal/entity/app/command"
	entquery "github.com/0xsj/overwatch-backend/internal/entity/app/query"
	entdomain "github.com/0xsj/overwatch-backend/internal/entity/domain"
	obsquery "github.com/0xsj/overwatch-backend/internal/observation/app/query"
	runquery "github.com/0xsj/overwatch-backend/internal/run/app/query"
	scopequery "github.com/0xsj/overwatch-backend/internal/scope/app/query"
	scopedomain "github.com/0xsj/overwatch-backend/internal/scope/domain"
	toolquery "github.com/0xsj/overwatch-backend/internal/tool/app/query"
	tooldomain "github.com/0xsj/overwatch-backend/internal/tool/domain"
	workspacequery "github.com/0xsj/overwatch-backend/internal/workspace/app/query"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Where `entity` meets the three domains it may not import.

// subjects is the port into `observation`. It asks for DISTINCT SUBJECTS rather
// than observations, because a fragment is a subject and pulling ten thousand
// rows to group them in Go would move the group-by out of the database.
type subjects struct{ observed *obsquery.Observations }

func (s subjects) ForInvocation(ctx context.Context, workspace, invocation id.ID) ([]entcmd.Subject, error) {
	// The invocation filter is applied after the group-by rather than inside it,
	// because `observation` exposes subjects per WORKSPACE and per invocation
	// separately and only the second is what a delivery is about.
	found, err := s.observed.ForInvocation(ctx, workspace, invocation, obsquery.MaxPage)
	if err != nil {
		return nil, err
	}
	type key struct{ kind, value string }
	held := map[key]*entcmd.Subject{}
	order := make([]key, 0, len(found))
	for _, o := range found {
		k := key{o.SubjectKind, o.SubjectValue}
		if held[k] == nil {
			held[k] = &entcmd.Subject{Kind: o.SubjectKind, Value: o.SubjectValue}
			order = append(order, k)
		}
		held[k].Count++
		if o.ObservedAt.After(held[k].LastSeen) {
			held[k].LastSeen = o.ObservedAt
		}
	}
	out := make([]entcmd.Subject, 0, len(order))
	for _, k := range order {
		out = append(out, *held[k])
	}
	return out, nil
}

// provenances is entity's port into `observation` for what each record was READ
// OUT OF — decisions/0040 §4 — and it is the one place the `from` value's KIND
// is supplied.
//
// **The kind is the TOOL's `consumes`, and it is resolved here because nothing
// else can see both ends.** `observation` holds the value and not the kind;
// `entity` may import neither `observation` nor `tool`. So the walk is
// invocation → tool → consumes, and it happens at the composition root, which is
// where two peers are allowed to meet.
//
// Reading the kind off the value's SHAPE was the alternative and it is the
// observation/fact error one level down: `a.acme.test` looks like a host and
// `192.0.2.1` looks like an ip, and a tool consuming `cidr` would have both
// guessed wrong.
//
// **The tool is read LIVE**, so a tool whose `consumes` changed between the run
// and the assembly resolves the new kind. The window is the seconds between an
// invocation finishing and its delivery landing, and it is the same trade the
// executor already makes for `Succeeded` and `MediaType`.
type provenances struct {
	observed *obsquery.Observations
	runs     *runquery.Runs
	tools    *toolquery.Tools
	spaces   *workspacequery.Workspaces
}

func (p provenances) ForInvocation(ctx context.Context, workspace, invocation id.ID) ([]entcmd.Provenance, error) {
	found, err := p.observed.ProvenanceForInvocation(ctx, workspace, invocation)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		// MOST TOOLS DECLARE NO PROVENANCE MAPPING, so this is the common path
		// and it must not cost three reads to learn nothing happened.
		return nil, nil
	}

	kind, err := p.consumesOf(ctx, workspace, invocation)
	if err != nil {
		return nil, err
	}
	if kind == "" {
		// The tool consumes nothing, or was archived since the run. A source
		// tool cannot legally hold a `derived_from` mapping (0040 §3), so
		// reaching here means the tool changed under a run that already
		// happened — and an edge whose `from` has no kind cannot be built.
		// Answering NOTHING makes those readings unresolved rather than
		// silently kind-less.
		return nil, nil
	}

	out := make([]entcmd.Provenance, 0, len(found))
	for _, one := range found {
		out = append(out, entcmd.Provenance{
			SubjectKind: one.SubjectKind, SubjectValue: one.SubjectValue,
			FromKind: kind, FromValue: one.FromValue, Label: one.Label,
			MappingID: one.MappingID, ArtifactID: one.ArtifactID,
		})
	}
	return out, nil
}

// consumesOf walks invocation → tool → consumes. It is `entity`'s second
// invocation walk beside [targetOfRun], and they are separate because they
// answer different questions and one of them is allowed to come back empty.
func (p provenances) consumesOf(ctx context.Context, workspace, invocation id.ID) (string, error) {
	one, err := p.runs.Invocation(ctx, workspace, invocation)
	if err != nil {
		return "", err
	}
	org, _, err := p.spaces.OrgOf(ctx, workspace)
	if err != nil {
		return "", err
	}
	found, err := p.tools.ByID(ctx, org, one.ToolID)
	if err != nil {
		if errors.IsKind(err, errors.NotFound) {
			return "", nil
		}
		return "", err
	}
	if found.Consumes == tooldomain.FeedNone || found.Consumes == tooldomain.FeedFinding {
		return "", nil
	}
	return found.Consumes.String(), nil
}

// targets resolves invocation → run → target. **This is the join decisions/0036
// names as the thing keeping attributions on the right client**, and it is one
// join away from being wrong: get it from the wrong place and every row is
// well-formed, the asset appears under the wrong client, and nothing in the
// schema can tell.
type targetOfRun struct{ runs *runquery.Runs }

func (t targetOfRun) TargetOf(ctx context.Context, workspace, invocation id.ID) (id.ID, id.ID, error) {
	found, err := t.runs.Invocation(ctx, workspace, invocation)
	if err != nil {
		return id.ID{}, id.ID{}, err
	}
	run, err := t.runs.ByID(ctx, workspace, found.RunID)
	if err != nil {
		return id.ID{}, id.ID{}, err
	}
	// The rule that PERMITTED the spawn. A spawn-gated fragment is attributed
	// because the run that found it was aimed at this target and this rule let
	// it — decisions/0036 §5.
	return run.TargetID, found.PermitRule, nil
}

// claims is scope's SECOND gate — 0010. The spawn gate asks what a tool may
// touch; this asks what the engagement covers, and it is what decides whether a
// fragment is an asset.
type claims struct{ rules *scopequery.Rules }

func (c claims) MayClaim(ctx context.Context, workspace, target id.ID,
	kind, value string, permittedBy id.ID) (entdomain.Claim, error) {
	parsed, err := scopedomain.ParseKind(kind)
	if err != nil {
		// A kind scope has no word for cannot be attributed. FAILS CLOSED, and
		// `root/vocabulary_test.go` is what stops this being reachable.
		return entdomain.Claim{}, nil
	}

	// THE GATE THE KIND BELONGS TO — decisions/0036 §5. The two gates take
	// disjoint kind sets (0010), so there is exactly one right question to ask
	// and asking the other one always answers no.
	if parsed.Gate() == scopedomain.GateSpawn {
		// A host, ip, cidr, asn or url is attributed because THE RUN THAT FOUND
		// IT was aimed at this target and a spawn rule permitted it. That is
		// `attributed` and `permitted` being the same rule answering two
		// questions, which is why the drawer shows it twice rather than once.
		if permittedBy.IsZero() {
			return entdomain.Claim{}, nil
		}
		rule, err := c.rules.ByID(ctx, workspace, permittedBy)
		if err != nil {
			if errors.IsKind(err, errors.NotFound) {
				return entdomain.Claim{}, nil
			}
			return entdomain.Claim{}, err
		}
		return entdomain.Claim{
			Covered: true, Rule: rule.ID,
			Basis: "found by a run this engagement's scope rule " + rule.Pattern + " permitted",
		}, nil
	}

	// A repo, an email, a person: nothing spawns against them, so the question
	// is the CLAIM gate's. NO INTENSITY — 0010: "it is absent on a claim rule,
	// because there are no processes on that gate."
	decision, err := c.rules.Decide(ctx, workspace, target, scopedomain.GateClaim,
		scopedomain.Candidate{Kind: parsed, Value: value})
	if err != nil {
		return entdomain.Claim{}, err
	}
	if decision.Verdict != scopedomain.Permitted {
		// Refused, or nothing matched. Both mean the engagement does not cover
		// it — the fragment exists and is not an asset.
		return entdomain.Claim{}, nil
	}
	return entdomain.Claim{
		Covered: true, Rule: decision.Winner.ID,
		// For a rule, THE BASIS IS THE SCOPE RULE ITSELF — the screens draft
		// says so, and it is what the drawer renders under "on what basis".
		Basis: "scope rule " + decision.Winner.Pattern + " covers this engagement",
	}, nil
}

// spawnPermits is the canvas's port into `scope`'s SPAWN GATE — decisions/0044
// §3, and it is where `CLAUDE.md`'s first pair gets both halves on one screen.
//
// **It reads the live rules ONCE and decides in memory.** `scope.Decide` is a
// pure function over a rule slice, so a call per node would re-read the same
// rules per node — this is one read and N decisions.
type spawnPermits struct{ rules *scopequery.Rules }

func (s spawnPermits) Permitted(ctx context.Context, workspace, target id.ID,
	of []entquery.Subject) (map[entquery.Subject]bool, error) {
	out := make(map[entquery.Subject]bool, len(of))
	if len(of) == 0 || target.IsZero() {
		// No target means no scope to be in or out of — a root with no target
		// row, which `0029` allows for a graph built before one existed. Every
		// node reads NOT in scope, which is the honest answer rather than a
		// blanket yes.
		return out, nil
	}
	live, err := s.rules.Live(ctx, workspace, target)
	if err != nil {
		return nil, err
	}
	for _, one := range of {
		kind, err := scopedomain.ParseKind(one.Kind)
		if err != nil {
			// A kind scope has no word for cannot be permitted by any rule.
			// FAILS CLOSED, and `root/vocabulary_test.go` is what stops this
			// being reachable.
			out[one] = false
			continue
		}
		// **PERMITTED FOR ANYTHING.** `0010` qualifies a spawn rule by the
		// intensities it allows, and the facet asks whether the engagement may
		// touch this at all — so a host permitted only for `loud` is in scope,
		// and one every rule excludes is not. Three pure decisions over an
		// already-loaded slice; the cost is nothing.
		for _, intensity := range []scopedomain.Intensity{
			scopedomain.IntensityPassive, scopedomain.IntensityLight, scopedomain.IntensityLoud,
		} {
			decision := scopedomain.Decide(live, scopedomain.GateSpawn, scopedomain.Candidate{
				Kind: kind, Value: one.Value, Intensity: intensity,
			})
			if decision.Verdict == scopedomain.Permitted {
				out[one] = true
				break
			}
		}
	}
	return out, nil
}

// coverageChecks is the port into `check`. It resolves the one thing coverage
// needs that a check row alone does not carry: the org, because checks are
// org-scoped (0031) and coverage is per engagement.
type coverageChecks struct {
	checks     *checkquery.Checks
	workspaces *workspacequery.Workspaces
}

func (c coverageChecks) ForCoverage(ctx context.Context, workspace id.ID) ([]entquery.Check, error) {
	org, _, err := c.workspaces.OrgOf(ctx, workspace)
	if err != nil {
		return nil, err
	}
	found, err := c.checks.ForOrg(ctx, org, false)
	if err != nil {
		return nil, err
	}
	out := make([]entquery.Check, 0, len(found))
	for _, check := range found {
		out = append(out, entquery.Check{
			ID: check.ID, Name: check.Name, Question: check.Question,
			AppliesTo: checkdomain.Names(check.AppliesTo),
			Interval:  check.Interval,
			// THE FLAG, not `chain.Empty()`. An unfinished check is chainless
			// too, and deriving it reported every asset fresh the moment one was
			// read — decisions/0037 §3, corrected by the first live grid.
			Human: check.Human,
		})
	}
	return out, nil
}

// coverageChecked is the port into `run`. The two Checked types are identical
// and live in two packages because those packages are peers — the same
// duplication every port in this file carries.
type coverageChecked struct {
	runs     *runquery.Runs
	observed *obsquery.Observations
}

// LatestPerSubject is the UNION OF TWO TRUE FACTS, and the composition root is
// where they meet because they live in two domains.
//
//	AIMED AT    an invocation of this check was pointed at this subject.
//	            This is what keeps `never checked` and `found nothing` apart:
//	            a check that ran and found nothing produces no observation
//	            and would otherwise read as never run.
//	PRODUCED    an observation from an invocation of this check is ABOUT this
//	            subject. This catches DISCOVERY — a tool aimed at a seed finds
//	            subjects nobody aimed at, and the check that found them has
//	            plainly looked at them.
//
// The first walk missed the second and reported `never` for an asset the check
// had just produced, which is the kind of falsehood this whole screen exists to
// remove.
func (c coverageChecked) LatestPerSubject(ctx context.Context, workspace, target id.ID) ([]entquery.CheckedAt, error) {
	type key struct {
		check id.ID
		kind  string
		value string
	}
	latest := map[key]time.Time{}
	note := func(check id.ID, kind, value string, at time.Time) {
		if check.IsZero() || kind == "" || value == "" || at.IsZero() {
			return
		}
		k := key{check, kind, strings.ToLower(value)}
		if at.After(latest[k]) {
			latest[k] = at
		}
	}

	aimed, err := c.runs.LatestChecked(ctx, workspace, target)
	if err != nil {
		return nil, err
	}
	for _, at := range aimed {
		note(at.CheckID, at.Kind, at.Value, at.At)
	}

	belongs, err := c.runs.InvocationChecks(ctx, workspace, target)
	if err != nil {
		return nil, err
	}
	check := make(map[id.ID]id.ID, len(belongs))
	for _, b := range belongs {
		check[b.InvocationID] = b.CheckID
	}
	seen, err := c.observed.SubjectsPerInvocation(ctx, workspace)
	if err != nil {
		return nil, err
	}
	for _, s := range seen {
		// An invocation from ANOTHER target's run is absent from `check` when a
		// target filter is in force, and is skipped — coverage for one target
		// must not be credited by a run against a different one.
		note(check[s.InvocationID], s.Kind, s.Value, s.At)
	}

	out := make([]entquery.CheckedAt, 0, len(latest))
	for k, at := range latest {
		out = append(out, entquery.CheckedAt{
			CheckID: k.check, Kind: k.kind, Value: k.value, At: at,
		})
	}
	return out, nil
}
