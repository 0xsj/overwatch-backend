// Author-written, against a live database, from decisions/0043's Verification
// block. The claims here are the ones the domain cannot make: the kind
// vocabulary is really asked, the report's eighth section really carries the
// subjectless notes, and a frozen report really keeps what a note said then.
package root

import (
	"encoding/json"
	"net/http"
	"testing"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func writeNote(t *testing.T, s traced, ws id.ID, body string, auth map[string]string) noteResponse {
	t.Helper()
	res := s.post(t, "/v1/workspaces/"+ws.String()+"/notes", body, auth)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("write note %s: %d", body, res.StatusCode)
	}
	var out noteResponse
	decode(t, res, &out)
	return out
}

// A note about a host and the engagement summary are the SAME shape, and the
// difference is whether two fields are filled.
func TestANoteIsEitherAboutSomethingOrTheEngagementSummary(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)
	base := "/v1/workspaces/" + workspace.String() + "/notes"

	about := writeNote(t, s, workspace,
		`{"subject_kind":"host","subject_value":"A.ACME.Test","body":"staging box, ignore"}`, owner)
	if about.SubjectValue != "a.acme.test" {
		t.Fatalf("the subject value is folded: %q", about.SubjectValue)
	}
	summary := writeNote(t, s, workspace, `{"body":"the engagement started late"}`, owner)
	if summary.SubjectKind != "" {
		t.Fatalf("the summary has no subject: %+v", summary)
	}

	// The LIST is everything; the SUBJECT filter is one thing; `summary=true`
	// is the subjectless ones.
	var all, one, only []noteResponse
	decode(t, s.get(t, base, owner), &all)
	decode(t, s.get(t, base+"?subject_kind=host&subject_value=a.acme.test", owner), &one)
	decode(t, s.get(t, base+"?summary=true", owner), &only)

	if len(all) != 2 || len(one) != 1 || len(only) != 1 {
		t.Fatalf("all %d, about %d, summary %d", len(all), len(one), len(only))
	}
	if one[0].NoteID != about.NoteID || only[0].NoteID != summary.NoteID {
		t.Fatal("the two filters answered the wrong notes")
	}
	// The FILTER folds too, so a caller passing what a person typed finds what
	// a person saved.
	var typed []noteResponse
	decode(t, s.get(t, base+"?subject_kind=host&subject_value=A.ACME.TEST", owner), &typed)
	if len(typed) != 1 {
		t.Fatalf("the filter must fold: %d", len(typed))
	}
}

// The notebook's newer page contract is cursor-addressed and searches on the
// server. The old array-shaped list above remains intact for older clients.
func TestWorkingNotePageSearchesAndResumesBeyondTheFirstWindow(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)
	base := "/v1/workspaces/" + workspace.String() + "/notes"
	writeNote(t, s, workspace, `{"body":"first research thread"}`, owner)
	writeNote(t, s, workspace, `{"body":"second research thread"}`, owner)
	writeNote(t, s, workspace, `{"body":"third unique corroboration"}`, owner)

	var first struct {
		Items      []noteResponse `json:"items"`
		NextCursor *string        `json:"next_cursor"`
	}
	decode(t, s.get(t, base+"?page=true&summary=true&limit=2", owner), &first)
	if len(first.Items) != 2 || first.NextCursor == nil {
		t.Fatalf("first page must be bounded and resumable: %+v", first)
	}

	var second struct {
		Items      []noteResponse `json:"items"`
		NextCursor *string        `json:"next_cursor"`
	}
	decode(t, s.get(t, base+"?page=true&summary=true&limit=2&before="+*first.NextCursor, owner), &second)
	if len(second.Items) != 1 || second.Items[0].NoteID == first.Items[0].NoteID || second.NextCursor != nil {
		t.Fatalf("cursor must continue after the first window: %+v", second)
	}

	var searched struct {
		Items []noteResponse `json:"items"`
	}
	decode(t, s.get(t, base+"?page=true&summary=true&q=unique", owner), &searched)
	if len(searched.Items) != 1 || searched.Items[0].Body != "third unique corroboration" {
		t.Fatalf("server search must return the matching note: %+v", searched)
	}
}

func TestWorkingNoteCanPreserveResearchContext(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)
	base := "/v1/workspaces/" + workspace.String()
	questionResponse := s.post(t, base+"/questions", researchJSON(t, map[string]any{
		"question": "Which account needs corroboration?", "state": "open", "observation_ids": []id.ID{},
	}), owner)
	researchStatus(t, questionResponse, http.StatusCreated)
	var question struct {
		ID id.ID `json:"question_id"`
	}
	decode(t, questionResponse, &question)

	createdResponse := s.post(t, base+"/notes", researchJSON(t, map[string]any{
		"body": "Find a second account before closing this question.", "context_kind": "question", "context_id": question.ID.String(),
	}), owner)
	researchStatus(t, createdResponse, http.StatusCreated)
	var note noteResponse
	decode(t, createdResponse, &note)
	var filtered struct {
		Items []noteResponse `json:"items"`
	}
	decode(t, s.get(t, base+"/notes?page=true&summary=true&context_kind=question", owner), &filtered)
	if len(filtered.Items) != 1 || filtered.Items[0].NoteID != note.NoteID {
		t.Fatalf("context filter must return the linked note: %+v", filtered)
	}
	var legacyFiltered []noteResponse
	decode(t, s.get(t, base+"/notes?context_kind=question", owner), &legacyFiltered)
	if len(legacyFiltered) != 1 || legacyFiltered[0].NoteID != note.NoteID {
		t.Fatalf("legacy context filter must return the linked note: %+v", legacyFiltered)
	}
	if res := s.get(t, base+"/notes?page=true&summary=true&context_kind=unknown", owner); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown context filter must be rejected: %d", res.StatusCode)
	}
	if note.ContextKind != "question" || note.ContextID != question.ID.String() {
		t.Fatalf("note context was not retained: %+v", note)
	}

	var page struct {
		Items []noteResponse `json:"items"`
	}
	decode(t, s.get(t, base+"/notes?page=true&summary=true", owner), &page)
	if len(page.Items) != 1 || page.Items[0].ContextKind != "question" || page.Items[0].ContextID != question.ID.String() {
		t.Fatalf("paged note context was not retained: %+v", page)
	}
	var searched struct {
		Items []noteResponse `json:"items"`
	}
	decode(t, s.get(t, base+"/notes?page=true&summary=true&q=question", owner), &searched)
	if len(searched.Items) != 1 || searched.Items[0].NoteID != note.NoteID {
		t.Fatalf("note context was not searchable: %+v", searched)
	}
	for _, body := range []string{
		`{"body":"missing context id","context_kind":"question"}`,
		`{"body":"unknown context","context_kind":"tool","context_id":"` + question.ID.String() + `"}`,
	} {
		if res := s.post(t, base+"/notes", body, owner); res.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid note context got %d", res.StatusCode)
		}
	}
}

// **A HALF-SET FILTER is refused rather than ignored.** Asking for
// `subject_kind=host` with no value is a question about every host, which is not
// the question this endpoint answers — and silently answering the OTHER question
// (every note in the engagement) would look like it worked.
//
// A mutation round found this unkilled: every list test sent both halves or
// neither.
func TestAHalfSetSubjectFilterIsRefused(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)
	writeNote(t, s, workspace, `{"body":"the engagement summary"}`, owner)
	base := "/v1/workspaces/" + workspace.String() + "/notes"

	for _, query := range []string{
		"?subject_kind=host",
		"?subject_value=a.acme.test",
	} {
		res := s.get(t, base+query, owner)
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: got %d, want 400", query, res.StatusCode)
		}
	}
	// And NEITHER half is the list, which still works.
	var all []noteResponse
	decode(t, s.get(t, base, owner), &all)
	if len(all) != 1 {
		t.Fatalf("no filter is every note: %d", len(all))
	}
}

// **The kind vocabulary is really asked** — `0034`'s canonical copy lives in
// `scope`, and a note about `hosst:acme.test` would attach to nothing and read
// as a note about something.
func TestANoteAboutAKindNobodyKnowsIsRefused(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)
	res := s.post(t, "/v1/workspaces/"+workspace.String()+"/notes",
		`{"subject_kind":"hosst","subject_value":"acme.test","body":"typo"}`, owner)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", res.StatusCode)
	}
}

// Anybody on the engagement READS a note; only its author edits or erases it.
func TestOnlyTheAuthorEditsOverTheWire(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, kit, owner, kitAuth := firm(t, s, orgdomain.RoleMember)
	seat := "/v1/workspaces/" + workspace.String() + "/members/" + kit.String()
	if res := s.put(t, seat, `{"level":"write"}`, owner); res.StatusCode != http.StatusOK {
		t.Fatalf("grant write: %d", res.StatusCode)
	}
	mine := writeNote(t, s, workspace, `{"body":"sam's note"}`, owner)
	at := "/v1/workspaces/" + workspace.String() + "/notes/" + mine.NoteID
	var direct noteResponse
	decode(t, s.get(t, at, owner), &direct)
	if direct.NoteID != mine.NoteID || direct.Body != mine.Body || !direct.Mine {
		t.Fatalf("direct note read lost authored context: %+v", direct)
	}

	// Kit can SEE it, and the response says it is not theirs to edit.
	var seen []noteResponse
	decode(t, s.get(t, "/v1/workspaces/"+workspace.String()+"/notes", kitAuth), &seen)
	if len(seen) != 1 || seen[0].Mine {
		t.Fatalf("kit reads sam's note and it is not theirs: %+v", seen)
	}
	if res := s.put(t, at, `{"body":"kit's words"}`, kitAuth); res.StatusCode != http.StatusForbidden {
		t.Fatalf("kit edited sam's note: %d", res.StatusCode)
	}
	if res := s.delete(t, at, kitAuth); res.StatusCode != http.StatusForbidden {
		t.Fatalf("kit erased sam's note: %d", res.StatusCode)
	}
	// And the author can.
	if res := s.put(t, at, `{"body":"sam's revised note"}`, owner); res.StatusCode != http.StatusOK {
		t.Fatalf("sam editing their own: %d", res.StatusCode)
	}
	var after []noteResponse
	decode(t, s.get(t, "/v1/workspaces/"+workspace.String()+"/notes", owner), &after)
	if !after[0].Edited || !after[0].Mine {
		t.Fatalf("%+v", after[0])
	}
}

// **The eighth section carries the SUBJECTLESS notes only.** A note about one
// host belongs on that host's drawer; mixing them would put "staging box,
// ignore it" into a document somebody sends a client.
func TestTheReportSectionCarriesTheEngagementSummaryOnly(t *testing.T) {
	s := tracedSystem(t)
	ws, target, owner, _ := engagementWithAClient(t, s)
	writeNote(t, s, ws, `{"body":"the engagement started late"}`, owner)
	writeNote(t, s, ws,
		`{"subject_kind":"host","subject_value":"a.acme.test","body":"staging box"}`, owner)

	opened := openReport(t, s, ws, target, owner)
	res := s.post(t, "/v1/workspaces/"+ws.String()+"/reports/"+opened.ReportID+"/revisions", "", owner)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("issue: %d", res.StatusCode)
	}
	var rev revisionResponse
	decode(t, res, &rev)

	var doc struct {
		Sections []struct {
			Key     string `json:"key"`
			Content []struct {
				Body   string `json:"body"`
				Author string `json:"author"`
			} `json:"content"`
			Counts []struct {
				Label string `json:"label"`
				Value int    `json:"value"`
			} `json:"counts"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(revisionBody(t, s, ws, rev.RevisionID, owner), &doc); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, one := range doc.Sections {
		if one.Key != "engagement_notes" {
			continue
		}
		found = true
		if len(one.Content) != 1 {
			t.Fatalf("only the subjectless note: %+v", one.Content)
		}
		if one.Content[0].Body != "the engagement started late" {
			t.Fatalf("body: %q", one.Content[0].Body)
		}
		// **SOURCED, not free text.** Each note names who wrote it.
		if one.Content[0].Author == "" {
			t.Fatal("a note in a report names its author")
		}
	}
	if !found {
		t.Fatal("the eighth section ships ON")
	}
}

// **A frozen report keeps what a note said THEN**, which is the whole reason
// `0043` could choose editing in place over versioning: `0042` already freezes
// the bytes, and that is the only place a note's past text is load-bearing.
func TestAFrozenReportKeepsWhatANoteSaidWhenItWasIssued(t *testing.T) {
	s := tracedSystem(t)
	ws, target, owner, _ := engagementWithAClient(t, s)
	note := writeNote(t, s, ws, `{"body":"as at the third of September"}`, owner)

	opened := openReport(t, s, ws, target, owner)
	res := s.post(t, "/v1/workspaces/"+ws.String()+"/reports/"+opened.ReportID+"/revisions", "", owner)
	var rev revisionResponse
	decode(t, res, &rev)
	before := revisionBody(t, s, ws, rev.RevisionID, owner)

	// Rewrite the note completely.
	if res := s.put(t, "/v1/workspaces/"+ws.String()+"/notes/"+note.NoteID,
		`{"body":"entirely different words"}`, owner); res.StatusCode != http.StatusOK {
		t.Fatalf("edit: %d", res.StatusCode)
	}

	after := revisionBody(t, s, ws, rev.RevisionID, owner)
	if string(after) != string(before) {
		t.Fatal("editing a note changed a document that had already been issued")
	}
	if !contains(string(after), "as at the third of September") {
		t.Fatal("the frozen bytes must hold the words as they were")
	}
}

// **A closed engagement's notes are READABLE and not WRITABLE**, and `0043` puts
// no rule of its own behind that — it falls out of `0027`, which makes a closed
// engagement a record you can read and not act in. The value of the test is
// precisely that: it proves the note routes went through the shared gate rather
// than around it, which is the only way this could have been wrong.
func TestAClosedEngagementsNotesAreReadableAndNotWritable(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, owner, _ := firm(t, s, orgdomain.RoleMember)
	base := "/v1/workspaces/" + workspace.String() + "/notes"
	before := writeNote(t, s, workspace, `{"body":"written while it was open"}`, owner)

	if res := s.post(t, "/v1/workspaces/"+workspace.String()+"/close", "", owner); res.StatusCode != http.StatusNoContent {
		t.Fatalf("close: %d", res.StatusCode)
	}

	var after []noteResponse
	decode(t, s.get(t, base, owner), &after)
	if len(after) != 1 || after[0].NoteID != before.NoteID {
		t.Fatalf("a closed engagement's notes still read: %+v", after)
	}

	// Every WRITE is refused — and the author being the one asking changes
	// nothing, because this gate is about the engagement and not the person.
	at := base + "/" + before.NoteID
	if res := s.post(t, base, `{"body":"after the close"}`, owner); res.StatusCode != http.StatusConflict {
		t.Fatalf("wrote a note into a closed engagement: %d", res.StatusCode)
	}
	if res := s.put(t, at, `{"body":"revised after the close"}`, owner); res.StatusCode != http.StatusConflict {
		t.Fatalf("edited a note in a closed engagement: %d", res.StatusCode)
	}
	if res := s.delete(t, at, owner); res.StatusCode != http.StatusConflict {
		t.Fatalf("erased a note in a closed engagement: %d", res.StatusCode)
	}
}
