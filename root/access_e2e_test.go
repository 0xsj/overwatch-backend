package root

import (
	"context"
	"crypto/rand"
	"net/http"
	"testing"
	"time"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	orgpg "github.com/0xsj/overwatch-backend/internal/org/infra/postgres"
	sourcedomain "github.com/0xsj/overwatch-backend/internal/source/domain"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// A second member is written directly through the store, because no invite
// command exists yet — decisions/0019 defines the roles and says the flow that
// creates them arrives with the invite. Reaching past the (absent) command is
// legitimate here: what is under test is the GATE, not how a member got there.
// seat puts somebody in an org. A TIME-BOXED role gets a date far enough out
// that no test measures it — owed item D makes `guest` and `client` require one,
// and a suite about something else must not be a suite about expiry.
//
// `seatUntil` is the variant for tests that ARE about it.
func (s traced) seat(t *testing.T, org, account id.ID, role orgdomain.Role) {
	t.Helper()
	until := time.Time{}
	if role.TimeBoxed() {
		until = time.Now().Add(365 * 24 * time.Hour)
	}
	s.seatUntil(t, org, account, role, until)
}

func (s traced) seatUntil(t *testing.T, org, account id.ID, role orgdomain.Role, until time.Time) {
	t.Helper()
	ctx := context.Background()
	ids := id.NewV7(clock.System{}, rand.Reader)
	member, err := orgdomain.NewMember(ids.NewID(), org, account, role, until, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := orgpg.NewStore(s.pool).AddMember(ctx, member); err != nil {
		t.Fatal(err)
	}
}

func (s traced) grant(t *testing.T, org, account, workspace id.ID, level orgdomain.Level) {
	t.Helper()
	ctx := context.Background()
	ids := id.NewV7(clock.System{}, rand.Reader)
	g, err := orgdomain.NewGrant(ids.NewID(), org, account, workspace, level, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := orgpg.NewStore(s.pool).AddGrant(ctx, g); err != nil {
		t.Fatal(err)
	}
}

// signUp walks a whole registration and hands back a signed-in caller.
func (s traced) signUp(t *testing.T, email string) (account id.ID, auth map[string]string) {
	t.Helper()
	if res := s.register(t, email, nil); res.StatusCode != http.StatusCreated {
		t.Fatalf("register %s: %d", email, res.StatusCode)
	}
	s.drain(t)

	var in struct {
		Token     string `json:"token"`
		AccountID string `json:"account_id"`
	}
	decode(t, s.post(t, "/v1/sessions",
		`{"email":`+quote(email)+`,"password":"a passphrase nobody guesses"}`, nil), &in)
	parsed, err := id.Parse(in.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	return parsed, map[string]string{"authorization": "Bearer " + in.Token}
}

func (s traced) me(t *testing.T, auth map[string]string) meResponse {
	t.Helper()
	var out meResponse
	decode(t, s.get(t, "/v1/me", auth), &out)
	return out
}

// This is the bug decisions/0019 predicted and the reason it was written before
// any of this shipped: /v1/me listed every workspace of every org the caller was
// a member of, with no grant consulted. It could not fail while every org had
// exactly one member, because that member is the owner and the owner is exempt.
func TestAMemberWithNoGrantCannotSeeAWorkspace(t *testing.T) {
	s := tracedSystem(t)

	owner, ownerAuth := s.signUp(t, "sam@example.com")
	_ = owner

	seen := s.me(t, ownerAuth)
	if len(seen.Orgs) != 1 || len(seen.Orgs[0].Workspaces) != 1 {
		t.Fatalf("the owner sees %d orgs", len(seen.Orgs))
	}
	if got := seen.Orgs[0].Workspaces[0].Access; got != "admin" {
		t.Errorf("the owner's access is %q, not admin — the exemption is gone", got)
	}
	org, err := id.Parse(seen.Orgs[0].OrgID)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := id.Parse(seen.Orgs[0].Workspaces[0].WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}

	// A second person, in the same org, as a plain member. They have their own
	// org from their own registration; this seats them in Sam's.
	kit, kitAuth := s.signUp(t, "kit@example.com")
	s.seat(t, org, kit, orgdomain.RoleMember)

	seen = s.me(t, kitAuth)
	var sams *meOrg
	for i := range seen.Orgs {
		if seen.Orgs[i].OrgID == org.String() {
			sams = &seen.Orgs[i]
		}
	}
	if sams == nil {
		t.Fatal("the new member cannot see the org they belong to")
	}
	if sams.Role != "member" {
		t.Errorf("seated as %q", sams.Role)
	}
	// The whole point. The workspace EXISTS, they are a member of the org that
	// owns it, and they must not see that it is there.
	if len(sams.Workspaces) != 0 {
		t.Fatalf("a member with no grant sees %d workspaces: %+v",
			len(sams.Workspaces), sams.Workspaces)
	}

	// Grant read, and it appears — with the level, not just the name.
	s.grant(t, org, kit, workspace, orgdomain.LevelRead)
	seen = s.me(t, kitAuth)
	for i := range seen.Orgs {
		if seen.Orgs[i].OrgID != org.String() {
			continue
		}
		if len(seen.Orgs[i].Workspaces) != 1 {
			t.Fatalf("after a grant, %d workspaces", len(seen.Orgs[i].Workspaces))
		}
		if got := seen.Orgs[i].Workspaces[0].Access; got != "read" {
			t.Errorf("access %q after a read grant", got)
		}
	}
}

// An admin manages people, not work — decisions/0019. Promoting somebody to
// handle invitations must not hand them every client's wall.
func TestAnAdminIsNotExemptTheWayAnOwnerIs(t *testing.T) {
	s := tracedSystem(t)

	_, ownerAuth := s.signUp(t, "sam@example.com")
	seen := s.me(t, ownerAuth)
	org, err := id.Parse(seen.Orgs[0].OrgID)
	if err != nil {
		t.Fatal(err)
	}

	kit, kitAuth := s.signUp(t, "kit@example.com")
	s.seat(t, org, kit, orgdomain.RoleAdmin)

	for _, o := range s.me(t, kitAuth).Orgs {
		if o.OrgID != org.String() {
			continue
		}
		if o.Role != "admin" {
			t.Fatalf("seated as %q", o.Role)
		}
		if len(o.Workspaces) != 0 {
			t.Errorf("an admin with no grant sees %d workspaces", len(o.Workspaces))
		}
	}
}

// A caller who is not a member gets NotFound and never Forbidden. A 403 tells an
// analyst that a client they cannot see exists, which for the client behind that
// wall is the leak itself — decisions/0005.
func TestAStrangerCannotTellAnOrgApartFromNothing(t *testing.T) {
	s := tracedSystem(t)

	_, ownerAuth := s.signUp(t, "sam@example.com")
	org := s.me(t, ownerAuth).Orgs[0].OrgID

	_, strangerAuth := s.signUp(t, "kit@example.com")

	real := s.get(t, "/v1/orgs/"+org+"/members", strangerAuth)
	invented := s.get(t, "/v1/orgs/01a07b02-0000-7000-0000-000000000000/members", strangerAuth)
	malformed := s.get(t, "/v1/orgs/not-an-id/members", strangerAuth)

	for name, res := range map[string]*http.Response{
		"an org that exists": real, "one that does not": invented, "a malformed id": malformed,
	} {
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", name, res.StatusCode)
		}
	}
}

func TestRestrictedSourceIsHiddenFromReadMembersAndSearch(t *testing.T) {
	s := tracedSystem(t)
	org, workspace, _, _, ownerAuth, _ := firm(t, s, orgdomain.RoleMember)
	source := addResearchSource(t, s, workspace, ownerAuth, "Restricted source passage that must not appear in a read-member search.")
	base := "/v1/workspaces/" + workspace.String()
	sourceBase := base + "/sources/" + source.ID.String()
	researchStatus(t, s.put(t, sourceBase+"/privacy", researchJSON(t, map[string]any{
		"sensitivity": sourcedomain.SensitivityRestricted,
	}), ownerAuth), http.StatusOK)

	member, memberAuth := s.signUp(t, "restricted-reader@example.com")
	s.seat(t, org, member, orgdomain.RoleMember)
	s.grant(t, org, member, workspace, orgdomain.LevelRead)

	var sources struct {
		Items []struct {
			SourceID string `json:"source_id"`
			Title    string `json:"title"`
		} `json:"items"`
	}
	list := s.get(t, base+"/sources", memberAuth)
	researchStatus(t, list, http.StatusOK)
	decode(t, list, &sources)
	for _, item := range sources.Items {
		if item.SourceID == source.ID.String() || item.Title == source.Title {
			t.Fatalf("restricted source leaked into a read-member source list: %+v", item)
		}
	}

	search := s.get(t, base+"/search?q=Restricted+source+passage", memberAuth)
	researchStatus(t, search, http.StatusOK)
	var results struct {
		Items []struct {
			SourceID string `json:"source_id"`
		} `json:"items"`
	}
	decode(t, search, &results)
	if len(results.Items) != 0 {
		t.Fatalf("restricted source leaked into read-member search: %+v", results.Items)
	}

	researchStatus(t, s.get(t, sourceBase, memberAuth), http.StatusNotFound)
	researchStatus(t, s.get(t, sourceBase+"/captures/"+source.LatestCapture.ID.String(), memberAuth), http.StatusNotFound)
	researchStatus(t, s.get(t, sourceBase, ownerAuth), http.StatusOK)
	ownerSearch := s.get(t, base+"/search?q=Restricted+source+passage", ownerAuth)
	researchStatus(t, ownerSearch, http.StatusOK)
	decode(t, ownerSearch, &results)
	if len(results.Items) != 1 || results.Items[0].SourceID != source.ID.String() {
		t.Fatalf("owner could not search restricted source: %+v", results.Items)
	}

	manual := recordResearchObservation(t, s, sourceBase, ownerAuth, source.LatestCapture.ID, "Restricted source passage")
	recordResponse := s.post(t, base+"/records", researchJSON(t, map[string]any{
		"kind": "account", "name": "Restricted source account", "description": "Must follow source visibility.",
		"observation_ids": []string{manual.ID.String()},
	}), ownerAuth)
	researchStatus(t, recordResponse, http.StatusCreated)
	var record researchRecordResponse
	decode(t, recordResponse, &record)
	publicSource := addResearchSource(t, s, workspace, ownerAuth, "A public companion notice for review boundaries.")
	publicManual := recordResearchObservation(t, s, base+"/sources/"+publicSource.ID.String(), ownerAuth, publicSource.LatestCapture.ID, "public companion notice")
	researchStatus(t, s.put(t, base+"/evidence/relations", researchJSON(t, map[string]any{
		"left_observation_id": manual.ID, "right_observation_id": publicManual.ID,
		"kind": "supports", "rationale": "Restricted evidence relation must remain private.",
	}), ownerAuth), http.StatusOK)
	researchStatus(t, s.put(t, base+"/evidence/source-links", researchJSON(t, map[string]any{
		"downstream_observation_id": manual.ID, "upstream_observation_id": publicManual.ID,
		"rationale": "Restricted source link must remain private.",
	}), ownerAuth), http.StatusOK)

	synthesisResponse := s.post(t, base+"/evidence/syntheses", researchJSON(t, map[string]any{
		"observation_ids": []string{manual.ID.String(), publicManual.ID.String()},
	}), ownerAuth)
	researchStatus(t, synthesisResponse, http.StatusCreated)
	var synthesis struct {
		ID string `json:"synthesis_id"`
	}
	decode(t, synthesisResponse, &synthesis)
	if synthesis.ID == "" {
		t.Fatal("owner synthesis response did not include an id")
	}

	comparisonResponse := s.post(t, base+"/evidence/comparisons", researchJSON(t, map[string]any{
		"observation_ids": []string{manual.ID.String(), publicManual.ID.String()},
	}), ownerAuth)
	researchStatus(t, comparisonResponse, http.StatusCreated)
	var comparison struct {
		ID string `json:"comparison_id"`
	}
	decode(t, comparisonResponse, &comparison)
	if comparison.ID == "" {
		t.Fatal("owner comparison response did not include an id")
	}

	questionResponse := s.post(t, base+"/evidence/question-suggestions", researchJSON(t, map[string]any{
		"gaps": []map[string]any{{
			"kind": "contradiction", "label": "Restricted source account", "detail": "The selected material needs a separate review.",
			"observation_ids": []string{manual.ID.String(), publicManual.ID.String()},
		}},
	}), ownerAuth)
	researchStatus(t, questionResponse, http.StatusCreated)
	var questions struct {
		ID string `json:"question_suggestions_id"`
	}
	decode(t, questionResponse, &questions)
	if questions.ID == "" {
		t.Fatal("owner question suggestion response did not include an id")
	}

	var memberEvidence struct {
		Items []struct {
			ObservationID string `json:"observation_id"`
		} `json:"items"`
	}
	memberEvidenceResponse := s.get(t, base+"/evidence", memberAuth)
	researchStatus(t, memberEvidenceResponse, http.StatusOK)
	decode(t, memberEvidenceResponse, &memberEvidence)
	for _, item := range memberEvidence.Items {
		if item.ObservationID == manual.ID.String() {
			t.Fatalf("restricted evidence leaked into read-member evidence list: %+v", item)
		}
	}
	var memberBoard struct {
		Items []struct {
			ObservationID string `json:"observation_id"`
		} `json:"items"`
	}
	memberBoardResponse := s.get(t, base+"/evidence/board", memberAuth)
	researchStatus(t, memberBoardResponse, http.StatusOK)
	decode(t, memberBoardResponse, &memberBoard)
	for _, item := range memberBoard.Items {
		if item.ObservationID == manual.ID.String() {
			t.Fatalf("restricted evidence leaked into read-member board: %+v", item)
		}
	}
	researchStatus(t, s.get(t, base+"/evidence/"+manual.ID.String(), memberAuth), http.StatusNotFound)
	var memberRelations struct {
		Items []struct {
			LeftObservationID  string `json:"left_observation_id"`
			RightObservationID string `json:"right_observation_id"`
		} `json:"items"`
	}
	memberRelationsResponse := s.get(t, base+"/evidence/relations", memberAuth)
	researchStatus(t, memberRelationsResponse, http.StatusOK)
	decode(t, memberRelationsResponse, &memberRelations)
	for _, item := range memberRelations.Items {
		if item.LeftObservationID == manual.ID.String() || item.RightObservationID == manual.ID.String() {
			t.Fatalf("restricted evidence relation leaked into read-member list: %+v", item)
		}
	}
	var memberSourceLinks struct {
		Items []struct {
			DownstreamObservationID string `json:"downstream_observation_id"`
			UpstreamObservationID   string `json:"upstream_observation_id"`
		} `json:"items"`
	}
	memberSourceLinksResponse := s.get(t, base+"/evidence/source-links", memberAuth)
	researchStatus(t, memberSourceLinksResponse, http.StatusOK)
	decode(t, memberSourceLinksResponse, &memberSourceLinks)
	for _, item := range memberSourceLinks.Items {
		if item.DownstreamObservationID == manual.ID.String() || item.UpstreamObservationID == manual.ID.String() {
			t.Fatalf("restricted evidence source link leaked into read-member list: %+v", item)
		}
	}
	var memberSyntheses struct {
		Items []struct {
			ID string `json:"synthesis_id"`
		} `json:"items"`
	}
	synthesisList := s.get(t, base+"/evidence/syntheses", memberAuth)
	researchStatus(t, synthesisList, http.StatusOK)
	decode(t, synthesisList, &memberSyntheses)
	for _, item := range memberSyntheses.Items {
		if item.ID == synthesis.ID {
			t.Fatalf("restricted evidence synthesis leaked into read-member list: %+v", item)
		}
	}
	researchStatus(t, s.get(t, base+"/evidence/syntheses/"+synthesis.ID, memberAuth), http.StatusNotFound)
	researchStatus(t, s.get(t, base+"/evidence/syntheses/"+synthesis.ID, ownerAuth), http.StatusOK)

	var memberComparisons struct {
		Items []struct {
			ID string `json:"comparison_id"`
		} `json:"items"`
	}
	comparisonList := s.get(t, base+"/evidence/comparisons", memberAuth)
	researchStatus(t, comparisonList, http.StatusOK)
	decode(t, comparisonList, &memberComparisons)
	for _, item := range memberComparisons.Items {
		if item.ID == comparison.ID {
			t.Fatalf("restricted evidence comparison leaked into read-member list: %+v", item)
		}
	}
	researchStatus(t, s.get(t, base+"/evidence/comparisons/"+comparison.ID, memberAuth), http.StatusNotFound)
	researchStatus(t, s.get(t, base+"/evidence/comparisons/"+comparison.ID, ownerAuth), http.StatusOK)

	var memberQuestions struct {
		Items []struct {
			ID string `json:"question_suggestions_id"`
		} `json:"items"`
	}
	questionList := s.get(t, base+"/evidence/question-suggestions", memberAuth)
	researchStatus(t, questionList, http.StatusOK)
	decode(t, questionList, &memberQuestions)
	for _, item := range memberQuestions.Items {
		if item.ID == questions.ID {
			t.Fatalf("restricted evidence question suggestions leaked into read-member list: %+v", item)
		}
	}
	researchStatus(t, s.get(t, base+"/evidence/question-suggestions/"+questions.ID, memberAuth), http.StatusNotFound)
	researchStatus(t, s.get(t, base+"/evidence/question-suggestions/"+questions.ID, ownerAuth), http.StatusOK)

	researchStatus(t, s.put(t, base+"/brief", researchJSON(t, map[string]any{
		"title": "Restricted handoff", "question": "What does the restricted notice support?", "current_account": "Owner-only handoff content.",
		"observation_ids": []string{manual.ID.String()},
	}), ownerAuth), http.StatusOK)
	draftResponse := s.post(t, base+"/brief/drafts", researchJSON(t, map[string]any{
		"observation_ids": []string{manual.ID.String()},
	}), ownerAuth)
	researchStatus(t, draftResponse, http.StatusCreated)
	var draft struct {
		ID string `json:"brief_draft_id"`
	}
	decode(t, draftResponse, &draft)
	if draft.ID == "" {
		t.Fatal("owner brief draft response did not include an id")
	}
	snapshotResponse := s.post(t, base+"/brief/snapshots", `{}`, ownerAuth)
	researchStatus(t, snapshotResponse, http.StatusCreated)
	var snapshot struct {
		ID string `json:"snapshot_id"`
	}
	decode(t, snapshotResponse, &snapshot)
	if snapshot.ID == "" {
		t.Fatal("owner snapshot response did not include an id")
	}
	shareResponse := s.post(t, base+"/brief/snapshots/"+snapshot.ID+"/shares", `{}`, ownerAuth)
	researchStatus(t, shareResponse, http.StatusCreated)
	var snapshotShare struct {
		ID    string `json:"share_id"`
		Token string `json:"token"`
	}
	decode(t, shareResponse, &snapshotShare)
	if snapshotShare.ID == "" || snapshotShare.Token == "" {
		t.Fatal("owner snapshot share response did not include an id and token")
	}
	citationShareResponse := s.post(t, base+"/sources/"+source.ID.String()+"/observations/"+manual.ID.String()+"/shares", `{}`, ownerAuth)
	researchStatus(t, citationShareResponse, http.StatusCreated)
	var citationShare struct {
		Token string `json:"token"`
	}
	decode(t, citationShareResponse, &citationShare)
	if citationShare.Token == "" {
		t.Fatal("owner citation share response did not include a token")
	}

	var memberBrief map[string]any
	briefReadResponse := s.get(t, base+"/brief", memberAuth)
	researchStatus(t, briefReadResponse, http.StatusOK)
	decode(t, briefReadResponse, &memberBrief)
	if len(memberBrief) != 0 {
		t.Fatalf("restricted working brief leaked into read-member view: %+v", memberBrief)
	}
	var memberDrafts struct {
		Items []struct {
			ID string `json:"brief_draft_id"`
		} `json:"items"`
	}
	draftListResponse := s.get(t, base+"/brief/drafts", memberAuth)
	researchStatus(t, draftListResponse, http.StatusOK)
	decode(t, draftListResponse, &memberDrafts)
	for _, item := range memberDrafts.Items {
		if item.ID == draft.ID {
			t.Fatalf("restricted brief draft leaked into read-member list: %+v", item)
		}
	}
	researchStatus(t, s.get(t, base+"/brief/drafts/"+draft.ID, memberAuth), http.StatusNotFound)
	researchStatus(t, s.get(t, base+"/brief/drafts/"+draft.ID, ownerAuth), http.StatusOK)

	var memberSnapshots struct {
		Items []struct {
			ID string `json:"snapshot_id"`
		} `json:"items"`
	}
	snapshotListResponse := s.get(t, base+"/brief/snapshots", memberAuth)
	researchStatus(t, snapshotListResponse, http.StatusOK)
	decode(t, snapshotListResponse, &memberSnapshots)
	for _, item := range memberSnapshots.Items {
		if item.ID == snapshot.ID {
			t.Fatalf("restricted snapshot leaked into read-member list: %+v", item)
		}
	}
	researchStatus(t, s.get(t, base+"/brief/snapshots/"+snapshot.ID, memberAuth), http.StatusNotFound)
	researchStatus(t, s.get(t, base+"/brief/snapshots/"+snapshot.ID, ownerAuth), http.StatusOK)
	for _, suffix := range []string{"activity", "comments", "review", "shares"} {
		researchStatus(t, s.get(t, base+"/brief/snapshots/"+snapshot.ID+"/"+suffix, memberAuth), http.StatusNotFound)
	}
	var memberHandoffs struct {
		Items []struct {
			ID string `json:"snapshot_id"`
		} `json:"items"`
	}
	handoffListResponse := s.get(t, base+"/brief/handoffs", memberAuth)
	researchStatus(t, handoffListResponse, http.StatusOK)
	decode(t, handoffListResponse, &memberHandoffs)
	for _, item := range memberHandoffs.Items {
		if item.ID == snapshot.ID {
			t.Fatalf("restricted handoff leaked into read-member list: %+v", item)
		}
	}
	researchStatus(t, s.get(t, base+"/brief/handoffs/"+snapshot.ID, memberAuth), http.StatusNotFound)
	researchStatus(t, s.get(t, base+"/brief/handoffs/"+snapshot.ID+"/export", memberAuth), http.StatusNotFound)
	researchStatus(t, s.get(t, base+"/brief/shared/"+snapshotShare.Token, memberAuth), http.StatusNotFound)
	researchStatus(t, s.get(t, base+"/brief/shared/"+snapshotShare.Token+"/export", memberAuth), http.StatusNotFound)
	researchStatus(t, s.get(t, base+"/brief/shared/"+snapshotShare.Token, ownerAuth), http.StatusOK)
	researchStatus(t, s.get(t, base+"/brief/shared/"+snapshotShare.Token+"/export", ownerAuth), http.StatusOK)
	researchStatus(t, s.get(t, "/v1/workspaces/"+workspace.String()+"/observations/shared/"+citationShare.Token, memberAuth), http.StatusNotFound)
	researchStatus(t, s.get(t, "/v1/workspaces/"+workspace.String()+"/observations/shared/"+citationShare.Token, ownerAuth), http.StatusOK)
	clusterResponse := s.post(t, base+"/evidence/clusters", researchJSON(t, map[string]any{
		"kind": "claim", "title": "Restricted evidence cluster", "description": "Must follow source visibility.",
		"observation_ids": []string{manual.ID.String()},
	}), ownerAuth)
	researchStatus(t, clusterResponse, http.StatusCreated)
	var cluster struct {
		ClusterID string `json:"cluster_id"`
	}
	decode(t, clusterResponse, &cluster)
	var memberClusters struct {
		Items []struct {
			ClusterID string `json:"cluster_id"`
		} `json:"items"`
	}
	memberClusterList := s.get(t, base+"/evidence/clusters", memberAuth)
	researchStatus(t, memberClusterList, http.StatusOK)
	decode(t, memberClusterList, &memberClusters)
	for _, item := range memberClusters.Items {
		if item.ClusterID == cluster.ClusterID {
			t.Fatalf("restricted evidence cluster leaked into read-member list: %+v", item)
		}
	}
	researchStatus(t, s.get(t, base+"/evidence/clusters/"+cluster.ClusterID, memberAuth), http.StatusNotFound)
	var memberCoverage struct {
		Items []struct {
			ClusterID string `json:"cluster_id"`
		} `json:"items"`
	}
	memberCoverageResponse := s.get(t, base+"/evidence/clusters/coverage", memberAuth)
	researchStatus(t, memberCoverageResponse, http.StatusOK)
	decode(t, memberCoverageResponse, &memberCoverage)
	for _, item := range memberCoverage.Items {
		if item.ClusterID == cluster.ClusterID {
			t.Fatalf("restricted evidence cluster leaked into read-member coverage: %+v", item)
		}
	}

	var memberRecords struct {
		Items []researchRecordResponse `json:"items"`
	}
	memberRecordList := s.get(t, base+"/records", memberAuth)
	researchStatus(t, memberRecordList, http.StatusOK)
	decode(t, memberRecordList, &memberRecords)
	if len(memberRecords.Items) != 0 {
		t.Fatalf("restricted source record leaked into read-member list: %+v", memberRecords.Items)
	}
	researchStatus(t, s.get(t, base+"/records/"+record.RecordID, memberAuth), http.StatusNotFound)

	var memberSummary researchRecordSummaryResponse
	memberSummaryResponse := s.get(t, base+"/records/summary", memberAuth)
	researchStatus(t, memberSummaryResponse, http.StatusOK)
	decode(t, memberSummaryResponse, &memberSummary)
	if memberSummary.RecordCount != 0 || memberSummary.CitationCount != 0 {
		t.Fatalf("restricted source record leaked into read-member summary: %+v", memberSummary)
	}
	var ownerRecords struct {
		Items []researchRecordResponse `json:"items"`
	}
	ownerRecordList := s.get(t, base+"/records?q=Restricted+source+account", ownerAuth)
	researchStatus(t, ownerRecordList, http.StatusOK)
	decode(t, ownerRecordList, &ownerRecords)
	if len(ownerRecords.Items) != 1 || ownerRecords.Items[0].RecordID != record.RecordID {
		t.Fatalf("owner could not read restricted-source record: %+v", ownerRecords.Items)
	}

	visibleResponse := s.post(t, base+"/records", researchJSON(t, map[string]any{
		"kind": "person", "name": "Visible connection endpoint", "description": "An uncited endpoint remains visible.",
	}), ownerAuth)
	researchStatus(t, visibleResponse, http.StatusCreated)
	var visibleRecord researchRecordResponse
	decode(t, visibleResponse, &visibleRecord)
	connectionResponse := s.post(t, base+"/connections", researchJSON(t, map[string]any{
		"from_record_id": record.RecordID, "to_record_id": visibleRecord.RecordID,
		"kind": "associated_with", "state": "proposed", "rationale": "Restricted endpoint must remain private.",
	}), ownerAuth)
	researchStatus(t, connectionResponse, http.StatusCreated)
	var connection researchConnectionResponse
	decode(t, connectionResponse, &connection)
	historyResponse := s.get(t, base+"/connections/"+connection.ConnectionID+"/revisions", ownerAuth)
	researchStatus(t, historyResponse, http.StatusOK)
	var history struct {
		Items []researchConnectionRevisionResponse `json:"items"`
	}
	decode(t, historyResponse, &history)
	if len(history.Items) != 1 {
		t.Fatalf("owner connection history: %+v", history.Items)
	}
	reviewResponse := s.post(t, base+"/connections/"+connection.ConnectionID+"/reviews", "{}", ownerAuth)
	researchStatus(t, reviewResponse, http.StatusCreated)
	var connectionReview researchConnectionReviewResponse
	decode(t, reviewResponse, &connectionReview)

	var memberConnections struct {
		Items []researchConnectionResponse `json:"items"`
	}
	memberConnectionList := s.get(t, base+"/connections", memberAuth)
	researchStatus(t, memberConnectionList, http.StatusOK)
	decode(t, memberConnectionList, &memberConnections)
	for _, item := range memberConnections.Items {
		if item.ConnectionID == connection.ConnectionID {
			t.Fatalf("restricted-source connection leaked into read-member list: %+v", item)
		}
	}
	researchStatus(t, s.get(t, base+"/connections/"+connection.ConnectionID, memberAuth), http.StatusNotFound)
	researchStatus(t, s.get(t, base+"/connections/"+connection.ConnectionID+"/revisions", memberAuth), http.StatusNotFound)
	researchStatus(t, s.get(t, base+"/connections/"+connection.ConnectionID+"/revisions/"+history.Items[0].RevisionID, memberAuth), http.StatusNotFound)
	researchStatus(t, s.get(t, base+"/connections/"+connection.ConnectionID+"/reviews", memberAuth), http.StatusNotFound)
	researchStatus(t, s.get(t, base+"/connections/"+connection.ConnectionID+"/reviews/"+connectionReview.ConnectionReviewID, memberAuth), http.StatusNotFound)
	researchStatus(t, s.get(t, base+"/connections/"+connection.ConnectionID, ownerAuth), http.StatusOK)
}

// The members screen is the join /v1/me already is: org owns the seat, identity
// owns the name, and the root is the only place allowed to know both.
func TestTheMemberListNamesPeopleOrgCannotSee(t *testing.T) {
	s := tracedSystem(t)

	_, ownerAuth := s.signUp(t, "sam@example.com")
	seen := s.me(t, ownerAuth)
	org, err := id.Parse(seen.Orgs[0].OrgID)
	if err != nil {
		t.Fatal(err)
	}
	kit, _ := s.signUp(t, "kit@example.com")
	s.seat(t, org, kit, orgdomain.RoleMember)

	var members []memberResponse
	decode(t, s.get(t, "/v1/orgs/"+org.String()+"/members", ownerAuth), &members)
	if len(members) != 2 {
		t.Fatalf("%d members: %+v", len(members), members)
	}
	if members[0].Role != "owner" || members[0].Email != "sam@example.com" {
		t.Errorf("first seat: %+v", members[0])
	}
	if members[1].Role != "member" || members[1].Email != "kit@example.com" {
		t.Errorf("second seat: %+v", members[1])
	}
	if members[1].JoinedAt == "" {
		t.Error("a seat with no joined_at cannot be sorted or explained")
	}
}

// The other half of decisions/0020: an admin is not exempt, so an engagement
// they open is invisible to them until the grant subscriber runs. If the fourth
// link is ever dropped from the dispatcher this is the test that notices.
func TestWhoeverOpensAnEngagementEndsUpAdminOnIt(t *testing.T) {
	s := tracedSystem(t)

	_, ownerAuth := s.signUp(t, "sam@example.com")
	seen := s.me(t, ownerAuth)
	org, err := id.Parse(seen.Orgs[0].OrgID)
	if err != nil {
		t.Fatal(err)
	}
	// Verified, because opening an engagement is gated on it — decisions/0018.
	verify(t, s, ownerAuth)

	kit, kitAuth := s.signUp(t, "kit@example.com")
	s.seat(t, org, kit, orgdomain.RoleAdmin)
	verify(t, s, kitAuth)

	// An admin may start one. A member may not.
	res := s.post(t, "/v1/orgs/"+org.String()+"/workspaces", `{"name":"Acme Q3"}`, kitAuth)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("an admin could not open an engagement: %d", res.StatusCode)
	}
	var opened workspaceResponse
	decode(t, res, &opened)
	if opened.Name != "Acme Q3" {
		t.Errorf("opened %q", opened.Name)
	}

	// Before the subscriber runs the admin holds no grant, and the workspace is
	// therefore invisible to them. That window is decisions/0020's stated cost.
	if found := workspaceIn(s.me(t, kitAuth), org.String(), opened.WorkspaceID); found != nil {
		t.Error("the admin could see it before the grant was written")
	}

	s.drain(t)

	found := workspaceIn(s.me(t, kitAuth), org.String(), opened.WorkspaceID)
	if found == nil {
		t.Fatal("the admin cannot see the engagement they opened")
	}
	if found.Access != "admin" {
		t.Errorf("the opener has %q on what they opened", found.Access)
	}

	// created_by is a RECORD and not a permission — it is never consulted by
	// the gate. What makes the opener admin is the grant the subscriber wrote.
	var createdBy string
	ctx := context.Background()
	if err := s.pool.DB(ctx).QueryRow(ctx,
		`select coalesce(created_by::text,'') from workspace.workspace where id = $1`,
		opened.WorkspaceID).Scan(&createdBy); err != nil {
		t.Fatal(err)
	}
	if createdBy != kit.String() {
		t.Errorf("created_by is %q, not the opener", createdBy)
	}

	// One act, two records — decisions/0014 and 0020. The work is in the
	// journal; the choice is in audit.
	if n := s.counted(t, `select count(*) from journal.line where action = $1`,
		"workspace.opened"); n != 1 {
		t.Errorf("journal lines for workspace.opened: %d", n)
	}
	if n := s.counted(t, `select count(*) from audit.entry where action = $1`,
		"workspace.opened"); n != 1 {
		t.Errorf("audit entries for workspace.opened: %d — the decision was not recorded", n)
	}
	// And provisioning stays out of audit, exactly as before.
	if n := s.counted(t, `select count(*) from audit.entry where action = $1`,
		"workspace.created"); n != 0 {
		t.Errorf("workspace.created reached audit: %d rows", n)
	}

	// A plain member may not start one, and is told why rather than being shown
	// a 404 — they already know the org exists.
	pat, patAuth := s.signUp(t, "pat@example.com")
	s.seat(t, org, pat, orgdomain.RoleMember)
	verify(t, s, patAuth)
	res = s.post(t, "/v1/orgs/"+org.String()+"/workspaces", `{"name":"CTF1"}`, patAuth)
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("a member opening an engagement: %d, want 403", res.StatusCode)
	}
	_ = pat
}

// verify walks the account behind an authorisation header through its
// verification link, because opening an engagement is gated on a proven address.
func verify(t *testing.T, s traced, auth map[string]string) {
	t.Helper()
	var who meResponse
	decode(t, s.get(t, "/v1/me", auth), &who)
	if res := s.post(t, "/v1/verifications", `{"email":`+quote(who.Email)+`}`, nil); res.StatusCode != http.StatusAccepted {
		t.Fatalf("request verification: %d", res.StatusCode)
	}
	token := s.token(t, "/verify")
	if res := s.post(t, "/v1/verifications/confirm", `{"token":`+quote(token)+`}`, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("confirm: %d", res.StatusCode)
	}
}

func workspaceIn(seen meResponse, org, workspace string) *meWorkspace {
	for i := range seen.Orgs {
		if seen.Orgs[i].OrgID != org {
			continue
		}
		for j := range seen.Orgs[i].Workspaces {
			if seen.Orgs[i].Workspaces[j].WorkspaceID == workspace {
				return &seen.Orgs[i].Workspaces[j]
			}
		}
	}
	return nil
}
