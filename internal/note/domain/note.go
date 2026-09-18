package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	MaxBodyLength  = 20000
	MaxValueLength = 2000
)

// Note is a person's own text — decisions/0043, and **the only input this system
// has that nothing else produces.** Every other row here is something a tool
// said, a rule decided, or a subscriber assembled.
//
//	no subject   the ENGAGEMENT SUMMARY — 0042's eighth report section
//	a subject    "this host is a staging box, ignore it"
//
// One noun, two uses, and the difference is whether two columns are filled.
type Note struct {
	ID          id.ID
	WorkspaceID id.ID

	// SubjectKind and SubjectValue are the TUPLE, not a foreign key — `0036`
	// made a fragment IS `(workspace, kind, value)`, so a note points at the
	// same thing and holds no `fragment_id`.
	//
	// That buys three things: a note SURVIVES re-observation, because the
	// fragment row is upserted and the tuple is stable; a note about something
	// NEVER OBSERVED is still a note, which is the record that somebody knew
	// before the machinery did; and there is no polymorphic foreign key, which
	// `0009` rejected on the ground the database cannot enforce it.
	//
	// **They move together.** A half-set subject is a note about nothing,
	// spelled like a note about something.
	SubjectKind  string
	SubjectValue string

	Body string

	// Author is who wrote it and never changes. An edit moves `UpdatedAt` and
	// keeps this: a note is somebody's statement, and rewriting the byline is
	// how it stops being one.
	Author    id.ID
	CreatedAt time.Time
	UpdatedAt time.Time
}

// New writes a note. `subjectKind` and `subjectValue` are optional and must be
// supplied together.
//
// The KIND is not validated here — the vocabulary is `scope`'s canonical copy
// (`0034`) and this package may not import it. The composition root checks it,
// which is where every other domain's kind is checked too.
func New(newID, workspace, author id.ID, subjectKind, subjectValue, body string,
	at time.Time) (Note, error) {
	if newID.IsZero() || author.IsZero() {
		return Note{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Note{}, ErrWorkspaceRequired
	}
	if at.IsZero() {
		return Note{}, ErrTimeRequired
	}
	body = strings.TrimSpace(body)
	if body == "" || len(body) > MaxBodyLength {
		return Note{}, ErrBodyRequired
	}
	kind, value, err := subject(subjectKind, subjectValue)
	if err != nil {
		return Note{}, err
	}
	return Note{
		ID: newID, WorkspaceID: workspace,
		SubjectKind: kind, SubjectValue: value,
		Body: body, Author: author, CreatedAt: at, UpdatedAt: at,
	}, nil
}

// subject folds and pairs. **The value is FOLDED, matching
// `entity.fragment.value`** — `0037` and `0040` both named the fold mismatch as
// their own quiet failure, and this is the third table to join on it.
func subject(kind, value string) (string, string, error) {
	kind = strings.TrimSpace(kind)
	value = Fold(value)
	switch {
	case kind == "" && value == "":
		// The engagement summary. Legal, and it is what 0042's section carries.
		return "", "", nil
	case kind == "" || value == "":
		return "", "", ErrSubjectHalfSet
	case len(value) > MaxValueLength:
		return "", "", ErrSubjectTooLong
	}
	return kind, value, nil
}

// Fold is the one place a note's subject value is normalised, and it must agree
// with `entity`'s: a note attaches to a fragment by tuple, and two folds one
// domain apart is a note about nothing.
func Fold(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// About says whether this note is attached to something, as opposed to being
// the engagement summary. It is a function so the report section and the list
// filter cannot disagree about what "subjectless" means.
func (n Note) About() bool { return n.SubjectKind != "" }

// Edit replaces the body. **The author never moves and neither does
// `CreatedAt`** — a note is somebody's statement at a time, and an edit is that
// person revising it rather than a new statement by somebody else.
//
// It is edited IN PLACE and not versioned — decisions/0043 §2. `mapping` and
// `scope.rule` are append-only because three surfaces cite them by id and a
// moved expression makes a citation a lie; nothing cites a note that way, and
// the one place its past text is load-bearing is a report, whose bytes `0042`
// already freezes.
func (n Note) Edit(by id.ID, body string, at time.Time) (Note, error) {
	if by.IsZero() {
		return n, ErrIDRequired
	}
	if at.IsZero() {
		return n, ErrTimeRequired
	}
	// **The author is NOT reassigned below, and a mutation that reassigns it is
	// an EQUIVALENT MUTANT** — this guard has already established that `by` IS
	// the author, so writing it again changes nothing. It is worth saying out
	// loud because the next reader will wonder why the byline is not defended
	// after the check rather than by it.
	if by != n.Author {
		// **ONLY THE AUTHOR EDITS.** Anybody on the engagement may READ it —
		// that is what makes it useful — but a note is a person's own text, and
		// somebody else changing the words under their name is the one thing
		// that would make it untrustworthy.
		return n, ErrNotYours
	}
	body = strings.TrimSpace(body)
	if body == "" || len(body) > MaxBodyLength {
		return n, ErrBodyRequired
	}
	next := n
	next.Body = body
	next.UpdatedAt = at
	return next, nil
}

// Edited says whether this note has changed since it was written. It is a
// comparison rather than a stored flag, because a flag is a second authority
// over two timestamps that already answer it.
func (n Note) Edited() bool { return n.UpdatedAt.After(n.CreatedAt) }

const (
	EventNoteWritten = "note.written"

	SubjectKind = "workspace"
)

// Written is a DECISION — 0014. A person chose to record something about a
// client's estate: that has an author, no outcome column, and a reason to
// outlive the journal's retention.
type Written struct {
	WorkspaceID string `json:"workspace_id"`
	NoteID      string `json:"note_id"`
	// About is the subject, or empty for the engagement summary. A subscriber
	// cannot compute it — 0013 — and "somebody wrote about this host" is the
	// sentence an activity feed wants.
	About string `json:"about,omitempty"`
	Edit  bool   `json:"edit"`
}
