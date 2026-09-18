package root

import (
	"net/http"
	"strconv"
	"time"

	notedomain "github.com/0xsj/overwatch-backend/internal/note/domain"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type noteResponse struct {
	NoteID string `json:"note_id"`

	// SubjectKind and SubjectValue are ABSENT on the engagement summary, and
	// that absence is the difference between the two uses of this noun.
	SubjectKind  string `json:"subject_kind,omitempty"`
	SubjectValue string `json:"subject_value,omitempty"`

	Body   string `json:"body"`
	Author string `json:"author"`

	CreatedAt string `json:"created_at"`
	// Edited is derived from the two timestamps rather than stored — a flag
	// would be a second authority over something they already answer.
	Edited    bool   `json:"edited"`
	UpdatedAt string `json:"updated_at"`

	// Mine tells a screen whether to draw an edit control. Only the author may
	// change the words under their own name, and a button that 403s is worse
	// than no button.
	Mine bool `json:"mine"`
}

func asNote(n notedomain.Note, caller id.ID) noteResponse {
	return noteResponse{
		NoteID:      n.ID.String(),
		SubjectKind: n.SubjectKind, SubjectValue: n.SubjectValue,
		Body: n.Body, Author: n.Author.String(),
		CreatedAt: n.CreatedAt.UTC().Format(time.RFC3339Nano),
		Edited:    n.Edited(),
		UpdatedAt: n.UpdatedAt.UTC().Format(time.RFC3339Nano),
		Mine:      n.Author == caller,
	}
}

type writeNoteRequest struct {
	// Both optional and both together. Absent is the ENGAGEMENT SUMMARY —
	// `0042`'s eighth report section.
	SubjectKind  string `json:"subject_kind"`
	SubjectValue string `json:"subject_value"`
	Body         string `json:"body"`
}

// listNotes answers every note in an engagement, or the ones about one subject.
//
// **It is not search.** `CLAUDE.md` says a note is *"searched beside assets and
// observations"*; there is no search surface here for anything, and `0043` §4
// records that as a noun shipped short of its own definition rather than
// pretending a filtered list is the same thing.
func (m *me) listNotes(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	kind := r.URL.Query().Get("subject_kind")
	value := r.URL.Query().Get("subject_value")

	var (
		found []notedomain.Note
		err   error
	)
	switch {
	case r.URL.Query().Get("summary") == "true":
		found, err = m.notes.Summary(r.Context(), workspace, size)
	case kind != "" || value != "":
		// A HALF-SET filter is refused rather than ignored: asking for
		// `subject_kind=host` with no value is a question about every host,
		// which is not the question this endpoint answers.
		found, err = m.notes.About(r.Context(), workspace, kind, value, size)
	default:
		found, err = m.notes.List(r.Context(), workspace, size)
	}
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := make([]noteResponse, 0, len(found))
	for _, one := range found {
		out = append(out, asNote(one, caller))
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

func (m *me) readNote(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("note"))
	if err != nil {
		httpx.Fail(m.log, w, r, notedomain.ErrNotFound)
		return
	}
	found, err := m.notes.ByID(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asNote(found, caller))
}

// writeNote needs `write`. It is a person recording something about a client's
// estate, which is an act rather than a read — and a `client` cannot, because
// their ceiling is `read` and the gate refuses them anyway.
func (m *me) writeNote(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in writeNoteRequest
	if !decodeBody(w, r, &in) {
		return
	}
	written, err := m.notesCmd.Write(r.Context(), workspace, caller,
		in.SubjectKind, in.SubjectValue, in.Body)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, asNote(written, caller))
}

type editNoteRequest struct {
	Body string `json:"body"`
}

func (m *me) editNote(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("note"))
	if err != nil {
		httpx.Fail(m.log, w, r, notedomain.ErrNotFound)
		return
	}
	var in editNoteRequest
	if !decodeBody(w, r, &in) {
		return
	}
	next, err := m.notesCmd.Edit(r.Context(), workspace, want, caller, in.Body)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asNote(next, caller))
}

func (m *me) eraseNote(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("note"))
	if err != nil {
		httpx.Fail(m.log, w, r, notedomain.ErrNotFound)
		return
	}
	if err := m.notesCmd.Erase(r.Context(), workspace, want, caller); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
