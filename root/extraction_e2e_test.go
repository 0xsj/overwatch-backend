package root

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
)

// The EXTRACTION HALF, end to end and for real: a process spawns, writes bytes,
// the bytes are read under a live mapping, and the graph subscriber turns what
// was read into fragments, attributions and coverage.
//
// **Nothing had ever asserted this.** Every run test in this package stopped at
// the plan — what was refused, what argv would have run — and `Executor.Tick`
// carried a comment saying it was exported for a test that did not exist. The
// half was assumed to work because each piece had a unit test, which is how it
// went unnoticed that extraction could not read the format most of the corpus
// emits.
//
// The tool here writes LINES, because that is what `subfinder -silent`,
// `assetfinder`, `dnsx` and `httpx` without `-json` all write: one identifier
// per line and nothing else. Until 2026-09-08 that produced `fields_seen: 0`
// and a green run.
func TestALineOrientedToolBecomesAssetsAndCoverage(t *testing.T) {
	s := tracedSystem(t)
	org, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)

	// `printf` and not `echo`: pkg/execx does not use a shell, so the format
	// string has to be interpreted by the program itself. Two `{{host}}` fields
	// because the substitution is per-field — that is `execx`'s split-first rule
	// standing in the open rather than being described.
	//
	// NO `-json` FLAG, so `root/runports.go` reads the media type as
	// `text/plain` and extraction reads lines. That is the whole point of the
	// tool.
	res := s.post(t, "/v1/orgs/"+org.String()+"/tools",
		`{"name":"lister","argv":"printf a.%s\\nb.%s\\n {{host}} {{host}}",`+
			`"intensity":"passive","produces":"host"}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add tool: %d", res.StatusCode)
	}
	var tool toolResponse
	decode(t, res, &tool)

	// The SUBJECT mapping. A text line has no key, so the path is `.line` —
	// where the value sat, never what it means. What it means is `host`, and
	// that is this mapping's `field`, exactly as it would be for `.host` in a
	// JSON record.
	res = s.post(t, "/v1/orgs/"+org.String()+"/tools/"+tool.ToolID+"/mappings",
		`{"field":"host","expression":".line","role":"subject","promote":true}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add mapping: %d", res.StatusCode)
	}
	var mapped mappingResponse
	decode(t, res, &mapped)
	if mapped.State != "live" {
		t.Fatalf("a mapping nobody promoted reads nothing: %q", mapped.State)
	}

	res = s.post(t, "/v1/orgs/"+org.String()+"/checks",
		`{"name":"surface","question":"what is exposed?","applies_to":["host"],"enabled":true}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add check: %d", res.StatusCode)
	}
	var made checkResponse
	decode(t, res, &made)
	if res = s.put(t, "/v1/orgs/"+org.String()+"/checks/"+made.CheckID+"/chain",
		`{"steps":[{"tool_id":"`+tool.ToolID+`"}],"flows":[]}`, owner); res.StatusCode != http.StatusOK {
		t.Fatalf("save chain: %d", res.StatusCode)
	}

	res = s.post(t, "/v1/workspaces/"+workspace.String()+"/targets",
		`{"name":"acme.test","kind":"organisation"}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add target: %d", res.StatusCode)
	}
	var subject targetResponse
	decode(t, res, &subject)
	scope := "/v1/workspaces/" + workspace.String() + "/targets/" + subject.TargetID + "/scope"

	// ONE RULE, ANSWERING BOTH QUESTIONS — decisions/0036 Section 5. A host is
	// SPAWN-gated, so it is attributed because the run that found it was aimed
	// at this target and this rule permitted the spawn: `attributed` and
	// `permitted` are the same rule answering two questions. A `claim` rule
	// naming `host` is refused, and it is the obvious thing to write here.
	if res = s.post(t, scope,
		`{"pattern":"acme.test","polarity":"include","gate":"spawn","kinds":["host"],`+
			`"tools":["passive"]}`, owner); res.StatusCode != http.StatusCreated {
		t.Fatalf("spawn rule: %d", res.StatusCode)
	}
	s.drain(t)

	res = s.post(t, "/v1/workspaces/"+workspace.String()+"/runs",
		`{"target_id":"`+subject.TargetID+`","check_id":"`+made.CheckID+`"}`, owner)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("start: %d", res.StatusCode)
	}
	var started runDetailResponse
	decode(t, res, &started)
	if len(started.Invocations) != 1 {
		t.Fatalf("want one planned invocation, got %d", len(started.Invocations))
	}

	// Spawn, extract, deliver, and again for anything the delivery planned.
	s.work(t, 2)

	// 1 · THE OBSERVATIONS. Two lines, two rows: an observation is per VALUE.
	invocation := started.Invocations[0].InvocationID
	res = s.get(t, "/v1/workspaces/"+workspace.String()+
		"/observations?invocation="+invocation, owner)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("observations: %d", res.StatusCode)
	}
	var observed []observationResponse
	decode(t, res, &observed)
	if len(observed) != 2 {
		t.Fatalf("two lines is two observations, got %d: %+v", len(observed), observed)
	}
	for _, o := range observed {
		if o.SubjectKind != "host" {
			t.Errorf("subject kind is the TOOL's produces-kind: %q", o.SubjectKind)
		}
		if o.Field != "host" {
			t.Errorf("the field is the MAPPING's name and not the path: %q", o.Field)
		}
		if o.ArtifactID == "" || o.InvocationID == "" || o.MappingID == "" {
			t.Errorf("lineage back to the bytes is the point: %+v", o)
		}
	}
	if got := values(observed); got != "a.acme.test|b.acme.test" {
		t.Errorf("read the wrong lines: %s", got)
	}

	// THE FIELD ACCOUNTING — 0035. `.line` is the only path a text record has,
	// and a mapping claimed it, so nothing is left alone. A non-empty list here
	// means the synthesised path and the mapping's expression disagree, which
	// extracts nothing while looking like a tool nobody has finished teaching.
	res = s.get(t, "/v1/workspaces/"+workspace.String()+
		"/invocations/"+invocation+"/extraction", owner)
	var quality struct {
		FieldsSeen     int                `json:"fields_seen"`
		Mapped         int                `json:"mapped"`
		LeftAlone      int                `json:"left_alone"`
		Observations   int                `json:"observations"`
		LeftAlonePaths []unmappedResponse `json:"left_alone_paths"`
	}
	decode(t, res, &quality)
	if quality.FieldsSeen != 1 || quality.Mapped != 1 || quality.LeftAlone != 0 {
		t.Errorf("one path, claimed: %+v", quality)
	}
	if len(quality.LeftAlonePaths) != 0 {
		t.Errorf("`.line` is mapped, so nothing is left alone: %+v", quality.LeftAlonePaths)
	}

	// 2 · THE FRAGMENTS. Deduped identifiers, origin `observed`.
	res = s.get(t, "/v1/workspaces/"+workspace.String()+"/fragments", owner)
	var fragments []fragmentResponse
	decode(t, res, &fragments)
	if len(fragments) != 2 {
		t.Fatalf("two subjects is two fragments, got %d: %+v", len(fragments), fragments)
	}
	for _, f := range fragments {
		if f.Origin != "observed" {
			t.Errorf("a tool said it, so it was observed: %q", f.Origin)
		}
		if f.FirstSeen == "" {
			t.Errorf("an observed fragment has been seen: %+v", f)
		}
	}

	// 3 · THE ASSETS. A fragment in a role — 0009 — which is the claim gate
	// having answered, not a second table.
	res = s.get(t, "/v1/workspaces/"+workspace.String()+"/assets", owner)
	var assets []assetResponse
	decode(t, res, &assets)
	if len(assets) != 2 {
		t.Fatalf("the rule that permitted the spawn attributes both, got %d: %+v",
			len(assets), assets)
	}
	for _, a := range assets {
		if a.RootEntityID == "" || a.TargetID == "" {
			t.Errorf("an asset is attributed to the TARGET's root entity: %+v", a)
		}
		if a.Basis == "" {
			t.Errorf("the basis comes from the gate the kind belongs to: %+v", a)
		}
	}

	// 4 · COVERAGE. The question PRODUCT.md says nothing else answers.
	res = s.get(t, "/v1/workspaces/"+workspace.String()+"/coverage", owner)
	var cover coverageResponse
	decode(t, res, &cover)
	if cover.Assets != 2 {
		t.Fatalf("coverage counts the assets: %+v", cover)
	}
	// RAGGED — 0011. One applicable check over two assets is two pairs, and it
	// is a SUM of what applies rather than assets x checks.
	if cover.Pairs != 2 {
		t.Errorf("one applicable check over two assets is two pairs: %+v", cover)
	}
	if cover.Never != 0 {
		t.Errorf("the run that produced them checked them — 0037: %+v", cover)
	}
	if cover.Fresh != 2 {
		t.Errorf("both cells were checked by the run that discovered them: %+v", cover)
	}
	for _, row := range cover.Rows {
		if len(row.Cells) != 1 {
			t.Errorf("one applicable check is one cell: %+v", row)
		}
	}
}

// values renders the observed values in a stable order, so a failure says what
// was read rather than that two slices differ.
func values(in []observationResponse) string {
	seen := make([]string, 0, len(in))
	for _, o := range in {
		seen = append(seen, o.Value)
	}
	sort.Strings(seen)
	return strings.Join(seen, "|")
}
