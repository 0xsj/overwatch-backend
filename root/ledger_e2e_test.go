package root

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type entryRow struct {
	ID          string `json:"id"`
	Scope       string `json:"scope"`
	Action      string `json:"action"`
	Subject     string `json:"subject"`
	Actor       string `json:"actor"`
	WorkspaceID string `json:"workspace_id"`
	Correlation string `json:"correlation_id"`
	OccurredAt  string `json:"occurred_at"`
}

type facetRow struct {
	Facet string `json:"facet"`
	Total int    `json:"total"`
}

type pageRow struct {
	Entries []entryRow `json:"entries"`
	Next    string     `json:"next"`
	Facets  []facetRow `json:"facets"`
}

type stepRow struct {
	Action      string `json:"action"`
	Subject     string `json:"subject"`
	WorkspaceID string `json:"workspace_id"`
	Depth       int    `json:"depth"`
	Decision    bool   `json:"decision"`
}

// My activity is what happened TO MY ACCOUNT, and nobody else's.
func TestMyActivityIsMineAndOnlyMine(t *testing.T) {
	s := tracedSystem(t)
	sam, samAuth := s.signUp(t, "sam@example.com")
	_, kitAuth := s.signUp(t, "kit@example.com")
	s.drain(t)

	var mine pageRow
	decode(t, s.get(t, "/v1/me/activity", samAuth), &mine)
	if len(mine.Entries) == 0 {
		t.Fatal("no activity for an account that registered and signed in")
	}
	for _, e := range mine.Entries {
		if e.Subject != "account:"+sam.String() {
			t.Errorf("somebody else's row on my activity: %+v", e)
		}
		if e.Scope != "account" {
			t.Errorf("scope %q on an account activity row", e.Scope)
		}
	}

	// Registration and sign-in are both decisions, so both are here. Org and
	// workspace provisioning are work and are NOT — decisions/0014.
	seen := map[string]bool{}
	for _, e := range mine.Entries {
		seen[e.Action] = true
	}
	for _, want := range []string{"identity.account.created", "identity.session.started"} {
		if !seen[want] {
			t.Errorf("%s missing from my activity", want)
		}
	}
	for _, notThere := range []string{"org.created", "workspace.created"} {
		if seen[notThere] {
			t.Errorf("%s is on an account's activity page", notThere)
		}
	}

	// Kit's page is disjoint. Nothing about the endpoint takes an account id,
	// so there is no id to tamper with — but assert the outcome anyway.
	var theirs pageRow
	decode(t, s.get(t, "/v1/me/activity", kitAuth), &theirs)
	for _, e := range theirs.Entries {
		if e.Subject == "account:"+sam.String() {
			t.Fatalf("kit can see sam's activity: %+v", e)
		}
	}
}

// The engagement audit log is gated by the grant, and a caller with none gets
// the same answer as one naming a workspace that does not exist.
func TestTheEngagementAuditLogIsGatedByTheGrant(t *testing.T) {
	s := tracedSystem(t)

	_, ownerAuth := s.signUp(t, "sam@example.com")
	verify(t, s, ownerAuth)
	seen := s.me(t, ownerAuth)
	org := seen.Orgs[0].OrgID

	res := s.post(t, "/v1/orgs/"+org+"/workspaces", `{"name":"Acme Q3"}`, ownerAuth)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("open: %d", res.StatusCode)
	}
	var opened workspaceResponse
	decode(t, res, &opened)
	s.drain(t)

	// The owner reads it. `workspace.opened` is the decision that reaches audit.
	var log pageRow
	decode(t, s.get(t, "/v1/workspaces/"+opened.WorkspaceID+"/audit", ownerAuth), &log)
	if len(log.Entries) != 1 {
		t.Fatalf("%d entries on a fresh engagement: %+v", len(log.Entries), log.Entries)
	}
	if log.Entries[0].Action != "workspace.opened" {
		t.Errorf("first entry is %q", log.Entries[0].Action)
	}
	if log.Entries[0].Scope != "workspace" || log.Entries[0].WorkspaceID != opened.WorkspaceID {
		t.Errorf("entry is not scoped to the workspace: %+v", log.Entries[0])
	}
	if log.Next != "" {
		t.Errorf("a cursor was returned for a single page: %q", log.Next)
	}

	// A stranger, an absent workspace and a malformed id are one answer.
	_, strangerAuth := s.signUp(t, "kit@example.com")
	for name, path := range map[string]string{
		"a real workspace they are not on": "/v1/workspaces/" + opened.WorkspaceID + "/audit",
		"one that does not exist":          "/v1/workspaces/01a07b02-0000-7000-0000-000000000000/audit",
		"a malformed id":                   "/v1/workspaces/not-an-id/audit",
	} {
		if res := s.get(t, path, strangerAuth); res.StatusCode != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", name, res.StatusCode)
		}
	}
}

// The chain is the "what else was part of this" link, and it crosses three
// domains. The registration chain is the case that proves it: one act, three
// subjects, one correlation.
func TestAChainJoinsWhatOneActDidAcrossThreeDomains(t *testing.T) {
	s := tracedSystem(t)
	_, auth := s.signUp(t, "sam@example.com")
	s.drain(t)

	var mine pageRow
	decode(t, s.get(t, "/v1/me/activity", auth), &mine)
	var correlation string
	for _, e := range mine.Entries {
		if e.Action == "identity.account.created" {
			correlation = e.Correlation
		}
	}
	if correlation == "" {
		t.Fatal("the registration entry carries no correlation")
	}

	var steps []stepRow
	decode(t, s.get(t, "/v1/chains/"+correlation, auth), &steps)
	if len(steps) != 3 {
		t.Fatalf("%d steps in the registration chain: %+v", len(steps), steps)
	}

	// Oldest first, and depth increasing — it is read as a story.
	want := []struct {
		action string
		depth  int
	}{
		{"identity.account.created", 0},
		{"org.created", 1},
		{"workspace.created", 2},
	}
	for i, w := range want {
		if steps[i].Action != w.action || steps[i].Depth != w.depth {
			t.Errorf("step %d is %s at depth %d, want %s at %d",
				i, steps[i].Action, steps[i].Depth, w.action, w.depth)
		}
	}
	// The middle step is org's, and it is visible because the caller is a
	// member — not because of a grant, and not because of the subject.
	if steps[1].Subject[:4] != "org:" {
		t.Errorf("the middle step is subjected to %q", steps[1].Subject)
	}
	// Only the first is a decision. The other two are work — decisions/0014.
	if !steps[0].Decision || steps[1].Decision || steps[2].Decision {
		t.Errorf("decision flags: %v %v %v",
			steps[0].Decision, steps[1].Decision, steps[2].Decision)
	}
}

// A stranger sees none of a chain, and is told nothing about how much there was.
func TestAStrangerSeesNoneOfSomebodyElsesChain(t *testing.T) {
	s := tracedSystem(t)
	_, auth := s.signUp(t, "sam@example.com")
	s.drain(t)

	var mine pageRow
	decode(t, s.get(t, "/v1/me/activity", auth), &mine)
	correlation := mine.Entries[0].Correlation

	_, strangerAuth := s.signUp(t, "kit@example.com")
	s.drain(t)
	if res := s.get(t, "/v1/chains/"+correlation, strangerAuth); res.StatusCode != http.StatusNotFound {
		t.Errorf("a stranger read somebody else's chain: %d", res.StatusCode)
	}
	// And a correlation that never existed answers identically.
	if res := s.get(t, "/v1/chains/01a07b02-0000-7000-0000-000000000000", strangerAuth); res.StatusCode != http.StatusNotFound {
		t.Errorf("an invented correlation answered %d", res.StatusCode)
	}
}

// Keyset paging: the cursor must not repeat or skip a row, and the registration
// chain writes several entries inside one millisecond — which is exactly the tie
// an occurred_at-only cursor gets wrong.
func TestPagingTheLedgerNeitherRepeatsNorSkips(t *testing.T) {
	s := tracedSystem(t)
	_, auth := s.signUp(t, "sam@example.com")

	// Enough rows to page through in ones.
	for i := 0; i < 5; i++ {
		if res := s.post(t, "/v1/sessions",
			`{"email":"sam@example.com","password":`+quote(pass)+`}`, nil); res.StatusCode != http.StatusCreated {
			t.Fatalf("sign in %d: %d", i, res.StatusCode)
		}
	}
	s.drain(t)

	var all pageRow
	decode(t, s.get(t, "/v1/me/activity", auth), &all)
	if len(all.Entries) < 6 {
		t.Fatalf("only %d entries to page through", len(all.Entries))
	}

	seen := []string{}
	next := ""
	for page := 0; page < 20; page++ {
		path := "/v1/me/activity?limit=1"
		if next != "" {
			path += "&after=" + url.QueryEscape(next)
		}
		var got pageRow
		decode(t, s.get(t, path, auth), &got)
		if len(got.Entries) == 0 {
			break
		}
		seen = append(seen, got.Entries[0].ID)
		if got.Next == "" {
			break
		}
		next = got.Next
	}

	if len(seen) != len(all.Entries) {
		t.Errorf("paged %d rows, the unpaged read has %d", len(seen), len(all.Entries))
	}
	unique := map[string]bool{}
	for _, row := range seen {
		if unique[row] {
			t.Errorf("row %s came back twice", row)
		}
		unique[row] = true
	}
	for i, row := range seen {
		if i < len(all.Entries) && row != all.Entries[i].ID {
			t.Errorf("page %d gave %s, the unpaged read has %s", i, row, all.Entries[i].ID)
		}
	}
}

// Facets are the action's first segment, counted over the WHOLE subject and
// never over the filtered set — the rule that lets a reader leave a facet they
// have entered.
func TestFacetsCountTheWholeSubjectAndFilterThePage(t *testing.T) {
	s := tracedSystem(t)
	_, auth := s.signUp(t, "sam@example.com")
	verify(t, s, auth)
	// A workspace.opened, so there is a second facet on the account's own page…
	// there is not: workspace events are workspace-scoped. So make more identity
	// events instead, which is the honest shape of an account's activity.
	for i := 0; i < 2; i++ {
		if res := s.post(t, "/v1/sessions",
			`{"email":"sam@example.com","password":`+quote(pass)+`}`, nil); res.StatusCode != http.StatusCreated {
			t.Fatalf("sign in: %d", res.StatusCode)
		}
	}
	s.drain(t)

	var all pageRow
	decode(t, s.get(t, "/v1/me/activity", auth), &all)
	if len(all.Facets) == 0 {
		t.Fatal("the first page carried no facets")
	}
	total := 0
	for _, f := range all.Facets {
		total += f.Total
		if strings.Contains(f.Facet, ".") {
			t.Errorf("facet %q is not a first segment", f.Facet)
		}
	}
	if total != len(all.Entries) {
		t.Errorf("facets total %d, the page has %d entries", total, len(all.Entries))
	}

	// Filtering to a facet returns only that facet's rows — and STILL reports
	// every facet's count, so the reader can switch to another one.
	var filtered pageRow
	decode(t, s.get(t, "/v1/me/activity?facet=identity", auth), &filtered)
	for _, e := range filtered.Entries {
		if !strings.HasPrefix(e.Action, "identity.") {
			t.Errorf("a %q row came back under facet=identity", e.Action)
		}
	}
	if len(filtered.Facets) != len(all.Facets) {
		t.Errorf("filtering changed the facet list: %v vs %v", filtered.Facets, all.Facets)
	}
	for i := range filtered.Facets {
		if filtered.Facets[i] != all.Facets[i] {
			t.Errorf("facet counts moved under a filter: %+v vs %+v",
				filtered.Facets[i], all.Facets[i])
		}
	}

	// An unknown facet is an empty page and not an error: the facet set is
	// whatever actions exist, and an empty answer is correct.
	var unknown pageRow
	res := s.get(t, "/v1/me/activity?facet=nonesuch", auth)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("an unknown facet answered %d", res.StatusCode)
	}
	decode(t, res, &unknown)
	if len(unknown.Entries) != 0 {
		t.Errorf("%d entries under an unknown facet", len(unknown.Entries))
	}

	// Facets are on the FIRST page only. A later page must not pay for them.
	var page1 pageRow
	decode(t, s.get(t, "/v1/me/activity?limit=1", auth), &page1)
	if page1.Next == "" {
		t.Fatal("no second page to check")
	}
	var page2 pageRow
	decode(t, s.get(t, "/v1/me/activity?limit=1&after="+url.QueryEscape(page1.Next), auth), &page2)
	if len(page2.Facets) != 0 {
		t.Errorf("a later page recomputed facets: %+v", page2.Facets)
	}
}
