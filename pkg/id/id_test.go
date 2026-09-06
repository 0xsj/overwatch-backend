// AUTHOR-WRITTEN, not produced behind an information barrier. pkg/id's behaviour
// is round-trips and formats rather than a property matrix, so a spec-derived
// run was judged not worth its cost — custody has no entry for this package.
package id_test

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	pkgerrors "github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type fixedClock struct{ t time.Time }

func (c *fixedClock) Now() time.Time { return c.t }

func at(y int, mo time.Month, d, h, mi, s int) time.Time {
	return time.Date(y, mo, d, h, mi, s, 0, time.UTC)
}

func TestNilIsTheZeroValueAndNamesNothing(t *testing.T) {
	var zero id.ID
	if zero != id.Nil {
		t.Errorf("the zero ID must equal Nil, or an unset field has two empty states")
	}
	if !zero.IsZero() {
		t.Errorf("IsZero must be true for the zero value; it exists so callers need not compare against Nil")
	}
	if zero.Version() != 0 {
		t.Errorf("Nil reads as version %d, want 0 — an identifier nobody set must not pass for a real one", zero.Version())
	}
	if _, ok := zero.Time(); ok {
		t.Errorf("Nil must report no timestamp; version 0 is not version 7")
	}
}

func TestParseRoundTripsAndNormalisesCase(t *testing.T) {
	g := id.NewSequence(at(2026, 9, 6, 12, 0, 0))
	original := g.NewID()

	back, err := id.Parse(original.String())
	if err != nil || back != original {
		t.Fatalf("Parse(String()) = %v, %v; want the original back", back, err)
	}

	upper := bytes.ToUpper([]byte(original.String()))
	fromUpper, err := id.Parse(string(upper))
	if err != nil {
		t.Fatalf("Parse must accept the uppercase form: %v", err)
	}
	if fromUpper != original {
		t.Errorf("case must normalise, so identifiers compare as values and never as strings")
	}
	// AMENDED 2026-09-06, custody 0014 M06: this compared
	// `fromUpper.String() != original.String()`, so both sides ran through the
	// function under test and a String that emitted UPPERCASE passed it.
	// Assert against a literal instead.
	if got := fromUpper.String(); got != strings.ToLower(got) {
		t.Errorf("String() emitted %q; it must be lowercase whatever the input case was", got)
	}
}

func TestParseRejects(t *testing.T) {
	valid := id.NewSequence(at(2026, 9, 6, 12, 0, 0)).NewID().String()
	tests := []struct {
		name, in string
	}{
		{"the nil identifier, which is what an unset field serialises to", "00000000-0000-0000-0000-000000000000"},
		{"a string of the wrong length", valid[:35]},
		{"hyphens in the wrong places", valid[:8] + "x" + valid[9:]},
		{"a non-hexadecimal character", "zzzzzzzz" + valid[8:]},
		{"empty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := id.Parse(tt.in)
			if err == nil {
				t.Fatalf("Parse(%q) succeeded, returning %v", tt.in, got)
			}
			if got != id.Nil {
				t.Errorf("a failed Parse must return Nil, not a partial value")
			}
			if !pkgerrors.IsKind(err, pkgerrors.Invalid) {
				t.Errorf("a bad identifier is the caller's mistake: want Invalid, got %v", pkgerrors.KindOf(err))
			}
			if fields := pkgerrors.FieldsOf(err); fields["id"] == "" {
				t.Errorf("the error must name what was wrong with it, got fields %v", fields)
			}
		})
	}
}

func TestUnmarshalTextLeavesTheReceiverAloneWhenItFails(t *testing.T) {
	original := id.NewSequence(at(2026, 9, 6, 12, 0, 0)).NewID()
	subject := original

	if err := subject.UnmarshalText([]byte("not an identifier")); err == nil {
		t.Fatal("UnmarshalText accepted garbage")
	}
	if subject != original {
		t.Errorf("a failed UnmarshalText mutated the receiver to %v — a partially decoded identifier is worse than none, because it is well-formed enough to be stored", subject)
	}
}

func TestJSONRoundTripsThroughTheCanonicalForm(t *testing.T) {
	type row struct{ ID id.ID }
	original := row{ID: id.NewSequence(at(2026, 9, 6, 12, 0, 0)).NewID()}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(encoded, []byte(original.ID.String())) {
		t.Errorf("JSON must carry the canonical form, got %s", encoded)
	}
	var decoded row
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != original {
		t.Errorf("round trip gave %v, %v", decoded, err)
	}
	if err := json.Unmarshal([]byte(`{"ID":"nonsense"}`), &decoded); err == nil {
		t.Errorf("a malformed identifier in JSON must be refused, not silently zeroed")
	}
}

func TestIDsAreComparableAndUsableAsMapKeys(t *testing.T) {
	g := id.NewSequence(at(2026, 9, 6, 12, 0, 0))
	a, b := g.NewID(), g.NewID()
	seen := map[id.ID]string{a: "a", b: "b"}
	if seen[a] != "a" || seen[b] != "b" || a == b {
		t.Errorf("an array type must be comparable and distinct per value; that is why ID is not a slice")
	}
}

func TestTimeReportsTheStampedMillisecondAndOnlyForV7(t *testing.T) {
	want := at(2026, 9, 6, 12, 34, 56)
	got, ok := id.NewSequence(want).NewID().Time()
	if !ok {
		t.Fatal("a version 7 identifier must report its timestamp")
	}
	if !got.Equal(want) {
		t.Errorf("Time() = %v, want %v", got, want)
	}
	if got.Location() != time.UTC {
		t.Errorf("Time() must be UTC, got %v", got.Location())
	}

	withNanos := want.Add(999999 * time.Nanosecond)
	got2, _ := id.NewSequence(withNanos).NewID().Time()
	if !got2.Equal(want) {
		t.Errorf("the stamp is milliseconds, so sub-millisecond precision is lost: got %v, want %v", got2, want)
	}
}

func TestV7StampsTheClockAndIsWellFormed(t *testing.T) {
	now := at(2026, 9, 6, 12, 0, 0)
	g := id.NewV7(&fixedClock{now}, rand.Reader)
	got := g.NewID()

	if got.Version() != 7 {
		t.Errorf("version = %d, want 7", got.Version())
	}
	if variant := got[8] >> 6; variant != 0b10 {
		t.Errorf("variant bits = %02b, want 10 — RFC 9562 requires it and a parser elsewhere may check", variant)
	}
	if stamped, _ := got.Time(); !stamped.Equal(now) {
		t.Errorf("Time() = %v, want the clock's %v", stamped, now)
	}
}

func TestV7IsOrderedWithinOneMillisecond(t *testing.T) {
	g := id.NewV7(&fixedClock{at(2026, 9, 6, 12, 0, 0)}, rand.Reader)
	prev := g.NewID()
	for i := 0; i < 100; i++ {
		next := g.NewID()
		if bytes.Compare(prev[:], next[:]) >= 0 {
			t.Fatalf("identifier %d did not sort after its predecessor; the counter in rand_a exists so a shared millisecond still orders", i)
		}
		prev = next
	}
}

func TestV7SurvivesTheClockGoingBackwards(t *testing.T) {
	c := &fixedClock{at(2026, 9, 6, 12, 0, 0)}
	g := id.NewV7(c, rand.Reader)
	before := g.NewID()

	c.t = c.t.Add(-time.Hour)
	after := g.NewID()

	if bytes.Compare(before[:], after[:]) >= 0 {
		t.Errorf("an identifier minted after a backwards clock step sorted before its predecessor — ordering is a correctness property and the timestamp is an approximation, so the high-water mark wins")
	}
}

func TestV7SequenceOverflowBorrowsFromTheNextMillisecond(t *testing.T) {
	now := at(2026, 9, 6, 12, 0, 0)
	g := id.NewV7(&fixedClock{now}, rand.Reader)

	var prev id.ID
	for i := 0; i <= 0xFFF+2; i++ {
		got := g.NewID()
		if i > 0 && bytes.Compare(prev[:], got[:]) >= 0 {
			t.Fatalf("ordering broke at %d, where the 12-bit counter wraps", i)
		}
		prev = got
	}
	stamped, _ := prev.Time()
	if !stamped.After(now) {
		t.Errorf("after 4096 identifiers in one millisecond the stamp must borrow from the next, got %v", stamped)
	}
}

func TestV7RejectsMissingDependencies(t *testing.T) {
	for _, tt := range []struct {
		name    string
		clock   id.Clock
		entropy io.Reader
		want    string
	}{
		{"a nil clock", nil, rand.Reader, "clock"},
		{"a nil entropy source", &fixedClock{time.Now()}, nil, "entropy"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			wantPanic(t, tt.want, func() { _ = id.NewV7(tt.clock, tt.entropy) })
		})
	}
}

func TestV7PanicsWhenEntropyRunsOut(t *testing.T) {
	wantPanic(t, "entropy", func() {
		g := id.NewV7(&fixedClock{time.Now()}, bytes.NewReader([]byte{1, 2, 3}))
		_ = g.NewID()
	})
}

func TestSequenceIsDeterministicAndOrderedPastTheCounter(t *testing.T) {
	start := at(2026, 9, 6, 12, 0, 0)
	a, b := id.NewSequence(start), id.NewSequence(start)
	if a.NewID() != b.NewID() {
		t.Fatal("two Sequences from the same instant must issue the same identifiers, or fixtures are not reproducible")
	}

	g := id.NewSequence(start)
	var xs []id.ID
	for i := 0; i < 5000; i++ {
		xs = append(xs, g.NewID())
	}
	if !sort.SliceIsSorted(xs, func(i, j int) bool { return bytes.Compare(xs[i][:], xs[j][:]) < 0 }) {
		t.Errorf("Sequence must stay ordered past the 4096 that fit in rand_a, so fixtures read in the order they were written")
	}
	if v := xs[len(xs)-1].Version(); v != 7 {
		t.Errorf("test identifiers must be well-formed version 7 so downstream parsers behave as in production, got version %d", v)
	}
}

func TestGeneratorsAreSafeForConcurrentUse(t *testing.T) {
	for _, tt := range []struct {
		name string
		g    id.Generator
	}{
		{"V7", id.NewV7(&fixedClock{at(2026, 9, 6, 12, 0, 0)}, rand.Reader)},
		{"Sequence", id.NewSequence(at(2026, 9, 6, 12, 0, 0))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const n = 200
			var mu sync.Mutex
			seen := make(map[id.ID]bool, n)
			var wg sync.WaitGroup
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					v := tt.g.NewID()
					mu.Lock()
					defer mu.Unlock()
					if seen[v] {
						t.Errorf("%s issued a duplicate under concurrency", tt.name)
					}
					seen[v] = true
				}()
			}
			wg.Wait()
		})
	}
}

func TestParseErrorNamesTheInputAndIsSafeToEcho(t *testing.T) {
	_, err := id.Parse("nope")
	if err == nil {
		t.Fatal("expected an error")
	}
	if msg := pkgerrors.Message(err); !bytes.Contains([]byte(msg), []byte("nope")) {
		t.Errorf("the message should name the input, which is the caller's own and therefore safe to echo: got %q", msg)
	}
	if errors.Is(err, nil) {
		t.Errorf("unreachable; keeps the stdlib errors import honest")
	}
}

// wantPanic asserts WHICH panic, not merely that one happened. `recover() != nil`
// cannot distinguish a deliberate guard from a nil dereference two statements
// later, so it passes when the guard is deleted — measured on custody 0010
// (M27/M28) and 0014.
func wantPanic(t *testing.T, contains string, call func()) {
	t.Helper()
	defer func() {
		v := recover()
		if v == nil {
			t.Errorf("did not panic; wanted the guard mentioning %q", contains)
			return
		}
		s, ok := v.(string)
		if !ok || !strings.Contains(s, contains) {
			t.Errorf("panicked with %v (%T); wanted the guard mentioning %q", v, v, contains)
		}
	}()
	call()
}
