// Author-written, against a live database, from decisions/0044's Verification
// block. Two claims here cannot be made anywhere else: that `seen elsewhere`
// never crosses a workspace, and that a node can be attributed while no longer
// permitted — `CLAUDE.md`'s first pair, on one screen.
package root

import (
	"crypto/rand"
	"time"

	entdomain "github.com/0xsj/overwatch-backend/internal/entity/domain"
	entpg "github.com/0xsj/overwatch-backend/internal/entity/infra/postgres"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"net/http"
	"testing"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type canvasBody struct {
	Root  entityResponse `json:"root"`
	Nodes []struct {
		FragmentID    string `json:"fragment_id"`
		Kind          string `json:"kind"`
		Value         string `json:"value"`
		SeenElsewhere bool   `json:"seen_elsewhere"`
		InScope       bool   `json:"in_scope"`
		Edge          struct {
			Claimant string `json:"claimant"`
			State    string `json:"state"`
		} `json:"edge"`
	} `json:"nodes"`
	Derivations []struct {
		From         string `json:"from"`
		To           string `json:"to"`
		Label        string `json:"label"`
		InvocationID string `json:"invocation_id"`
		ArtifactID   string `json:"artifact_id"`
	} `json:"derivations"`
	Summary struct {
		Accepted    int `json:"accepted"`
		Proposed    int `json:"proposed"`
		Rejected    int `json:"rejected"`
		Derivations int `json:"derivations"`
	} `json:"summary"`
	Truncated bool `json:"truncated"`
}

// rootOf finds the root entity a target's graph hangs off — created by the
// subscriber on `target.added`, so the outbox has to have drained.
func rootOf(t *testing.T, s traced, ws id.ID, auth map[string]string) string {
	t.Helper()
	res := s.get(t, "/v1/workspaces/"+ws.String()+"/entities", auth)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("entities: %d", res.StatusCode)
	}
	var found []entityResponse
	decode(t, res, &found)
	if len(found) == 0 {
		t.Fatal("the target.added subscriber left no root entity")
	}
	return found[0].EntityID
}

// nodeFor finds a node by its VALUE and FAILS when there is none.
//
// The obvious version builds a `map[string]int` and indexes it — and a missing
// key returns 0, so an assertion silently becomes about node zero. This file had
// one of those for exactly as long as it took to notice.
func nodeFor(t *testing.T, c canvasBody, value string) struct {
	FragmentID    string `json:"fragment_id"`
	Kind          string `json:"kind"`
	Value         string `json:"value"`
	SeenElsewhere bool   `json:"seen_elsewhere"`
	InScope       bool   `json:"in_scope"`
	Edge          struct {
		Claimant string `json:"claimant"`
		State    string `json:"state"`
	} `json:"edge"`
} {
	t.Helper()
	for _, one := range c.Nodes {
		if one.Value == value {
			return one
		}
	}
	t.Fatalf("no node for %q: %+v", value, c.Nodes)
	return c.Nodes[0]
}

func canvasOf(t *testing.T, s traced, ws id.ID, entity string, auth map[string]string) canvasBody {
	t.Helper()
	res := s.get(t, "/v1/workspaces/"+ws.String()+"/entities/"+entity, auth)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("canvas: %d", res.StatusCode)
	}
	var out canvasBody
	decode(t, res, &out)
	return out
}

// drawn seeds a canvas with real nodes: two fragments attributed to the root,
// one derivation between them, and one derivation to a fragment that is NOT on
// this canvas.
//
// **It writes through the entity store rather than driving a run**, because the
// harness deliberately never ticks the executor — `0033` §1's whole point is
// that planning spawns nothing, and a canvas test that needed a real scan would
// be a test about the executor.
//
// The first version of this file had no nodes at all: four tests that drew an
// EMPTY canvas four ways, scoring 0/10 against mutation. This function is what
// that cost.
func drawn(t *testing.T, s traced, ws id.ID, rootID string) (onCanvas, offCanvas id.ID) {
	t.Helper()
	ctx := t.Context()
	store := entpg.NewStore(s.pool)
	ids := id.NewV7(clock.System{}, rand.Reader)
	at := time.Now().UTC()

	root, err := id.Parse(rootID)
	if err != nil {
		t.Fatal(err)
	}

	fragment := func(kind, value string) entdomain.Fragment {
		t.Helper()
		fresh, err := entdomain.NewFragment(ids.NewID(), ws, kind, value, entdomain.Observed, at)
		if err != nil {
			t.Fatal(err)
		}
		stored, _, err := store.Upsert(ctx, fresh)
		if err != nil {
			t.Fatal(err)
		}
		return stored
	}
	attribute := func(entity id.ID, f entdomain.Fragment, state entdomain.ClaimState) {
		t.Helper()
		claim, err := entdomain.Propose(ids.NewID(), ws, entity, f.ID,
			entdomain.ByRule, ids.NewID(), 0, false, "in scope", at)
		if err != nil {
			t.Fatal(err)
		}
		if state == entdomain.Accepted {
			// ByRule is born accepted — 0036 §3 — so this is already the state
			// we want and asserting it here stops a silent change.
			if claim.State != entdomain.Accepted {
				t.Fatalf("a rule's claim is born accepted: %s", claim.State)
			}
		}
		if err := store.Attribute(ctx, claim); err != nil {
			t.Fatal(err)
		}
	}

	host := fragment("host", "a.acme.test")
	url := fragment("url", "https://a.acme.test/")
	elsewhere := fragment("host", "shared.acme.test")
	attribute(root, host, entdomain.Accepted)
	attribute(root, url, entdomain.Accepted)
	attribute(root, elsewhere, entdomain.Accepted)

	// A SECOND ROOT in this workspace attributing the SAME fragment — one
	// client's two subsidiaries sharing a host, which is what `seen elsewhere`
	// means and the only thing it can mean.
	second, err := entdomain.NewEntity(ids.NewID(), ws, "org", "Subsidiary", at)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateEntity(ctx, second); err != nil {
		t.Fatal(err)
	}
	attribute(second.ID, elsewhere, entdomain.Accepted)

	// An edge BETWEEN two drawn nodes, and one to a fragment nobody attributed
	// — the second must be omitted.
	off := fragment("host", "never-drawn.acme.test")
	for _, edge := range []struct{ from, to id.ID }{
		{host.ID, url.ID},
		{off.ID, url.ID},
	} {
		d, err := entdomain.NewDerivation(ids.NewID(), ws, edge.from, edge.to,
			"input", ids.NewID(), ids.NewID(), ids.NewID(), at)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Draw(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	return host.ID, off.ID
}

// **THE test this file was missing.** A canvas with real nodes: both edge kinds,
// the facets, and a summary that counts what was drawn.
func TestACanvasDrawsBothEdgeKindsAndBothFacets(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)
	res := s.post(t, "/v1/workspaces/"+workspace.String()+"/targets",
		`{"name":"acme.test","kind":"organisation"}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add target: %d", res.StatusCode)
	}
	var subject targetResponse
	decode(t, res, &subject)
	s.drain(t)

	// A SPAWN RULE covering one host and not the other, so `in_scope`
	// partitions rather than being uniformly true.
	if res := s.post(t, "/v1/workspaces/"+workspace.String()+"/targets/"+subject.TargetID+"/scope",
		`{"pattern":"a.acme.test","polarity":"include","gate":"spawn","kinds":["host"],`+
			`"tools":["passive","light","loud"]}`, owner); res.StatusCode != http.StatusCreated {
		t.Fatalf("add rule: %d", res.StatusCode)
	}

	entity := rootOf(t, s, workspace, owner)
	drawn(t, s, workspace, entity)
	got := canvasOf(t, s, workspace, entity, owner)

	if len(got.Nodes) != 3 {
		t.Fatalf("three fragments attributed to this root: %+v", got.Nodes)
	}
	// **THE SECOND EDGE KIND**, its own array, and the off-canvas edge OMITTED.
	if len(got.Derivations) != 1 {
		t.Fatalf("one edge with both ends drawn: %+v", got.Derivations)
	}
	if got.Derivations[0].InvocationID == "" || got.Derivations[0].ArtifactID == "" {
		t.Fatal("0003: an edge without these is a similarity edge wearing a costume")
	}
	if got.Summary.Derivations != 1 || got.Summary.Accepted != 3 {
		t.Fatalf("the summary counts what was DRAWN: %+v", got.Summary)
	}

	// SEEN ELSEWHERE: attributed to two roots in this engagement.
	if !nodeFor(t, got, "shared.acme.test").SeenElsewhere {
		t.Fatal("a fragment attributed to two roots is seen elsewhere")
	}
	for _, only := range []string{"a.acme.test", "https://a.acme.test/"} {
		if nodeFor(t, got, only).SeenElsewhere {
			t.Fatalf("%s is attributed to one root and is not elsewhere", only)
		}
	}
	// IN SCOPE: the gate asked NOW. `a.acme.test` is covered by the rule;
	// `shared.acme.test` is attributed and NOT permitted, which is
	// CLAUDE.md's first pair with both halves visible.
	if !nodeFor(t, got, "a.acme.test").InScope {
		t.Fatal("a host the rule covers is in scope")
	}
	if nodeFor(t, got, "shared.acme.test").InScope {
		t.Fatal("attributed and NOT permitted — the pair that must not collapse")
	}
	// A URL is a spawn kind with no rule covering it, so it is out of scope too.
	if nodeFor(t, got, "https://a.acme.test/").InScope {
		t.Fatal("nothing permits this url")
	}
}

// **The four facts `seen elsewhere` and the edge filter turn on**, each of which
// a mutation round found unasserted after the first rewrite. They are one test
// because they share an expensive fixture and each is one line of setup.
func TestTheCanvasFacetsTurnOnTheThingsTheyClaimTo(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)
	if res := s.post(t, "/v1/workspaces/"+workspace.String()+"/targets",
		`{"name":"acme.test","kind":"organisation"}`, owner); res.StatusCode != http.StatusCreated {
		t.Fatalf("add target: %d", res.StatusCode)
	}
	s.drain(t)
	entity := rootOf(t, s, workspace, owner)

	ctx := t.Context()
	store := entpg.NewStore(s.pool)
	ids := id.NewV7(clock.System{}, rand.Reader)
	at := time.Now().UTC()
	root, err := id.Parse(entity)
	if err != nil {
		t.Fatal(err)
	}
	fragment := func(kind, value string) entdomain.Fragment {
		t.Helper()
		fresh, err := entdomain.NewFragment(ids.NewID(), workspace, kind, value,
			entdomain.Observed, at)
		if err != nil {
			t.Fatal(err)
		}
		stored, _, err := store.Upsert(ctx, fresh)
		if err != nil {
			t.Fatal(err)
		}
		return stored
	}
	claim := func(entity id.ID, f entdomain.Fragment, claimant entdomain.Claimant) {
		t.Helper()
		var (
			conf float64
			has  bool
			ref  id.ID
		)
		if claimant == entdomain.ByModel {
			conf, has = 0.6, true
		} else {
			ref = ids.NewID()
		}
		c, err := entdomain.Propose(ids.NewID(), workspace, entity, f.ID,
			claimant, ref, conf, has, "because", at)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Attribute(ctx, c); err != nil {
			t.Fatal(err)
		}
	}

	// A SECOND ROOT with NO TARGET — every canvas so far has had one, so the
	// gate's "no target means nothing is in scope" was asserted by nothing.
	orphan, err := entdomain.NewEntity(ids.NewID(), workspace, "org", "No target", at)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateEntity(ctx, orphan); err != nil {
		t.Fatal(err)
	}

	// 1 · A second root's claim that is only PROPOSED must NOT make the
	//     fragment `seen elsewhere` — an unruled claim is not another root
	//     saying it is theirs.
	proposedOnly := fragment("host", "proposed-elsewhere.acme.test")
	claim(root, proposedOnly, entdomain.ByRule)
	claim(orphan.ID, proposedOnly, entdomain.ByModel)

	// 2 · A fragment whose KIND is outside the scope vocabulary. `entity` does
	//     not validate kinds — only that one is present — so this is reachable,
	//     and the gate must FAIL CLOSED rather than permit it.
	nonsense := fragment("nonsense", "whatever.acme.test")
	claim(root, nonsense, entdomain.ByRule)

	// 3 · An edge FROM a drawn node TO one that is not drawn. The first version
	//     of this file only had the reverse, so removing the `to` filter changed
	//     nothing.
	drawnEnd := fragment("host", "drawn.acme.test")
	claim(root, drawnEnd, entdomain.ByRule)
	offCanvas := fragment("host", "off-canvas.acme.test")
	edge, err := entdomain.NewDerivation(ids.NewID(), workspace, drawnEnd.ID, offCanvas.ID,
		"input", ids.NewID(), ids.NewID(), ids.NewID(), at)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Draw(ctx, edge); err != nil {
		t.Fatal(err)
	}

	got := canvasOf(t, s, workspace, entity, owner)

	if nodeFor(t, got, "proposed-elsewhere.acme.test").SeenElsewhere {
		t.Fatal("a PROPOSED claim from another root is not another root saying it is theirs")
	}
	if nodeFor(t, got, "whatever.acme.test").InScope {
		t.Fatal("a kind scope has no word for cannot be permitted — FAIL CLOSED")
	}
	// The edge has one end off-canvas and must be omitted.
	if len(got.Derivations) != 0 {
		t.Fatalf("an edge to an undrawn node is omitted: %+v", got.Derivations)
	}

	// 4 · THE ORPHAN ROOT: no target, so no scope to be in, so nothing is in it.
	//     `true` here would be a canvas telling somebody an unscoped estate is
	//     entirely permitted.
	orphanCanvas := canvasOf(t, s, workspace, orphan.ID.String(), owner)
	if len(orphanCanvas.Nodes) != 1 {
		t.Fatalf("the orphan holds one proposed claim: %+v", orphanCanvas.Nodes)
	}
	if orphanCanvas.Nodes[0].InScope {
		t.Fatal("a root with no target has no scope, and nothing is in it")
	}
}

// A PROPOSED claim is drawn and counted apart from an accepted one — the
// legend's `ACCEPTED 37 · PROPOSED 25` is two numbers because they are two
// states, and `0009` keeps them apart.
func TestTheSummaryCountsAcceptedAndProposedApart(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)
	if res := s.post(t, "/v1/workspaces/"+workspace.String()+"/targets",
		`{"name":"acme.test","kind":"organisation"}`, owner); res.StatusCode != http.StatusCreated {
		t.Fatalf("add target: %d", res.StatusCode)
	}
	s.drain(t)
	entity := rootOf(t, s, workspace, owner)

	ctx := t.Context()
	store := entpg.NewStore(s.pool)
	ids := id.NewV7(clock.System{}, rand.Reader)
	at := time.Now().UTC()
	root, err := id.Parse(entity)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := entdomain.NewFragment(ids.NewID(), workspace, "host", "guess.acme.test",
		entdomain.Observed, at)
	if err != nil {
		t.Fatal(err)
	}
	stored, _, err := store.Upsert(ctx, fresh)
	if err != nil {
		t.Fatal(err)
	}
	// A MODEL's claim is born PROPOSED — 0036 §3, and only a model carries a
	// confidence.
	claim, err := entdomain.Propose(ids.NewID(), workspace, root, stored.ID,
		entdomain.ByModel, id.ID{}, 0.71, true, "pattern match", at)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Attribute(ctx, claim); err != nil {
		t.Fatal(err)
	}

	got := canvasOf(t, s, workspace, entity, owner)
	if got.Summary.Proposed != 1 || got.Summary.Accepted != 0 {
		t.Fatalf("a model's claim is proposed, not accepted: %+v", got.Summary)
	}
	if got.Nodes[0].Edge.State != "proposed" || got.Nodes[0].Edge.Claimant != "model" {
		t.Fatalf("%+v", got.Nodes[0].Edge)
	}
}

// An empty canvas still answers both edge kinds and a summary, and the summary
// counts the DRAWN set — zero, honestly, rather than being absent.
func TestAnEmptyCanvasStillAnswersBothEdgeKinds(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)
	res := s.post(t, "/v1/workspaces/"+workspace.String()+"/targets",
		`{"name":"acme.test","kind":"organisation"}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add target: %d", res.StatusCode)
	}
	s.drain(t)

	got := canvasOf(t, s, workspace, rootOf(t, s, workspace, owner), owner)
	if got.Nodes == nil || got.Derivations == nil {
		t.Fatalf("both arrays are present and empty, never null: %+v", got)
	}
	if got.Summary.Accepted != 0 || got.Summary.Derivations != 0 {
		t.Fatalf("summary: %+v", got.Summary)
	}
	if got.Truncated {
		t.Fatal("nothing was held back")
	}
}

// **THE DISCLOSURE CLAIM.** `seen elsewhere` means another ROOT in THIS
// engagement. A fragment does not exist across workspaces at all — `0036` makes
// it (workspace, kind, value) — and a flag that reached across one would tell an
// analyst that an engagement they may hold `none` on exists.
//
// This drives the negative: two engagements, the same host value, and neither
// canvas says "elsewhere".
func TestSeenElsewhereNeverCrossesAWorkspace(t *testing.T) {
	s := tracedSystem(t)
	org, first, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)

	// A SECOND engagement in the same org, with a target of the same name.
	res := s.post(t, "/v1/orgs/"+org.String()+"/workspaces", `{"name":"Beta"}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("open second: %d", res.StatusCode)
	}
	var opened workspaceResponse
	decode(t, res, &opened)
	second, err := id.Parse(opened.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}

	for _, ws := range []id.ID{first, second} {
		if res := s.post(t, "/v1/workspaces/"+ws.String()+"/targets",
			`{"name":"acme.test","kind":"organisation"}`, owner); res.StatusCode != http.StatusCreated {
			t.Fatalf("add target: %d", res.StatusCode)
		}
	}
	s.drain(t)

	// **THE SAME HOST VALUE, ATTRIBUTED IN BOTH.** Without this the test has no
	// nodes and asserts a negative over an empty set — which is what the first
	// version of it did, and what a mutation round scoring 0/10 found.
	ctx := t.Context()
	store := entpg.NewStore(s.pool)
	ids := id.NewV7(clock.System{}, rand.Reader)
	at := time.Now().UTC()
	for _, ws := range []id.ID{first, second} {
		root, err := id.Parse(rootOf(t, s, ws, owner))
		if err != nil {
			t.Fatal(err)
		}
		fresh, err := entdomain.NewFragment(ids.NewID(), ws, "host", "shared.acme.test",
			entdomain.Observed, at)
		if err != nil {
			t.Fatal(err)
		}
		stored, _, err := store.Upsert(ctx, fresh)
		if err != nil {
			t.Fatal(err)
		}
		claim, err := entdomain.Propose(ids.NewID(), ws, root, stored.ID,
			entdomain.ByRule, ids.NewID(), 0, false, "in scope", at)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Attribute(ctx, claim); err != nil {
			t.Fatal(err)
		}
	}

	for _, ws := range []id.ID{first, second} {
		got := canvasOf(t, s, ws, rootOf(t, s, ws, owner), owner)
		if len(got.Nodes) != 1 {
			t.Fatalf("one node per engagement, or this asserts nothing: %+v", got.Nodes)
		}
		if got.Nodes[0].Value != "shared.acme.test" {
			t.Fatalf("the same VALUE in both: %q", got.Nodes[0].Value)
		}
		// The same value, attributed in two engagements, and NEITHER says so.
		// `entity.fragment` is (workspace, kind, value), so these are two
		// different fragments and there is nothing to join — which is exactly
		// what keeps one client's estate off another's screen.
		if got.Nodes[0].SeenElsewhere {
			t.Fatalf("a node disclosed another engagement: %+v", got.Nodes[0])
		}
	}
}

// The canvas is a RECORD read, so a closed engagement still draws — "who
// widened the scope" is asked after an engagement, not during it (0027).
func TestAClosedEngagementStillDrawsItsCanvas(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)
	if res := s.post(t, "/v1/workspaces/"+workspace.String()+"/targets",
		`{"name":"acme.test","kind":"organisation"}`, owner); res.StatusCode != http.StatusCreated {
		t.Fatalf("add target: %d", res.StatusCode)
	}
	s.drain(t)
	entity := rootOf(t, s, workspace, owner)

	if res := s.post(t, "/v1/workspaces/"+workspace.String()+"/close", "", owner); res.StatusCode != http.StatusNoContent {
		t.Fatalf("close: %d", res.StatusCode)
	}
	if res := s.get(t, "/v1/workspaces/"+workspace.String()+"/entities/"+entity, owner); res.StatusCode != http.StatusOK {
		t.Fatalf("a closed engagement's canvas: got %d, want 200", res.StatusCode)
	}
}

// A `client` cannot reach the canvas — decisions/0042's fail-closed gate, which
// this route goes through like every other read.
func TestAClientCannotReachTheCanvas(t *testing.T) {
	s := tracedSystem(t)
	ws, _, owner, client := engagementWithAClient(t, s)
	s.drain(t)
	entity := rootOf(t, s, ws, owner)
	if res := s.get(t, "/v1/workspaces/"+ws.String()+"/entities/"+entity, client); res.StatusCode != http.StatusNotFound {
		t.Fatalf("a client read the canvas: %d", res.StatusCode)
	}
}

// **The pair moving over TIME.** `CLAUDE.md`'s first pair — *attributed* vs
// *permitted* — is usually shown as two fragments at one moment. This is one
// fragment at two moments: the estate did not change, the AUTHORISATION did.
//
// `0044` says `in_scope` is the spawn gate asked NOW, and `0030` says a rule is
// superseded rather than edited. Together those mean narrowing scope must flip a
// node to out-of-scope while leaving its attribution exactly as it was — a
// scope change is not a claim that the asset stopped being theirs, and a canvas
// that un-attributed it would be destroying evidence to reflect a permission.
func TestNarrowingScopeMovesANodeOutWithoutUnattributingIt(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)
	res := s.post(t, "/v1/workspaces/"+workspace.String()+"/targets",
		`{"name":"acme.test","kind":"organisation"}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add target: %d", res.StatusCode)
	}
	var subject targetResponse
	decode(t, res, &subject)
	s.drain(t)

	scope := "/v1/workspaces/" + workspace.String() + "/targets/" + subject.TargetID + "/scope"
	res = s.post(t, scope,
		`{"pattern":"a.acme.test","polarity":"include","gate":"spawn","kinds":["host"],`+
			`"tools":["passive","light","loud"]}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add rule: %d", res.StatusCode)
	}
	var rule ruleResponse
	decode(t, res, &rule)

	entity := rootOf(t, s, workspace, owner)
	drawn(t, s, workspace, entity)

	before := nodeFor(t, canvasOf(t, s, workspace, entity, owner), "a.acme.test")
	if !before.InScope || before.Edge.State != "accepted" {
		t.Fatalf("the rule covers it and a rule's claim is accepted: %+v", before)
	}

	// SUPERSEDE, which is the only way a rule changes — 0030.
	if res := s.delete(t, scope+"/"+rule.RuleID, owner); res.StatusCode != http.StatusNoContent {
		t.Fatalf("supersede: %d", res.StatusCode)
	}

	after := nodeFor(t, canvasOf(t, s, workspace, entity, owner), "a.acme.test")
	if after.InScope {
		t.Fatal("the permitting rule is gone and NOTHING is in scope until a rule says so")
	}
	// **And the attribution is untouched.** Same claim, same state, same
	// claimant — this is the half of the pair the scope change must not reach.
	if after.Edge.State != before.Edge.State || after.Edge.Claimant != before.Edge.Claimant {
		t.Fatalf("a scope change rewrote a claim: %+v -> %+v", before.Edge, after.Edge)
	}
	if after.FragmentID != before.FragmentID {
		t.Fatal("the node is the same fragment")
	}
}
