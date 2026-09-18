// Author-written, 2026-09-08. Extraction assumed JSON everywhere until this
// date, and the cost was silent: `subfinder -silent` wrote 487KB of hostnames
// and the run finished green with `fields_seen: 0`.
package domain_test

import (
	"strings"
	"testing"

	"github.com/0xsj/overwatch-backend/internal/observation/domain"
)

// The shape comes off the DECLARED media type, never the content — the same
// rule `root/runports.go` states for reading it off the argv in the first place.
func TestTheShapeComesFromTheDeclaredMediaType(t *testing.T) {
	for _, tc := range []struct {
		media string
		want  domain.Shape
	}{
		{"text/plain", domain.ShapeLines},
		{"text/plain; charset=utf-8", domain.ShapeLines},
		{"TEXT/PLAIN", domain.ShapeLines},
		{"application/json", domain.ShapeJSON},
		// EMPTY IS JSON — what extraction assumed before it asked. An
		// unrecognised type is not evidence of text, and defaulting it to lines
		// would turn a JSON artifact into one `.line` path per record: wrong in
		// a way that looks like a working extraction with a strange schema.
		{"", domain.ShapeJSON},
		{"application/x-ndjson", domain.ShapeJSON},
	} {
		if got := domain.ShapeOf(tc.media); got != tc.want {
			t.Errorf("%q: got %v want %v", tc.media, got, tc.want)
		}
	}
}

// The format most of the recon corpus emits: one identifier per line, no keys.
func TestATextArtifactIsOneRecordPerLine(t *testing.T) {
	body := "a.acme.test\nb.acme.test\nc.acme.test\n"
	got, err := domain.Extract([]byte(body),
		[]domain.Mapping{subject(t, 1, "host", "."+domain.LinePath)},
		"host", domain.ShapeLines)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Records) != 3 {
		t.Fatalf("three lines is three records, got %d", len(got.Records))
	}
	for n, want := range []string{"a.acme.test", "b.acme.test", "c.acme.test"} {
		if got.Records[n].SubjectValue != want {
			t.Errorf("record %d: got %q want %q", n, got.Records[n].SubjectValue, want)
		}
		if got.Records[n].SubjectKind != "host" {
			t.Errorf("the subject kind is the TOOL's, not the line's: %q",
				got.Records[n].SubjectKind)
		}
	}
	// ONE PATH, and a mapping claimed it. A text record has exactly one leaf
	// however many lines there are, so the field accounting is `1 seen, 1
	// mapped, 0 left alone` — not three of each.
	if len(got.Seen) != 1 || got.Seen[0] != "."+domain.LinePath {
		t.Errorf("a text record has one leaf: %+v", got.Seen)
	}
	if len(got.LeftAlone) != 0 {
		t.Errorf("the mapping claimed it: %+v", got.LeftAlone)
	}
}

// A blank line is NOT a record. Every tool here ends with a newline and several
// separate sections with one; counting them would inflate `seen` and produce
// subjects that are statements about nothing.
func TestBlankLinesAreNotRecords(t *testing.T) {
	got, err := domain.Extract([]byte("\n\na.acme.test\n\n\nb.acme.test\n\n"),
		[]domain.Mapping{subject(t, 1, "host", "."+domain.LinePath)},
		"host", domain.ShapeLines)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Records) != 2 {
		t.Fatalf("two values among the blanks, got %d: %+v", len(got.Records), got.Records)
	}
}

// A tool piped through something that writes CRLF has still said `acme.test`.
// Keeping the `\r` would make this fragment differ from the same host observed
// anywhere else — the dedup is on (workspace, kind, value), so the graph would
// carry two of everything and nothing would say why.
func TestACarriageReturnIsNotPartOfTheValue(t *testing.T) {
	got, err := domain.Extract([]byte("a.acme.test\r\nb.acme.test\r\n"),
		[]domain.Mapping{subject(t, 1, "host", "."+domain.LinePath)},
		"host", domain.ShapeLines)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range got.Records {
		if strings.ContainsAny(r.SubjectValue, "\r\n") {
			t.Errorf("the line ENDING is not part of the value: %q", r.SubjectValue)
		}
	}
}

// THE BUG, pinned. The bytes are fine, the mapping is fine, the tool is fine —
// and reading them as JSON produces nothing at all, indistinguishable from a
// tool that printed a banner and no records.
func TestTextReadAsJSONIsSilentlyEmpty(t *testing.T) {
	body := []byte("a.acme.test\nb.acme.test\n")
	ms := []domain.Mapping{subject(t, 1, "host", "."+domain.LinePath)}

	asJSON, err := domain.Extract(body, ms, "host", domain.ShapeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(asJSON.Records) != 0 || len(asJSON.Seen) != 0 {
		t.Fatalf("this is what was happening, and it was silent: %+v", asJSON)
	}

	asLines, err := domain.Extract(body, ms, "host", domain.ShapeLines)
	if err != nil {
		t.Fatal(err)
	}
	if len(asLines.Records) != 2 {
		t.Fatalf("the same bytes, declared correctly: %+v", asLines)
	}
}

// The reverse: JSON declared as text. It reads, and it reads WRONGLY — one
// `.line` per record whose value is the whole undecoded object. That is the
// argument for defaulting an unknown media type to JSON rather than to lines: a
// silent nothing is at least obviously nothing.
func TestJSONDeclaredAsTextReadsTheWholeLine(t *testing.T) {
	got, err := domain.Extract([]byte(httpxLine),
		[]domain.Mapping{subject(t, 1, "host", "."+domain.LinePath)},
		"host", domain.ShapeLines)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Records) != 1 {
		t.Fatalf("one line: %+v", got.Records)
	}
	if !strings.HasPrefix(got.Records[0].SubjectValue, `{"url"`) {
		t.Errorf("a text line is its whole content: %q", got.Records[0].SubjectValue)
	}
}

// An artifact with bytes but nothing in them is still nothing.
func TestAnEmptyTextArtifactHasNoRecords(t *testing.T) {
	for _, body := range []string{"", "\n", "   \n\t\n"} {
		got, err := domain.Extract([]byte(body),
			[]domain.Mapping{subject(t, 1, "host", "."+domain.LinePath)},
			"host", domain.ShapeLines)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Records) != 0 || len(got.Seen) != 0 {
			t.Errorf("%q: got %+v", body, got)
		}
	}
}
