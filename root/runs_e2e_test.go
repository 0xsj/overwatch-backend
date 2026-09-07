// Author-written, against a live database. These are the claims decisions/0033
// lists as "checked against a running server, because nothing else can" — plus
// the loud gate, which is the one rule in this slice a curl walk could not reach
// without the whole mail flow.
package root

import (
	"net/http"
	"testing"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// firmWithATool builds an engagement that can actually run something: a source
// tool, a check whose chain is that one step, a target, and a spawn rule that
// permits it. `echo` is used deliberately — it is on every PATH this will run
// on, and a test that needs subfinder installed is a test that is skipped.
func firmWithATool(t *testing.T, s traced, intensity string, role orgdomain.Role) (
	ws, target, check id.ID, ownerAuth, otherAuth map[string]string, other id.ID) {
	t.Helper()
	org, workspace, _, kit, owner, kitAuth := firm(t, s, role)

	res := s.post(t, "/v1/orgs/"+org.String()+"/tools",
		`{"name":"echoer","argv":"echo {{target}}","intensity":"`+intensity+`","produces":"host"}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add tool: %d", res.StatusCode)
	}
	var tool toolResponse
	decode(t, res, &tool)

	res = s.post(t, "/v1/orgs/"+org.String()+"/checks",
		`{"name":"surface","question":"what is exposed?","applies_to":["host"],"enabled":true}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add check: %d", res.StatusCode)
	}
	var made checkResponse
	decode(t, res, &made)

	res = s.put(t, "/v1/orgs/"+org.String()+"/checks/"+made.CheckID+"/chain",
		`{"steps":[{"tool_id":"`+tool.ToolID+`"}],"flows":[]}`, owner)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("save chain: %d", res.StatusCode)
	}

	res = s.post(t, "/v1/workspaces/"+workspace.String()+"/targets",
		`{"name":"acme.test","kind":"organisation"}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add target: %d", res.StatusCode)
	}
	var subject targetResponse
	decode(t, res, &subject)

	res = s.post(t, "/v1/workspaces/"+workspace.String()+"/targets/"+subject.TargetID+"/scope",
		`{"pattern":"acme.test","polarity":"include","gate":"spawn","kinds":["host"],`+
			`"tools":["passive","light","loud"]}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add rule: %d", res.StatusCode)
	}

	target, err := id.Parse(subject.TargetID)
	if err != nil {
		t.Fatal(err)
	}
	check, err = id.Parse(made.CheckID)
	if err != nil {
		t.Fatal(err)
	}
	return workspace, target, check, owner, kitAuth, kit
}

// 0033 §7 and the client's own fixture: "loud tools need `admin` on the
// workspace; passive ones need `write`."
//
// The raise is computed FROM THE CHAIN, so this also pins that nobody added a
// second copy of "is this check loud" to the check row.
func TestALoudCheckNeedsAdminAndAPassiveOneNeedsWrite(t *testing.T) {
	for _, tc := range []struct {
		intensity string
		withWrite int
		withAdmin int
	}{
		{"passive", http.StatusAccepted, http.StatusAccepted},
		{"loud", http.StatusNotFound, http.StatusAccepted},
	} {
		t.Run(tc.intensity, func(t *testing.T) {
			s := tracedSystem(t)
			// SEATED AS ADMIN, not member. decisions/0023 caps a grant at the
			// role ceiling, so granting `admin` on a workspace to a `member`
			// is a 409 — correctly. The org role is the ceiling and the grant
			// is what actually applies (0019), so an admin holding only
			// `write` has effective `write`, which is what this needs.
			ws, target, check, owner, kitAuth, kit := firmWithATool(t, s, tc.intensity, orgdomain.RoleAdmin)
			body := `{"target_id":"` + target.String() + `","check_id":"` + check.String() + `"}`
			seat := "/v1/workspaces/" + ws.String() + "/members/" + kit.String()

			if res := s.put(t, seat, `{"level":"write"}`, owner); res.StatusCode != http.StatusOK {
				t.Fatalf("grant write: %d", res.StatusCode)
			}
			res := s.post(t, "/v1/workspaces/"+ws.String()+"/runs", body, kitAuth)
			if res.StatusCode != tc.withWrite {
				t.Errorf("with write: got %d, want %d", res.StatusCode, tc.withWrite)
			}

			if res := s.put(t, seat, `{"level":"admin"}`, owner); res.StatusCode != http.StatusOK {
				t.Fatalf("grant admin: %d", res.StatusCode)
			}
			res = s.post(t, "/v1/workspaces/"+ws.String()+"/runs", body, kitAuth)
			if res.StatusCode != tc.withAdmin {
				t.Errorf("with admin: got %d, want %d", res.StatusCode, tc.withAdmin)
			}
		})
	}
}

// The PREVIEW and the PLAN are one computation — 0033 §1. If they ever diverge,
// the one that drifts is the preview, and the preview is what a person reads
// before authorising a scan against a client.
func TestThePreviewMatchesThePlanItWouldWrite(t *testing.T) {
	s := tracedSystem(t)
	ws, target, check, owner, _, _ := firmWithATool(t, s, "passive", orgdomain.RoleMember)
	body := `{"target_id":"` + target.String() + `","check_id":"` + check.String() + `"}`

	res := s.post(t, "/v1/workspaces/"+ws.String()+"/runs/preview", body, owner)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("preview: %d", res.StatusCode)
	}
	var preview runDetailResponse
	decode(t, res, &preview)

	res = s.post(t, "/v1/workspaces/"+ws.String()+"/runs", body, owner)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("start: %d", res.StatusCode)
	}
	var started runDetailResponse
	decode(t, res, &started)

	if len(preview.Invocations) != len(started.Invocations) {
		t.Fatalf("preview planned %d, start planned %d",
			len(preview.Invocations), len(started.Invocations))
	}
	for n := range preview.Invocations {
		p, a := preview.Invocations[n], started.Invocations[n]
		if p.State != a.State {
			t.Errorf("step %d: preview says %q, the plan says %q", n, p.State, a.State)
		}
		if len(p.Argv) != len(a.Argv) {
			t.Fatalf("step %d: preview argv %q, plan argv %q", n, p.Argv, a.Argv)
		}
		for i := range p.Argv {
			if p.Argv[i] != a.Argv[i] {
				t.Errorf("step %d: preview argv %q, plan argv %q", n, p.Argv, a.Argv)
			}
		}
	}

	// And the preview WROTE NOTHING. A preview that leaves a run behind turns
	// "what would happen" into a thing that happened.
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/runs", owner)
	var page runPageResponse
	decode(t, res, &page)
	if len(page.Runs) != 1 {
		t.Fatalf("the preview left a run behind: %d runs", len(page.Runs))
	}
}

// A run whose every step the gate refused is COMPLETE — 0033. It once sat at
// `running` forever, because the worker's claim required a PENDING invocation
// and a wholly-refused run has none. The predicate is now `state = 'running'`
// and this is what says so.
func TestARunWithNothingToDoStillFinishes(t *testing.T) {
	s := tracedSystem(t)
	org, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)

	res := s.post(t, "/v1/orgs/"+org.String()+"/tools",
		`{"name":"echoer","argv":"echo {{target}}","intensity":"passive","produces":"host"}`, owner)
	var tool toolResponse
	decode(t, res, &tool)

	res = s.post(t, "/v1/orgs/"+org.String()+"/checks",
		`{"name":"surface","question":"?","applies_to":["host"],"enabled":true}`, owner)
	var made checkResponse
	decode(t, res, &made)
	s.put(t, "/v1/orgs/"+org.String()+"/checks/"+made.CheckID+"/chain",
		`{"steps":[{"tool_id":"`+tool.ToolID+`"}],"flows":[]}`, owner)

	res = s.post(t, "/v1/workspaces/"+workspace.String()+"/targets",
		`{"name":"acme.test","kind":"organisation"}`, owner)
	var subject targetResponse
	decode(t, res, &subject)

	// NO SCOPE RULE AT ALL. decisions/0010: nothing is in scope until a rule
	// says so, so every step is refused before anything spawns.
	res = s.post(t, "/v1/workspaces/"+workspace.String()+"/runs",
		`{"target_id":"`+subject.TargetID+`","check_id":"`+made.CheckID+`"}`, owner)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("start: %d", res.StatusCode)
	}
	var started runDetailResponse
	decode(t, res, &started)

	if len(started.Invocations) != 1 || started.Invocations[0].State != "refused" {
		t.Fatalf("want one refused invocation, got %+v", started.Invocations)
	}
	// A refusal with NO RULE is a different fact from one a rule excluded.
	if started.Invocations[0].RefusalRule != "" {
		t.Error("nothing permitted it, so no rule is cited")
	}
	if started.Invocations[0].Exit != nil {
		t.Error("a refused invocation never had a process, so it has no exit code")
	}
	if len(started.Invocations[0].Argv) == 0 {
		t.Error("a refusal keeps the argv it would have run — that is what a person reviews")
	}
}

// 0010: "a range in scope for passive collection is not thereby in scope for a
// loud scan." The gate was asked WITHOUT an intensity until 2026-09-07, so a
// rule permitting only `passive` permitted a `loud` tool — a hole in the scope
// model, not a rough edge.
func TestARulePermittingPassiveDoesNotPermitALoudTool(t *testing.T) {
	s := tracedSystem(t)
	org, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)

	res := s.post(t, "/v1/workspaces/"+workspace.String()+"/targets",
		`{"name":"acme.test","kind":"organisation"}`, owner)
	var subject targetResponse
	decode(t, res, &subject)

	// The rule permits PASSIVE COLLECTION ONLY.
	res = s.post(t, "/v1/workspaces/"+workspace.String()+"/targets/"+subject.TargetID+"/scope",
		`{"pattern":"acme.test","polarity":"include","gate":"spawn","kinds":["host"],`+
			`"tools":["passive"]}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add rule: %d", res.StatusCode)
	}

	for _, tc := range []struct{ intensity, want string }{
		{"passive", "pending"},
		{"loud", "refused"},
	} {
		t.Run(tc.intensity, func(t *testing.T) {
			res := s.post(t, "/v1/orgs/"+org.String()+"/tools",
				`{"name":"tool-`+tc.intensity+`","argv":"echo {{target}}",`+
					`"intensity":"`+tc.intensity+`","produces":"host"}`, owner)
			var tool toolResponse
			decode(t, res, &tool)

			res = s.post(t, "/v1/orgs/"+org.String()+"/checks",
				`{"name":"check-`+tc.intensity+`","question":"?","applies_to":["host"],"enabled":true}`, owner)
			var made checkResponse
			decode(t, res, &made)
			s.put(t, "/v1/orgs/"+org.String()+"/checks/"+made.CheckID+"/chain",
				`{"steps":[{"tool_id":"`+tool.ToolID+`"}],"flows":[]}`, owner)

			// The PREVIEW, so nothing spawns and the assertion is about the gate
			// rather than about a process.
			res = s.post(t, "/v1/workspaces/"+workspace.String()+"/runs/preview",
				`{"target_id":"`+subject.TargetID+`","check_id":"`+made.CheckID+`"}`, owner)
			if res.StatusCode != http.StatusOK {
				t.Fatalf("preview: %d", res.StatusCode)
			}
			var preview runDetailResponse
			decode(t, res, &preview)
			if len(preview.Invocations) != 1 {
				t.Fatalf("want one step, got %d", len(preview.Invocations))
			}
			if got := preview.Invocations[0].State; got != tc.want {
				t.Errorf("a `passive`-only rule and a %s tool: got %q, want %q",
					tc.intensity, got, tc.want)
			}
		})
	}
}

// `GET /runs` with no `limit` returned exactly ONE run and reported no next
// page: the handler read `limit`, got 0, and asked for `0+1` rows. The query
// layer clamps a non-positive page size and 1 is positive, so nothing
// downstream could catch it.
//
// Found by a scheduler walk that had started four runs and could see one.
func TestListingRunsWithNoLimitReturnsMoreThanOne(t *testing.T) {
	s := tracedSystem(t)
	ws, target, check, owner, _, _ := firmWithATool(t, s, "passive", orgdomain.RoleMember)
	body := `{"target_id":"` + target.String() + `","check_id":"` + check.String() + `"}`

	for i := 0; i < 3; i++ {
		if res := s.post(t, "/v1/workspaces/"+ws.String()+"/runs", body, owner); res.StatusCode != http.StatusAccepted {
			t.Fatalf("start %d: %d", i, res.StatusCode)
		}
	}

	res := s.get(t, "/v1/workspaces/"+ws.String()+"/runs", owner)
	var page runPageResponse
	decode(t, res, &page)
	if len(page.Runs) != 3 {
		t.Fatalf("three runs started, %d returned", len(page.Runs))
	}
	if page.Next != "" {
		t.Fatal("there is no next page, and offering one invites fetching an empty one")
	}

	// And an explicit limit still pages, with a cursor.
	res = s.get(t, "/v1/workspaces/"+ws.String()+"/runs?limit=2", owner)
	decode(t, res, &page)
	if len(page.Runs) != 2 || page.Next == "" {
		t.Fatalf("limit=2: %d runs, next %q", len(page.Runs), page.Next)
	}
}

// `success_exit_codes` was stored and never projected: `asTool` built the
// response without the field, so every read answered null and the client had to
// assume the server's own default.
//
// The scripted edit that was meant to add it had no assertion and matched
// nothing after gofmt realigned the struct literal — the third time that has
// happened in this tree, and the reason every scripted edit is supposed to
// assert its target exists before writing.
//
// Found by the CLIENT, in a note about walking the live server.
func TestAToolProjectsItsSuccessExitCodes(t *testing.T) {
	s := tracedSystem(t)
	org, _, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)

	res := s.post(t, "/v1/orgs/"+org.String()+"/tools",
		`{"name":"nuclei","argv":"nuclei -u {{target}}","intensity":"loud",`+
			`"consumes":"url","produces":"finding","success_exit_codes":[0,1]}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add: %d", res.StatusCode)
	}
	var created toolResponse
	decode(t, res, &created)
	assertCodes(t, "on create", created.SuccessExitCodes, []int{0, 1})

	// And on the way back out, which is the read that was answering null.
	res = s.get(t, "/v1/orgs/"+org.String()+"/tools", owner)
	var listed []toolResponse
	decode(t, res, &listed)
	if len(listed) != 1 {
		t.Fatalf("want one tool, got %d", len(listed))
	}
	assertCodes(t, "on list", listed[0].SuccessExitCodes, []int{0, 1})
}

func assertCodes(t *testing.T, where string, got, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: want %v, got %v", where, want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: want %v, got %v", where, want, got)
		}
	}
}
