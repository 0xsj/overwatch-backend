// Author-written, against a live database. The end-to-end claim is the one the
// domain cannot make: the six probes are wired to the six places machinery
// actually goes quiet, and a real chainless check is really found.
package root

import (
	"net/http"
	"testing"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
)

// A clean engagement answers with SIX MEASURED PROBES and no symptoms — and the
// probes are the half that makes the other half mean anything.
func TestACleanEngagementSaysWhatItLookedAt(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)

	res := s.get(t, "/v1/workspaces/"+workspace.String()+"/health", owner)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("health: %d", res.StatusCode)
	}
	var out healthResponse
	decode(t, res, &out)

	if len(out.Probes) != 6 {
		t.Fatalf("six probes, always: %d", len(out.Probes))
	}
	for _, p := range out.Probes {
		if !p.Measured {
			t.Fatalf("%s could not be measured: %s", p.Kind, p.Because)
		}
		if p.Looked == nil || p.Found == nil {
			t.Fatalf("%s is measured and must carry both counts", p.Kind)
		}
		if *p.Found != 0 {
			t.Fatalf("%s found %d on a clean engagement", p.Kind, *p.Found)
		}
	}
	if !out.Trustworthy {
		t.Fatal("every probe ran, so `nothing is wrong` is sayable")
	}
	if len(out.Symptoms) != 0 {
		t.Fatalf("nothing has run yet: %+v", out.Symptoms)
	}
}

// **A check that can never run is found, and it says why.** This is the probe
// that matters most: `0038` stopped one of these starving the queue and it still
// never runs, and a coverage grid counts its column as never attempted without
// ever explaining it.
func TestAnEnabledCheckWithNoChainIsASymptom(t *testing.T) {
	s := tracedSystem(t)
	org, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)

	// Enabled, on a clock, and NO CHAIN.
	res := s.post(t, "/v1/orgs/"+org.String()+"/checks",
		`{"name":"surface","question":"what is exposed?","applies_to":["host"],`+
			`"enabled":true,"interval_seconds":3600}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add check: %d", res.StatusCode)
	}

	res = s.get(t, "/v1/workspaces/"+workspace.String()+"/health", owner)
	var out healthResponse
	decode(t, res, &out)

	var found *symptomResponse
	for n := range out.Symptoms {
		if out.Symptoms[n].Kind == "check_unrunnable" {
			found = &out.Symptoms[n]
		}
	}
	if found == nil {
		t.Fatalf("a chainless enabled check is a symptom: %+v", out.Symptoms)
	}
	if found.Subject != "surface" {
		t.Fatalf("a symptom names the thing a person acts on: %q", found.Subject)
	}
	if found.Says == "" {
		t.Fatal("a symptom says what the silence means")
	}
	// The DENOMINATOR moved with it: one check considered, one found.
	for _, p := range out.Probes {
		if p.Kind == "check_unrunnable" && (*p.Looked != 1 || *p.Found != 1) {
			t.Fatalf("looked %d found %d", *p.Looked, *p.Found)
		}
	}
}

// **The HUMAN check is chainless by design and must NOT be reported.** `READ BY
// YOU` has no chain on purpose — 0037 §3 made that a flag for exactly this
// reason — and reporting it would train a reader to ignore this list.
func TestTheHumanCheckIsNotASymptom(t *testing.T) {
	s := tracedSystem(t)
	org, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)

	res := s.post(t, "/v1/orgs/"+org.String()+"/checks",
		`{"name":"read by you","question":"has a person looked?","applies_to":["host"],`+
			`"enabled":true,"interval_seconds":3600,"human":true}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add human check: %d", res.StatusCode)
	}

	res = s.get(t, "/v1/workspaces/"+workspace.String()+"/health", owner)
	var out healthResponse
	decode(t, res, &out)
	for _, one := range out.Symptoms {
		if one.Kind == "check_unrunnable" {
			t.Fatalf("the human check was reported as broken: %+v", one)
		}
	}
}

// **THE THREE CHECKS THAT ARE NOT SYMPTOMS.** A mutation round killed the human
// filter and left the other two standing, because every test so far created a
// check that WAS broken — the negative side of each predicate was asserted by
// nothing.
//
// A chainless check is only a symptom when somebody has asked for it to run: a
// disabled one is switched off on purpose, and one with no interval runs when a
// person presses the button. Reporting either would train a reader to ignore
// this list, which is the same argument the human check already carries.
func TestAChainlessCheckIsOnlyASymptomWhenItIsMeantToRun(t *testing.T) {
	s := tracedSystem(t)
	org, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)

	for _, body := range []string{
		// Enabled, on a clock, no chain — the ONE that is a symptom.
		`{"name":"broken","question":"?","applies_to":["host"],"enabled":true,"interval_seconds":3600}`,
		// Switched off deliberately.
		`{"name":"switched off","question":"?","applies_to":["host"],"enabled":false,"interval_seconds":3600}`,
		// No clock: it runs when somebody presses the button.
		`{"name":"on demand","question":"?","applies_to":["host"],"enabled":true}`,
	} {
		if res := s.post(t, "/v1/orgs/"+org.String()+"/checks", body, owner); res.StatusCode != http.StatusCreated {
			t.Fatalf("add check %s: %d", body, res.StatusCode)
		}
	}

	res := s.get(t, "/v1/workspaces/"+workspace.String()+"/health", owner)
	var out healthResponse
	decode(t, res, &out)

	named := map[string]bool{}
	for _, one := range out.Symptoms {
		if one.Kind == "check_unrunnable" {
			named[one.Subject] = true
		}
	}
	if !named["broken"] {
		t.Fatalf("an enabled, clocked, chainless check IS a symptom: %+v", out.Symptoms)
	}
	for _, quiet := range []string{"switched off", "on demand"} {
		if named[quiet] {
			t.Fatalf("%q is not broken and must not be reported", quiet)
		}
	}
	if len(named) != 1 {
		t.Fatalf("exactly one symptom: %v", named)
	}
}

// An ARCHIVED tool with no mappings is not a symptom — nobody is going to run
// it. Reporting every tool a firm ever retired would bury the one that matters.
func TestAnArchivedToolIsNotASymptom(t *testing.T) {
	s := tracedSystem(t)
	org, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)

	res := s.post(t, "/v1/orgs/"+org.String()+"/tools",
		`{"name":"retired","argv":"echo {{target}}","intensity":"passive","produces":"host"}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add tool: %d", res.StatusCode)
	}
	var tool toolResponse
	decode(t, res, &tool)
	if res := s.delete(t, "/v1/orgs/"+org.String()+"/tools/"+tool.ToolID, owner); res.StatusCode != http.StatusNoContent {
		t.Fatalf("archive: %d", res.StatusCode)
	}

	res = s.get(t, "/v1/workspaces/"+workspace.String()+"/health", owner)
	var out healthResponse
	decode(t, res, &out)
	for _, one := range out.Symptoms {
		if one.Subject == "retired" {
			t.Fatalf("an archived tool was reported: %+v", one)
		}
	}
	// And the DENOMINATOR excludes it too, or "0 of 1" would count a tool
	// nobody will run.
	for _, p := range out.Probes {
		if p.Kind == "tool_unread" && *p.Looked != 0 {
			t.Fatalf("an archived tool was considered: looked %d", *p.Looked)
		}
	}
}

// **ONE PROBLEM, ONE SYMPTOM.** A finding tool with NO mappings at all is
// `tool_unread`; it must not ALSO be `tool_no_signature`, or a single
// untaught tool appears twice and the count is a lie.
func TestAToolWithNoMappingsIsNotAlsoReportedAsUnsigned(t *testing.T) {
	s := tracedSystem(t)
	org, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)

	res := s.post(t, "/v1/orgs/"+org.String()+"/tools",
		`{"name":"nuclei","argv":"nuclei -u {{url}}","intensity":"loud",`+
			`"consumes":"url","produces":"finding"}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add tool: %d", res.StatusCode)
	}

	res = s.get(t, "/v1/workspaces/"+workspace.String()+"/health", owner)
	var out healthResponse
	decode(t, res, &out)

	kinds := map[string]int{}
	for _, one := range out.Symptoms {
		if one.Subject == "nuclei" {
			kinds[one.Kind]++
		}
	}
	if kinds["tool_unread"] != 1 {
		t.Fatalf("a tool with no mappings is unread: %v", kinds)
	}
	if kinds["tool_no_signature"] != 0 {
		t.Fatalf("and must not be counted twice: %v", kinds)
	}
}

// A tool nobody has taught this system to read runs and says nothing. It is an
// ordinary state, and it is indistinguishable from a tool that found nothing —
// which is why it is on this list.
func TestAToolWithNoMappingsIsASymptom(t *testing.T) {
	s := tracedSystem(t)
	org, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)

	res := s.post(t, "/v1/orgs/"+org.String()+"/tools",
		`{"name":"subfinder","argv":"subfinder -d {{target}}","intensity":"passive","produces":"host"}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add tool: %d", res.StatusCode)
	}

	res = s.get(t, "/v1/workspaces/"+workspace.String()+"/health", owner)
	var out healthResponse
	decode(t, res, &out)

	var found bool
	for _, one := range out.Symptoms {
		if one.Kind == "tool_unread" && one.Subject == "subfinder" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a tool with no live mapping is a symptom: %+v", out.Symptoms)
	}
}
