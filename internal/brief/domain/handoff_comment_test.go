package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestSnapshotCommentNormalisesAndKeepsAuthor(t *testing.T) {
	comment, err := NewSnapshotComment(id.ID{1}, id.ID{2}, id.ID{3}, id.ID{4}, "  Clarify the independent account.  ", time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if comment.Body != "Clarify the independent account." || comment.AuthorID != (id.ID{4}) {
		t.Fatalf("unexpected comment: %+v", comment)
	}
}

func TestSnapshotCommentRejectsMissingAndOversizedBody(t *testing.T) {
	args := []id.ID{{1}, {2}, {3}, {4}}
	if _, err := NewSnapshotComment(args[0], args[1], args[2], args[3], "  ", time.Unix(1, 0)); err != ErrCommentRequired {
		t.Fatalf("got %v, want required comment error", err)
	}
	if _, err := NewSnapshotComment(args[0], args[1], args[2], args[3], strings.Repeat("x", MaxCommentBodyLength+1), time.Unix(1, 0)); err != ErrCommentTooLong {
		t.Fatalf("got %v, want oversized comment error", err)
	}
}
