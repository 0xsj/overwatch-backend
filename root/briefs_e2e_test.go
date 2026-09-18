package root

import (
	"net/http"
	"testing"

	briefdomain "github.com/0xsj/overwatch-backend/internal/brief/domain"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestResearchWorkingBriefPersistsCitationsAndOpenQuestions(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, owner, _, auth, _ := firm(t, s, orgdomain.RoleMember)
	source := addResearchSource(t, s, workspace, auth, "The notice places the disruption at East Quay.")
	observation := recordResearchObservation(t, s, "/v1/workspaces/"+workspace.String()+"/sources/"+source.ID.String(), auth, source.LatestCapture.ID, "disruption at East Quay")
	questionPath := "/v1/workspaces/" + workspace.String() + "/questions"
	res := s.post(t, questionPath, researchJSON(t, map[string]any{
		"question": "Was the disruption reported independently?", "state": "open", "observation_ids": []id.ID{observation.ID},
	}), auth)
	researchStatus(t, res, http.StatusCreated)
	var question struct {
		ID id.ID `json:"question_id"`
	}
	decode(t, res, &question)
	base := "/v1/workspaces/" + workspace.String()
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

	briefPath := "/v1/workspaces/" + workspace.String() + "/brief"
	res = s.put(t, briefPath, researchJSON(t, map[string]any{
		"title": "East Quay handoff", "question": "What happened at East Quay?",
		"current_account": "The notice describes a disruption at East Quay.",
		"alternatives":    "The wording may refer to a different occurrence.",
		"limitations":     "Only one retained notice is linked so far.", "next_steps": "Find an independent account.",
		"observation_ids": []id.ID{observation.ID}, "question_ids": []id.ID{question.ID}, "connection_ids": []string{connection.ConnectionID},
	}), auth)
	researchStatus(t, res, http.StatusOK)
	var created briefdomain.Brief
	decode(t, res, &created)
	if created.Author != owner || created.UpdatedBy != owner || created.Title != "East Quay handoff" || len(created.ObservationIDs) != 1 || len(created.QuestionIDs) != 1 || len(created.ConnectionIDs) != 1 {
		t.Fatalf("unexpected working brief: %+v", created)
	}

	res = s.put(t, briefPath, researchJSON(t, map[string]any{
		"title": "East Quay handoff", "question": "What happened at East Quay?",
		"current_account": "The account remains provisional.", "next_steps": "Find an independent account.",
		"observation_ids": []id.ID{observation.ID}, "question_ids": []id.ID{question.ID}, "connection_ids": []string{connection.ConnectionID},
	}), auth)
	researchStatus(t, res, http.StatusOK)
	var updated briefdomain.Brief
	decode(t, res, &updated)
	if updated.ID != created.ID || updated.Author != owner || updated.CurrentAccount != "The account remains provisional." || updated.UpdatedBy != owner || len(updated.ConnectionIDs) != 1 {
		t.Fatalf("brief singleton did not update in place: %+v", updated)
	}

	res = s.get(t, briefPath, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &updated)
	if updated.ID != created.ID || len(updated.ObservationIDs) != 1 || len(updated.QuestionIDs) != 1 || len(updated.ConnectionIDs) != 1 {
		t.Fatalf("brief links were not retained: %+v", updated)
	}

	snapshotsPath := briefPath + "/snapshots"
	res = s.post(t, snapshotsPath, `{}`, auth)
	researchStatus(t, res, http.StatusCreated)
	var frozen briefdomain.Snapshot
	decode(t, res, &frozen)
	if frozen.BriefID != created.ID || frozen.FrozenBy != owner || len(frozen.Questions) != 1 || frozen.Questions[0].Prompt != "Was the disruption reported independently?" || len(frozen.Questions[0].ObservationIDs) != 1 || frozen.Questions[0].ObservationIDs[0] != observation.ID || len(frozen.Connections) != 1 || frozen.Connections[0].State != "proposed" || frozen.Connections[0].ToRecordName != "East Quay" || frozen.Connections[0].FromRecordDescription != "A working account record for the East Quay notice." || len(frozen.Connections[0].FromRecordObservationIDs) != 1 || frozen.Connections[0].ToRecordDescription != "The place named in the retained notice." || len(frozen.Connections[0].ToRecordObservationIDs) != 1 {
		t.Fatalf("unexpected frozen brief: %+v", frozen)
	}

	researchStatus(t, s.put(t, base+"/records/"+from.RecordID, researchJSON(t, map[string]any{
		"kind": "account", "name": "@eastquay", "description": "The live account context was later refined.", "observation_ids": []string{},
	}), auth), http.StatusOK)
	researchStatus(t, s.put(t, base+"/records/"+to.RecordID, researchJSON(t, map[string]any{
		"kind": "place", "name": "East Quay", "description": "The live place context was later refined.", "observation_ids": []string{},
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
	if len(reread.Questions) != 1 || reread.Questions[0].Prompt != "Was the disruption reported independently?" || reread.Questions[0].State != "open" || len(reread.Questions[0].ObservationIDs) != 1 || reread.Questions[0].ObservationIDs[0] != observation.ID || len(reread.Connections) != 1 || reread.Connections[0].State != "proposed" || reread.Connections[0].FromRecordDescription != "A working account record for the East Quay notice." || len(reread.Connections[0].FromRecordObservationIDs) != 1 || reread.Connections[0].ToRecordDescription != "The place named in the retained notice." || len(reread.Connections[0].ToRecordObservationIDs) != 1 {
		t.Fatalf("frozen question changed with its source question: %+v", reread.Questions)
	}
	var page struct {
		Items []briefdomain.Snapshot `json:"items"`
	}
	res = s.get(t, snapshotsPath, auth)
	researchStatus(t, res, http.StatusOK)
	decode(t, res, &page)
	if len(page.Items) != 1 || page.Items[0].ID != frozen.ID {
		t.Fatalf("snapshot list: %+v", page)
	}

	res = s.put(t, briefPath, researchJSON(t, map[string]any{
		"title": "Bad handoff", "question": "Cross workspace", "observation_ids": []id.ID{{9}},
	}), auth)
	researchStatus(t, res, http.StatusNotFound)
}
