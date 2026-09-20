// Author-written, from decisions/0043's Verification block, which was written
// before this code.
package domain_test

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/note/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var (
	written = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	later   = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
)

func an(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}

func summary(t *testing.T) domain.Note {
	t.Helper()
	n, err := domain.New(an(1), an(2), an(3), "", "", "the engagement started late", written)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// **A note with NO subject is legal**, and it is the engagement summary —
// `0042`'s eighth report section. One noun, two uses, and the difference is
// whether two columns are filled.
func TestASubjectlessNoteIsTheEngagementSummary(t *testing.T) {
	got := summary(t)
	if got.About() {
		t.Fatal("no subject means the engagement summary")
	}
	if got.SubjectKind != "" || got.SubjectValue != "" {
		t.Fatalf("%+v", got)
	}
}

// **A HALF-SET subject is a note about nothing spelled like a note about
// something.** The two columns move together or neither does.
func TestASubjectIsBothHalvesOrNeither(t *testing.T) {
	for _, tc := range []struct{ kind, value string }{
		{"host", ""},
		{"", "a.acme.test"},
		{"host", "   "},
	} {
		if _, err := domain.New(an(1), an(2), an(3), tc.kind, tc.value, "text",
			written); !errors.Is(err, domain.ErrSubjectHalfSet) {
			t.Fatalf("%q/%q: want ErrSubjectHalfSet, got %v", tc.kind, tc.value, err)
		}
	}
}

// The value is FOLDED, matching `entity.fragment.value`. `0037` and `0040` both
// named the fold mismatch as their own quiet failure, and this is the third
// table to join on it.
func TestASubjectValueIsFolded(t *testing.T) {
	got, err := domain.New(an(1), an(2), an(3), "host", "  A.ACME.Test  ",
		"staging box, ignore", written)
	if err != nil {
		t.Fatal(err)
	}
	if got.SubjectValue != "a.acme.test" {
		t.Fatalf("folded: %q", got.SubjectValue)
	}
	if got.SubjectValue != domain.Fold("A.ACME.Test") {
		t.Fatal("the note's fold and the exported Fold must be the same function")
	}
	if !got.About() {
		t.Fatal("a note with a subject is about something")
	}
}

func TestAResearchContextIsTypedAndPaired(t *testing.T) {
	contextID := an(7)
	got, err := domain.NewWithContext(an(1), an(2), an(3), "", "", "question", contextID, "follow up", written)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContextKind != "question" || got.ContextID != contextID {
		t.Fatalf("context: %+v", got)
	}
	for _, tc := range []struct {
		kind string
		id   id.ID
		want error
	}{
		{kind: "question", want: domain.ErrContextHalfSet},
		{id: contextID, want: domain.ErrContextHalfSet},
		{kind: "tool", id: contextID, want: domain.ErrContextKindUnknown},
	} {
		if _, err := domain.NewWithContext(an(1), an(2), an(3), "", "", tc.kind, tc.id, "text", written); !errors.Is(err, tc.want) {
			t.Fatalf("%q/%v: want %v, got %v", tc.kind, tc.id, tc.want, err)
		}
	}
}

// **ONLY THE AUTHOR EDITS.** Anybody on the engagement may read a note — that is
// what makes it useful — but somebody else changing the words under a person's
// name is the one thing that would make it untrustworthy.
func TestOnlyTheAuthorMayEdit(t *testing.T) {
	held := summary(t)
	if _, err := held.Edit(an(9), "rewritten", later); !errors.Is(err, domain.ErrNotYours) {
		t.Fatalf("want ErrNotYours, got %v", err)
	}
	got, err := held.Edit(an(3), "the engagement started on time after all", later)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "the engagement started on time after all" {
		t.Fatalf("body: %q", got.Body)
	}
}

// An edit moves `UpdatedAt` and NOTHING ELSE that identifies the statement. A
// note is somebody's text at a time, and rewriting the byline is how it stops
// being one.
func TestAnEditKeepsTheAuthorAndTheBirthday(t *testing.T) {
	held := summary(t)
	got, err := held.Edit(an(3), "revised", later)
	if err != nil {
		t.Fatal(err)
	}
	if got.Author != held.Author {
		t.Fatalf("the author moved: %v", got.Author)
	}
	if !got.CreatedAt.Equal(written) {
		t.Fatalf("created_at moved: %v", got.CreatedAt)
	}
	if !got.UpdatedAt.Equal(later) {
		t.Fatalf("updated_at: %v", got.UpdatedAt)
	}
	// `Edited` is DERIVED from the two timestamps rather than stored — a flag
	// would be a second authority over something they already answer.
	if held.Edited() {
		t.Fatal("a fresh note is not edited")
	}
	if !got.Edited() {
		t.Fatal("an edited one is")
	}
	// AND THE RECEIVER IS UNTOUCHED. Every constructor here returns a value the
	// caller may keep.
	if held.Body == got.Body {
		t.Fatal("Edit mutated its receiver")
	}
}

func TestANoteWithNoTextIsNotANote(t *testing.T) {
	if _, err := domain.New(an(1), an(2), an(3), "", "", "   ", written); !errors.Is(err, domain.ErrBodyRequired) {
		t.Fatalf("want ErrBodyRequired, got %v", err)
	}
	held := summary(t)
	if _, err := held.Edit(an(3), "  ", later); !errors.Is(err, domain.ErrBodyRequired) {
		t.Fatalf("editing to nothing: want ErrBodyRequired, got %v", err)
	}
}

// A note names who wrote it, always. There is no system path here: `note` is the
// one input this system has that nothing else produces.
func TestANoteAlwaysNamesItsAuthor(t *testing.T) {
	if _, err := domain.New(an(1), an(2), id.ID{}, "", "", "text",
		written); !errors.Is(err, domain.ErrIDRequired) {
		t.Fatalf("want ErrIDRequired, got %v", err)
	}
}
