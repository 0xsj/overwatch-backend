package root

import (
	"net/http"
	"strings"
	"testing"

	briefdomain "github.com/0xsj/overwatch-backend/internal/brief/domain"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	reviewdomain "github.com/0xsj/overwatch-backend/internal/review/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestResearchWorkingBriefPersistsCitationsAndOpenQuestions(t *testing.T) {
	s := tracedSystem(t)
	org, workspace, owner, reviewer, auth, reviewerAuth := firm(t, s, orgdomain.RoleMember)
	s.grant(t, org, reviewer, workspace, orgdomain.LevelWrite)
	source := addResearchSource(t, s, workspace, auth, "The notice places the disruption at East Quay.")
	observation := recordResearchObservation(t, s, "/v1/workspaces/"+workspace.String()+"/sources/"+source.ID.String(), auth, source.LatestCapture.ID, "disruption at East Quay")
	base := "/v1/workspaces/" + workspace.String()
	clusterResponse := s.post(t, base+"/evidence/clusters", researchJSON(t, map[string]any{
		"kind": "claim", "title": "East Quay disruption grouping", "description": "A qualified grouping for the authored handoff.", "observation_ids": []id.ID{observation.ID},
	}), auth)
	researchStatus(t, clusterResponse, http.StatusCreated)
	var cluster reviewdomain.Cluster
	decode(t, clusterResponse, &cluster)
	questionPath := "/v1/workspaces/" + workspace.String() + "/questions"
	res := s.post(t, questionPath, researchJSON(t, map[string]any{
		"question": "Was the disruption reported independently?", "state": "open", "observation_ids": []id.ID{observation.ID},
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var question struct {
		ID id.ID `json:"question_id"`
	}
	decode(t, res, &question)
	fromResponse := s.post(t, base+"/records", researchJSON(t, map[string]any{
		"kind": "account", "name": "@eastquay", "description": "A working account record for the East Quay notice.", "observation_ids": []string{observation.ID.String()},
	}), auth)
	researchStatus(t, fromResponse, http.StatusCreated)
	var from researchRecordResponse
	decode(t, fromResponse, &from)
	toResponse := s.post(t, base+"/records", researchJSON(t, map[string]any{
		"kind": "place", "name": "East Quay", "description": "The place named in the retained notice.", "observation_ids": []string{observation.ID.String()},
	}), auth)
	researchStatus(t, toResponse, http.StatusCreated)
	var to researchRecordResponse
	decode(t, toResponse, &to)
	connectionResponse := s.post(t, base+"/connections", researchJSON(t, map[string]any{
		"from_record_id": from.RecordID, "to_record_id": to.RecordID, "kind": "located_at", "state": "proposed",
		"rationale":                  "The retained notice places the account at East Quay.",
		"supporting_observation_ids": []string{observation.ID.String()}, "opposing_observation_ids": []string{},
	}), auth)
	researchStatus(t, connectionResponse, http.StatusCreated)
	var connection researchConnectionResponse
	decode(t, connectionResponse, &connection)
	eventResponse := s.post(t, base+"/events", researchJSON(t, map[string]any{
		"title": "East Quay disruption", "description": "Several reports may describe the same disruption.", "reported_time": "around 18:00", "time_precision": "approximate", "sort_date": "2026-09-17", "location": "East Quay",
		"observation_ids": []id.ID{observation.ID}, "participant_record_ids": []string{from.RecordID}, "location_record_id": to.RecordID,
	}), auth)
	researchStatus(t, eventResponse, http.StatusCreated)
	var event struct {
		ID id.ID `json:"event_id"`
	}
	decode(t, eventResponse, &event)

	briefPath := "/v1/workspaces/" + workspace.String() + "/brief"
	res = s.put(t, briefPath, researchJSON(t, map[string]any{
		"title": "East Quay handoff", "question": "What happened at East Quay?",
		"current_account": "The notice describes a disruption at East Quay.",
		"alternatives":    "The wording may refer to a different occurrence.",
		"limitations":     "Only one retained notice is linked so far.", "next_steps": "Find an independent account.",
		"observation_ids": []id.ID{observation.ID}, "cluster_ids": []id.ID{cluster.ID}, "question_ids": []id.ID{question.ID}, "connection_ids": []string{connection.ConnectionID}, "event_ids": []string{event.ID.String()},
	}), auth)
	researchStatus(t, res, http.StatusOK)
	var created briefdomain.Brief
	decode(t, res, &created)
	if created.Author != owner || created.UpdatedBy != owner || created.Title != "East Quay handoff" || len(created.ObservationIDs) != 1 || len(created.ClusterIDs) != 1 || created.ClusterIDs[0] != cluster.ID || len(created.QuestionIDs) != 1 || len(created.ConnectionIDs) != 1 || len(created.EventIDs) != 1 || created.EventIDs[0] != event.ID {
		t.Fatalf("unexpected working brief: %+v", created)
	}

	res = s.put(t, briefPath, researchJSON(t, map[string]any{
		"title": "East Quay handoff", "question": "What happened at East Quay?",
		"current_account": "The account remains provisional.", "next_steps": "Find an independent account.",
		"observation_ids": []id.ID{observation.ID}, "cluster_ids": []id.ID{cluster.ID}, "question_ids": []id.ID{question.ID}, "connection_ids": []string{connection.ConnectionID}, "event_ids": []string{event.ID.String()},
	}), auth)
	researchStatus(t, res, http.StatusOK)
	var updated briefdomain.Brief
	decode(t, res, &updated)
	if updated.ID != created.ID || updated.Author != owner || updated.CurrentAccount != "The account remains provisional." || updated.UpdatedBy != owner || len(updated.ClusterIDs) != 1 || updated.ClusterIDs[0] != cluster.ID || len(updated.ConnectionIDs) != 1 {
		t.Fatalf("brief singleton did not update in place: %+v", updated)
	}

	res = s.get(t, briefPath, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &updated)
	if updated.ID != created.ID || len(updated.ObservationIDs) != 1 || len(updated.ClusterIDs) != 1 || updated.ClusterIDs[0] != cluster.ID || len(updated.QuestionIDs) != 1 || len(updated.ConnectionIDs) != 1 || len(updated.EventIDs) != 1 {
		t.Fatalf("brief links were not retained: %+v", updated)
	}

	snapshotsPath := briefPath + "/snapshots"
	res = s.post(t, snapshotsPath, `{}`, auth)
	researchStatus(t, res, http.StatusCreated)
	var frozen briefdomain.Snapshot
	decode(t, res, &frozen)
	if frozen.BriefID != created.ID || frozen.FrozenBy != owner || len(frozen.Clusters) != 1 || frozen.Clusters[0].ID != cluster.ID || frozen.Clusters[0].Title != "East Quay disruption grouping" || len(frozen.Clusters[0].ObservationIDs) != 1 || len(frozen.Questions) != 1 || frozen.Questions[0].Prompt != "Was the disruption reported independently?" || len(frozen.Questions[0].ObservationIDs) != 1 || frozen.Questions[0].ObservationIDs[0] != observation.ID || len(frozen.Connections) != 1 || frozen.Connections[0].State != "proposed" || frozen.Connections[0].ToRecordName != "East Quay" || frozen.Connections[0].FromRecordDescription != "A working account record for the East Quay notice." || len(frozen.Connections[0].FromRecordObservationIDs) != 1 || frozen.Connections[0].ToRecordDescription != "The place named in the retained notice." || len(frozen.Connections[0].ToRecordObservationIDs) != 1 || len(frozen.Events) != 1 || frozen.Events[0].Title != "East Quay disruption" || frozen.Events[0].RevisionID.IsZero() || frozen.Events[0].Revision != 1 || len(frozen.Events[0].ParticipantRecords) != 1 || frozen.Events[0].ParticipantRecords[0].Name != "@eastquay" || frozen.Events[0].LocationRecord == nil || frozen.Events[0].LocationRecord.Name != "East Quay" || len(frozen.Events[0].ObservationIDs) != 1 {
		t.Fatalf("unexpected frozen brief: %+v", frozen)
	}
	reviewPath := snapshotsPath + "/" + frozen.ID.String() + "/review"
	res = s.get(t, reviewPath, auth)
	researchStatus(t, res, http.StatusOK)
	var handoffReview briefdomain.SnapshotReview
	decode(t, res, &handoffReview)
	if handoffReview.State != briefdomain.ReviewPending || handoffReview.AssigneeID != nil || len(handoffReview.Decisions) != 0 {
		t.Fatalf("new handoff review: %+v", handoffReview)
	}
	res = s.put(t, reviewPath+"/assignment", researchJSON(t, map[string]any{"assignee_id": reviewer}), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &handoffReview)
	if handoffReview.AssigneeID == nil || *handoffReview.AssigneeID != reviewer {
		t.Fatalf("reviewer assignment: %+v", handoffReview)
	}
	res = s.post(t, reviewPath+"/decisions", researchJSON(t, map[string]any{"state": "changes_requested", "note": "Add an independent account before delivery."}), reviewerAuth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &handoffReview)
	if handoffReview.State != briefdomain.ReviewChangesRequested || len(handoffReview.Decisions) != 1 || handoffReview.Decisions[0].ReviewerID != reviewer {
		t.Fatalf("changes requested review: %+v", handoffReview)
	}
	res = s.post(t, reviewPath+"/decisions", researchJSON(t, map[string]any{"state": "approved", "note": "Independent account added."}), auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &handoffReview)
	if handoffReview.State != briefdomain.ReviewApproved || len(handoffReview.Decisions) != 2 {
		t.Fatalf("approved review history: %+v", handoffReview)
	}
	if res = s.post(t, reviewPath+"/decisions", researchJSON(t, map[string]any{"state": "changes_requested"}), auth); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("changes without a note: %d", res.StatusCode)
	}

	commentsPath := snapshotsPath + "/" + frozen.ID.String() + "/comments"
	res = s.get(t, commentsPath, auth)
	researchStatus(t, res, http.StatusOK)
	var comments []briefdomain.SnapshotComment
	decode(t, res, &comments)
	if len(comments) != 0 {
		t.Fatalf("new handoff comments: %+v", comments)
	}
	res = s.post(t, commentsPath, researchJSON(t, map[string]any{"body": "  Add the independent account to the delivery note.  "}), auth)
	researchStatus(t, res, http.StatusCreated)
	var ownerComment briefdomain.SnapshotComment
	decode(t, res, &ownerComment)
	if ownerComment.AuthorID != owner || ownerComment.Body != "Add the independent account to the delivery note." || ownerComment.SnapshotID != frozen.ID {
		t.Fatalf("owner handoff comment: %+v", ownerComment)
	}
	res = s.post(t, commentsPath, researchJSON(t, map[string]any{"body": "The independent account is now linked."}), reviewerAuth)
	researchStatus(t, res, http.StatusCreated)
	res = s.get(t, commentsPath, reviewerAuth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &comments)
	if len(comments) != 2 || comments[0].AuthorID != reviewer || comments[0].Body != "The independent account is now linked." || comments[1].AuthorID != owner || comments[1].Body != ownerComment.Body {
		t.Fatalf("handoff comment history: %+v", comments)
	}
	if res = s.post(t, commentsPath, researchJSON(t, map[string]any{"body": "   "}), auth); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty handoff comment: %d", res.StatusCode)
	}

	client, clientAuth := s.signUp(t, "client@example.com")
	s.seat(t, org, client, orgdomain.RoleClient)
	s.grant(t, org, client, workspace, orgdomain.LevelRead)
	handoffPath := base + "/brief/handoffs"
	res = s.get(t, handoffPath, clientAuth)
	researchStatus(t, res, http.StatusOK)
	var recipientPage briefRecipientHandoffPageResponse
	decode(t, res, &recipientPage)
	if len(recipientPage.Items) != 1 || recipientPage.Items[0].SnapshotID != frozen.ID.String() || recipientPage.Items[0].Visibility != "recipient" || len(recipientPage.Items[0].Redactions) != 4 {
		t.Fatalf("recipient handoff list: %+v", recipientPage)
	}
	if recipientPage.Items[0].Question != frozen.Question || len(recipientPage.Items[0].Connections) != 1 || recipientPage.Items[0].Connections[0].FromName != "@eastquay" || len(recipientPage.Items[0].Events) != 1 {
		t.Fatalf("recipient handoff content: %+v", recipientPage.Items[0])
	}
	res = s.get(t, handoffPath+"/"+frozen.ID.String(), clientAuth)
	researchStatus(t, res, http.StatusOK)
	var recipient briefRecipientHandoffResponse
	decode(t, res, &recipient)
	if recipient.CurrentAccount != "The account remains provisional." || len(recipient.Questions) != 1 || recipient.Questions[0].Question != "Was the disruption reported independently?" || recipient.Questions[0].State != "open" {
		t.Fatalf("recipient handoff detail: %+v", recipient)
	}
	if res = s.get(t, base+"/sources", clientAuth); res.StatusCode != http.StatusNotFound {
		t.Fatalf("client reached the underlying research source while testing export safety: %d", res.StatusCode)
	}
	exportPath := handoffPath + "/" + frozen.ID.String() + "/export"
	res = s.get(t, exportPath, clientAuth)
	researchStatus(t, res, http.StatusOK)
	var recipientExport briefHandoffExportResponse
	decode(t, res, &recipientExport)
	if recipientExport.ContentType != "text/markdown" || recipientExport.Filename != "recipient-handoff.md" || !strings.Contains(recipientExport.Content, "# East Quay handoff") || !strings.Contains(recipientExport.Content, "The account remains provisional.") {
		t.Fatalf("recipient handoff export: %+v", recipientExport)
	}
	for _, forbidden := range []string{observation.ID.String(), frozen.ID.String(), "observation_id", "source_id", "capture_id", "source_and_capture_details", "review_comments"} {
		if strings.Contains(recipientExport.Content, forbidden) {
			t.Fatalf("recipient handoff export disclosed %q: %s", forbidden, recipientExport.Content)
		}
	}
	sharesPath := snapshotsPath + "/" + frozen.ID.String() + "/shares"
	res = s.post(t, sharesPath, `{}`, auth)
	researchStatus(t, res, http.StatusCreated)
	var share briefHandoffShareResponse
	decode(t, res, &share)
	if share.ShareID == "" || share.SnapshotID != frozen.ID.String() || share.Token == "" {
		t.Fatalf("created handoff share: %+v", share)
	}
	res = s.get(t, sharesPath, auth)
	researchStatus(t, res, http.StatusOK)
	var shares []briefHandoffShareResponse
	decode(t, res, &shares)
	if len(shares) != 1 || shares[0].ShareID != share.ShareID || shares[0].Token != "" {
		t.Fatalf("handoff share management list leaked or lost state: %+v", shares)
	}
	if res = s.get(t, sharesPath, clientAuth); res.StatusCode != http.StatusNotFound {
		t.Fatalf("client reached handoff share management: %d", res.StatusCode)
	}
	sharedPath := base + "/brief/shared/" + share.Token
	res = s.get(t, sharedPath, clientAuth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &recipient)
	if recipient.SnapshotID != frozen.ID.String() || recipient.Visibility != "recipient" {
		t.Fatalf("shared recipient handoff: %+v", recipient)
	}
	res = s.get(t, sharedPath+"/export", clientAuth)
	researchStatus(t, res, http.StatusOK)
	var sharedExport briefHandoffExportResponse
	decode(t, res, &sharedExport)
	if sharedExport.Content != recipientExport.Content {
		t.Fatalf("shared export differed from direct recipient export")
	}
	res = s.post(t, base+"/brief/shares/"+share.ShareID+"/revoke", `{}`, auth)
	researchStatus(t, res, http.StatusOK)
	if res = s.get(t, sharedPath, clientAuth); res.StatusCode != http.StatusNotFound {
		t.Fatalf("revoked handoff share still opened: %d", res.StatusCode)
	}
	if res = s.get(t, sharedPath+"/export", clientAuth); res.StatusCode != http.StatusNotFound {
		t.Fatalf("revoked handoff share still exported: %d", res.StatusCode)
	}
	if res = s.get(t, base+"/brief/shared/not-a-token", clientAuth); res.StatusCode != http.StatusNotFound {
		t.Fatalf("invalid handoff share token: %d", res.StatusCode)
	}
	s.drain(t)
	var activity pageRow
	res = s.get(t, snapshotsPath+"/"+frozen.ID.String()+"/activity", auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &activity)
	seenActivity := map[string]int{}
	for _, entry := range activity.Entries {
		seenActivity[entry.Action]++
		if entry.WorkspaceID != workspace.String() {
			t.Errorf("handoff activity escaped its workspace: %+v", entry)
		}
	}
	for action, want := range map[string]int{
		briefdomain.EventSnapshotCreated:         1,
		briefdomain.EventSnapshotReviewAssigned:  1,
		briefdomain.EventSnapshotReviewDecided:   2,
		briefdomain.EventSnapshotCommentCreated:  2,
		briefdomain.EventSnapshotShareCreated:    1,
		briefdomain.EventSnapshotShareRevoked:    1,
		briefdomain.EventSnapshotHandoffAccessed: 2,
		briefdomain.EventSnapshotHandoffExported: 2,
	} {
		if seenActivity[action] != want {
			t.Errorf("handoff activity %s: got %d, want %d", action, seenActivity[action], want)
		}
	}
	if res = s.get(t, snapshotsPath+"/"+frozen.ID.String()+"/activity", clientAuth); res.StatusCode != http.StatusNotFound {
		t.Fatalf("client reached internal handoff activity: %d", res.StatusCode)
	}
	var raw map[string]any
	decode(t, s.get(t, handoffPath+"/"+frozen.ID.String(), clientAuth), &raw)
	for _, forbidden := range []string{"observation_ids", "from_record_id", "to_record_id", "review_comments"} {
		if _, found := raw[forbidden]; found {
			t.Fatalf("recipient handoff disclosed %q: %+v", forbidden, raw)
		}
	}

	researchStatus(t, s.put(t, base+"/records/"+from.RecordID, researchJSON(t, map[string]any{
		"kind": "account", "name": "@eastquay", "description": "The live account context was later refined.", "observation_ids": []string{},
	}), auth), http.StatusOK)
	researchStatus(t, s.put(t, base+"/records/"+to.RecordID, researchJSON(t, map[string]any{
		"kind": "place", "name": "East Quay", "description": "The live place context was later refined.", "observation_ids": []string{},
	}), auth), http.StatusOK)
	researchStatus(t, s.put(t, base+"/events/"+event.ID.String(), researchJSON(t, map[string]any{
		"title": "East Quay disruption revised", "description": "The event wording was refined after another review.", "reported_time": "around 19:00", "time_precision": "approximate", "sort_date": "2026-09-17", "location": "East Quay",
		"observation_ids": []id.ID{observation.ID}, "participant_record_ids": []string{from.RecordID}, "location_record_id": to.RecordID,
	}), auth), http.StatusOK)

	res = s.put(t, base+"/connections/"+connection.ConnectionID, researchJSON(t, map[string]any{
		"kind": "located_at", "state": "accepted", "rationale": "The retained notice is now treated as sufficient for the provisional assessment.",
		"supporting_observation_ids": []string{observation.ID.String()}, "opposing_observation_ids": []string{},
	}), auth)
	researchStatus(t, res, http.StatusOK)

	res = s.put(t, questionPath+"/"+question.ID.String(), researchJSON(t, map[string]any{
		"question": "Was the disruption independently confirmed?", "state": "answered", "resolution": "Not yet.", "observation_ids": []id.ID{},
	}), auth)
	researchStatus(t, res, http.StatusOK)
	res = s.get(t, snapshotsPath+"/"+frozen.ID.String(), auth)
	researchStatus(t, res, http.StatusOK)
	var reread briefdomain.Snapshot
	decode(t, res, &reread)
	if len(reread.Questions) != 1 || reread.Questions[0].Prompt != "Was the disruption reported independently?" || reread.Questions[0].State != "open" || len(reread.Questions[0].ObservationIDs) != 1 || reread.Questions[0].ObservationIDs[0] != observation.ID || len(reread.Connections) != 1 || reread.Connections[0].State != "proposed" || reread.Connections[0].FromRecordDescription != "A working account record for the East Quay notice." || len(reread.Connections[0].FromRecordObservationIDs) != 1 || reread.Connections[0].ToRecordDescription != "The place named in the retained notice." || len(reread.Connections[0].ToRecordObservationIDs) != 1 || len(reread.Events) != 1 || reread.Events[0].Title != "East Quay disruption" || reread.Events[0].ReportedTime != "around 18:00" || reread.Events[0].RevisionID != frozen.Events[0].RevisionID || reread.Events[0].Revision != frozen.Events[0].Revision {
		t.Fatalf("frozen question changed with its source question: %+v", reread.Questions)
	}
	res = s.put(t, questionPath+"/"+question.ID.String(), researchJSON(t, map[string]any{
		"question": "Was the disruption independently confirmed?", "state": "deferred", "resolution": "Set aside pending an independent account.", "observation_ids": []id.ID{},
	}), auth)
	researchStatus(t, res, http.StatusOK)
	res = s.post(t, snapshotsPath, `{}`, auth)
	researchStatus(t, res, http.StatusCreated)
	var deferredFrozen briefdomain.Snapshot
	decode(t, res, &deferredFrozen)
	if len(deferredFrozen.Questions) != 1 || deferredFrozen.Questions[0].State != "deferred" || deferredFrozen.Questions[0].Resolution != "Set aside pending an independent account." {
		t.Fatalf("deferred question was not preserved in the new frozen handoff: %+v", deferredFrozen.Questions)
	}
	var page struct {
		Items []briefdomain.Snapshot `json:"items"`
	}
	res = s.get(t, snapshotsPath, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &page)
	if len(page.Items) != 2 {
		t.Fatalf("snapshot list: %+v", page)
	}

	res = s.put(t, briefPath, researchJSON(t, map[string]any{
		"title": "Bad handoff", "question": "Cross workspace", "observation_ids": []id.ID{{9}},
	}), auth)
	researchStatus(t, res, http.StatusNotFound)
}
