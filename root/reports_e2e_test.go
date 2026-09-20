// Author-written, against a live database, from decisions/0042's Verification
// block. The fail-closed gate is the claim worth most here: it changes routes
// nobody edited, and nothing else in the suite would notice if it stopped
// holding.
package root

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	reportdomain "github.com/0xsj/overwatch-backend/internal/report/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// engagementWithAClient seats `kit` as a CLIENT on a workspace with `read`. That
// is the shape 0019 built and left empty: a ceiling of `read`, and a role that
// removes routes.
func engagementWithAClient(t *testing.T, s traced) (
	ws, target id.ID, ownerAuth, clientAuth map[string]string) {
	t.Helper()
	_, workspace, _, kit, owner, kitAuth := firm(t, s, orgdomain.RoleClient)

	seat := "/v1/workspaces/" + workspace.String() + "/members/" + kit.String()
	if res := s.put(t, seat, `{"level":"read"}`, owner); res.StatusCode != http.StatusOK {
		t.Fatalf("grant read to a client: %d", res.StatusCode)
	}

	res := s.post(t, "/v1/workspaces/"+workspace.String()+"/targets",
		`{"name":"acme.test","kind":"organisation"}`, owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add target: %d", res.StatusCode)
	}
	var subject targetResponse
	decode(t, res, &subject)
	parsed, err := id.Parse(subject.TargetID)
	if err != nil {
		t.Fatal(err)
	}
	return workspace, parsed, owner, kitAuth
}

func openReport(t *testing.T, s traced, ws, target id.ID, auth map[string]string) reportResponse {
	t.Helper()
	res := s.post(t, "/v1/workspaces/"+ws.String()+"/reports",
		`{"target_id":"`+target.String()+`","title":"Acme Q3","prepared_by":"Northbeam"}`, auth)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("open report: %d", res.StatusCode)
	}
	var out reportResponse
	decode(t, res, &out)
	return out
}

// **THE test of decisions/0042 §5.** A client reaches a deliverable and nothing
// in the research workspace; every refusal is a 404 — a client learning that an
// invocation log exists is a client learning what was run against them.
func TestAClientReachesReportsAndNothingElse(t *testing.T) {
	s := tracedSystem(t)
	ws, target, owner, client := engagementWithAClient(t, s)
	opened := openReport(t, s, ws, target, owner)

	base := "/v1/workspaces/" + ws.String()
	for _, path := range []string{
		base + "/runs",
		base + "/findings",
		base + "/observations",
		base + "/fragments",
		base + "/entities",
		base + "/targets",
		base + "/members",
	} {
		res := s.get(t, path, client)
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("a client read %s: got %d, want 404", path, res.StatusCode)
		}
	}

	// AND THE REPORT ROUTES ANSWER.
	if res := s.get(t, base+"/reports", client); res.StatusCode != http.StatusOK {
		t.Fatalf("a client listing reports: %d", res.StatusCode)
	}
	if res := s.get(t, base+"/reports/"+opened.ReportID, client); res.StatusCode != http.StatusOK {
		t.Fatalf("a client reading a report: %d", res.StatusCode)
	}
}

// The same routes for a NON-client with the same level still answer. The gate
// removes routes by ROLE, and a change that removed them for everybody would
// pass the test above and fail this one.
func TestANonClientWithReadStillReachesEverything(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, kit, owner, kitAuth := firm(t, s, orgdomain.RoleMember)
	seat := "/v1/workspaces/" + workspace.String() + "/members/" + kit.String()
	if res := s.put(t, seat, `{"level":"read"}`, owner); res.StatusCode != http.StatusOK {
		t.Fatalf("grant: %d", res.StatusCode)
	}
	base := "/v1/workspaces/" + workspace.String()
	for _, path := range []string{base + "/runs", base + "/findings", base + "/reports"} {
		if res := s.get(t, path, kitAuth); res.StatusCode != http.StatusOK {
			t.Errorf("a member with read got %d on %s", res.StatusCode, path)
		}
	}
}

// A client's ceiling is `read` — 0019 — so `write` is refused by the LADDER
// rather than by the role. Both halves matter: the ladder stays a clean min()
// and the role removes routes.
func TestAClientCannotOpenOrIssueAReport(t *testing.T) {
	s := tracedSystem(t)
	ws, target, owner, client := engagementWithAClient(t, s)
	opened := openReport(t, s, ws, target, owner)
	base := "/v1/workspaces/" + ws.String()

	res := s.post(t, base+"/reports",
		`{"target_id":"`+target.String()+`","title":"Mine"}`, client)
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("a client opened a report: %d", res.StatusCode)
	}
	res = s.post(t, base+"/reports/"+opened.ReportID+"/revisions", "", client)
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("a client issued a report: %d", res.StatusCode)
	}
}

// Issuing FREEZES. Re-issuing writes a SECOND revision beside the first, and the
// first stays byte-identical — a report sent in January and re-sent in March is
// two documents.
func TestIssuingFreezesAndReIssuingLeavesTheFirstUntouched(t *testing.T) {
	s := tracedSystem(t)
	ws, target, owner, _ := engagementWithAClient(t, s)
	opened := openReport(t, s, ws, target, owner)
	base := "/v1/workspaces/" + ws.String() + "/reports/" + opened.ReportID

	res := s.post(t, base+"/revisions", "", owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("issue: %d", res.StatusCode)
	}
	var first revisionResponse
	decode(t, res, &first)
	if first.Number != 1 || first.Hash == "" {
		t.Fatalf("first revision: %+v", first)
	}
	firstBytes := revisionBody(t, s, ws, first.RevisionID, owner)

	// Turn ON the invocation log, which is one of the two withheld sections,
	// and issue again.
	if res := s.put(t, base+"/sections/invocation_log", `{"enabled":true}`, owner); res.StatusCode != http.StatusOK {
		t.Fatalf("toggle: %d", res.StatusCode)
	}
	res = s.post(t, base+"/revisions", "", owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("re-issue: %d", res.StatusCode)
	}
	var second revisionResponse
	decode(t, res, &second)
	if second.Number != 2 {
		t.Fatalf("second revision: %+v", second)
	}
	if second.Hash == first.Hash {
		t.Fatal("a different section set must produce different bytes")
	}
	if len(second.Sections) != len(first.Sections)+1 {
		t.Fatalf("the second contains one more section: %v vs %v", second.Sections, first.Sections)
	}

	// THE FIRST IS BYTE-IDENTICAL. This is the whole argument for freezing.
	if got := revisionBody(t, s, ws, first.RevisionID, owner); string(got) != string(firstBytes) {
		t.Fatal("re-issuing changed what the first revision said")
	}
}

// A DISABLED section is ABSENT from the bytes, not present and empty. An empty
// section reads as "we looked and there was nothing", which is the pair
// CLAUDE.md refuses to collapse.
func TestADisabledSectionIsAbsentAndTheWithheldOnesAreNamed(t *testing.T) {
	s := tracedSystem(t)
	ws, target, owner, _ := engagementWithAClient(t, s)
	opened := openReport(t, s, ws, target, owner)

	res := s.post(t, "/v1/workspaces/"+ws.String()+"/reports/"+opened.ReportID+"/revisions", "", owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("issue: %d", res.StatusCode)
	}
	var rev revisionResponse
	decode(t, res, &rev)

	var doc struct {
		Sections []struct {
			Number int    `json:"number"`
			Key    string `json:"key"`
		} `json:"sections"`
		Withheld []string `json:"withheld"`
	}
	if err := json.Unmarshal(revisionBody(t, s, ws, rev.RevisionID, owner), &doc); err != nil {
		t.Fatal(err)
	}
	// SIX by default since 0043 — the eighth section, engagement notes, ships
	// ON. Derived from the domain rather than hardcoded, because this number
	// moves whenever a section is added and this test is not about the count.
	want := 0
	for _, one := range reportdomain.All {
		if one.DefaultOn() {
			want++
		}
	}
	if len(doc.Sections) != want {
		t.Fatalf("%d sections by default, got %d", want, len(doc.Sections))
	}
	for _, one := range doc.Sections {
		if one.Key == "invocation_log" || one.Key == "raw_artifacts" {
			t.Fatalf("a disabled section is ABSENT, not empty: %s", one.Key)
		}
	}
	// NUMBERS RENUMBER over the enabled set, 1..5 with no gaps.
	for n, one := range doc.Sections {
		if one.Number != n+1 {
			t.Fatalf("section %s is numbered %d, want %d", one.Key, one.Number, n+1)
		}
	}
	// And the document SAYS WHAT IT LEFT OUT, which is the argument for
	// section 5 applied to the document itself.
	if len(doc.Withheld) != 2 {
		t.Fatalf("both withheld sections are named: %v", doc.Withheld)
	}
}

// `withheld` names what was LEFT OUT and not simply which sections are
// withholdable. A mutation round found that unkilled: every other test here
// leaves both off, so listing them unconditionally passed.
func TestWithheldNamesOnlyTheSectionsActuallyLeftOut(t *testing.T) {
	s := tracedSystem(t)
	ws, target, owner, _ := engagementWithAClient(t, s)
	opened := openReport(t, s, ws, target, owner)
	base := "/v1/workspaces/" + ws.String() + "/reports/" + opened.ReportID

	// Turn ON the invocation log. The artifacts appendix stays off.
	if res := s.put(t, base+"/sections/invocation_log", `{"enabled":true}`, owner); res.StatusCode != http.StatusOK {
		t.Fatalf("toggle: %d", res.StatusCode)
	}
	res := s.post(t, base+"/revisions", "", owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("issue: %d", res.StatusCode)
	}
	var rev revisionResponse
	decode(t, res, &rev)

	var doc struct {
		Sections []struct {
			Key    string `json:"key"`
			Counts []struct {
				Label string `json:"label"`
				Value int    `json:"value"`
			} `json:"counts"`
		} `json:"sections"`
		Withheld []string `json:"withheld"`
	}
	if err := json.Unmarshal(revisionBody(t, s, ws, rev.RevisionID, owner), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Withheld) != 1 || doc.Withheld[0] != "raw_artifacts" {
		t.Fatalf("only the section actually left out is named: %v", doc.Withheld)
	}
	var present bool
	for _, one := range doc.Sections {
		if one.Key == "invocation_log" {
			present = true
		}
	}
	if !present {
		t.Fatal("the enabled section is in the document")
	}
}

// **A DISMISSED finding is excluded from the rows and COUNTED separately.**
// "8 open · 1 dismissed and excluded" is one sentence saying both; a silently
// shorter list says neither, and a count that folded them together would say
// something false.
func TestDismissedFindingsAreCountedApartFromOpenOnes(t *testing.T) {
	s := tracedSystem(t)
	ws, target, owner, _ := engagementWithAClient(t, s)
	opened := openReport(t, s, ws, target, owner)

	res := s.post(t, "/v1/workspaces/"+ws.String()+"/reports/"+opened.ReportID+"/revisions", "", owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("issue: %d", res.StatusCode)
	}
	var rev revisionResponse
	decode(t, res, &rev)

	var doc struct {
		Sections []struct {
			Key    string `json:"key"`
			Counts []struct {
				Label string `json:"label"`
				Value int    `json:"value"`
			} `json:"counts"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(revisionBody(t, s, ws, rev.RevisionID, owner), &doc); err != nil {
		t.Fatal(err)
	}
	// No scans have run, so both are zero — and the LABELS are the claim: the
	// findings section must carry two counts and keep them apart.
	labels := map[string]bool{}
	for _, one := range doc.Sections {
		if one.Key != "findings_by_severity" {
			continue
		}
		for _, c := range one.Counts {
			labels[c.Label] = true
		}
	}
	for _, want := range []string{"open", "dismissed and excluded"} {
		if !labels[want] {
			t.Fatalf("the findings section must count %q apart: %v", want, labels)
		}
	}
}

// Coverage cannot be turned off, and the refusal says why rather than naming a
// constraint.
func TestCoverageCannotBeDisabledOverTheWire(t *testing.T) {
	s := tracedSystem(t)
	ws, target, owner, _ := engagementWithAClient(t, s)
	opened := openReport(t, s, ws, target, owner)
	res := s.put(t, "/v1/workspaces/"+ws.String()+"/reports/"+opened.ReportID+"/sections/coverage",
		`{"enabled":false}`, owner)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if !contains(string(body), "completeness nobody achieved") {
		t.Fatalf("the refusal must name the thesis: %s", body)
	}
}

func revisionBody(t *testing.T, s traced, ws id.ID, revision string, auth map[string]string) []byte {
	t.Helper()
	res := s.get(t, "/v1/workspaces/"+ws.String()+"/revisions/"+revision, auth)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("read revision: %d", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
