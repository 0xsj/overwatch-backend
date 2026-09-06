// Specification-derived tests for github.com/0xsj/overwatch-backend/pkg/id.
//
// Written against `go doc -all ./pkg/id` and notes/substrate/rfc-9562-uuidv7.md
// only. The implementation was never read. Every top-level identifier here is
// prefixed with Spec/spec so it cannot collide with the existing id_test.go
// in this package directory.
package id_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

// specFixedInstant is an arbitrary, ms-aligned instant reused across tests
// that need a clock but do not care which instant it is.
var specFixedInstant = time.Date(2026, 9, 6, 12, 30, 0, 0, time.UTC)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// specControllableClock implements id.Clock and lets a test step the wall
// clock forward or backward between calls.
type specControllableClock struct {
	mu  sync.Mutex
	now time.Time
}

func newSpecControllableClock(t time.Time) *specControllableClock {
	return &specControllableClock{now: t}
}

func (c *specControllableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *specControllableClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// specConstReader is an io.Reader that fills every read with one repeated
// byte value. It is stateless and therefore trivially safe for concurrent
// use, which the spec requires of anything handed to V7.
type specConstReader struct{ b byte }

func (r specConstReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.b
	}
	return len(p), nil
}

// specFailingReader always fails, to exercise the documented "entropy
// source that fails panics" behaviour.
type specFailingReader struct{}

func (specFailingReader) Read(p []byte) (int, error) {
	return 0, errors.New("spec: entropy source unavailable")
}

// specRendezvousReader requires two concurrent Read calls to arrive before
// either returns. If V7.NewID holds its internal lock across the entropy
// read, a second concurrent NewID call can never reach Read while the first
// is blocked inside it, and this reader times out. If V7 reads entropy
// without holding the lock (as documented), both calls reach Read close
// enough together that the rendezvous succeeds quickly.
type specRendezvousReader struct {
	mu      sync.Mutex
	arrived int
	release chan struct{}
}

func newSpecRendezvousReader() *specRendezvousReader {
	return &specRendezvousReader{release: make(chan struct{})}
}

func (r *specRendezvousReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	r.arrived++
	n := r.arrived
	r.mu.Unlock()

	if n >= 2 {
		close(r.release)
	} else {
		select {
		case <-r.release:
		case <-time.After(2 * time.Second):
			return 0, errors.New("spec: timed out waiting for a second concurrent entropy read")
		}
	}

	for i := range p {
		p[i] = 0x42
	}
	return len(p), nil
}

// ---------------------------------------------------------------------------
// Generic helpers
// ---------------------------------------------------------------------------

func specGenerateN(g id.Generator, n int) []id.ID {
	out := make([]id.ID, n)
	for i := range out {
		out[i] = g.NewID()
	}
	return out
}

// specMillisOf reads the 48-bit big-endian millisecond timestamp out of
// bytes 0-5. ID is documented as an array, not a slice, so its elements are
// ordinary indexed values with no export restriction.
func specMillisOf(i id.ID) uint64 {
	return uint64(i[0])<<40 | uint64(i[1])<<32 | uint64(i[2])<<24 |
		uint64(i[3])<<16 | uint64(i[4])<<8 | uint64(i[5])
}

func specVersionNibble(i id.ID) byte { return i[6] >> 4 }
func specVariantBits(i id.ID) byte   { return i[8] >> 6 }

func specAllDistinct(ids []id.ID) bool {
	seen := make(map[id.ID]bool, len(ids))
	for _, v := range ids {
		if seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}

func specIsStrictlyIncreasing(ids []id.ID) bool {
	for k := 1; k < len(ids); k++ {
		if bytes.Compare(ids[k-1][:], ids[k][:]) >= 0 {
			return false
		}
	}
	return true
}

// specLooksCanonical checks the 8-4-4-4-12 lowercase-hex, hyphenated shape
// the spec describes for ID.String, independent of any particular value.
func specLooksCanonical(s string) bool {
	if len(s) != 36 {
		return false
	}
	for _, pos := range []int{8, 13, 18, 23} {
		if s[pos] != '-' {
			return false
		}
	}
	hexPart := s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:36]
	if hexPart != strings.ToLower(hexPart) {
		return false
	}
	if _, err := hex.DecodeString(hexPart); err != nil {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Layout: the 128 bits are not yours to choose everywhere
// ---------------------------------------------------------------------------

func TestSpecTimestampBytesEncodeUnixMillisecondsBigEndian(t *testing.T) {
	t.Parallel()
	instant := time.Date(2026, 3, 14, 9, 26, 53, 589000000, time.UTC)
	wantMs := uint64(instant.UnixMilli())

	t.Run("via V7", func(t *testing.T) {
		t.Parallel()
		clk := newSpecControllableClock(instant)
		gen := id.NewV7(clk, specConstReader{b: 0x07})
		got := gen.NewID()
		if specMillisOf(got) != wantMs {
			t.Fatalf("bytes 0-5 decoded to %d, want %d (the instant's Unix millisecond, big-endian): the whole benefit of v7 as a primary key is that raw bytes sort by time, which only holds if this field is exactly the ms timestamp", specMillisOf(got), wantMs)
		}
	})

	t.Run("via Sequence", func(t *testing.T) {
		t.Parallel()
		seq := id.NewSequence(instant)
		got := seq.NewID()
		if specMillisOf(got) != wantMs {
			t.Fatalf("Sequence issued from a fixed instant encoded ms %d, want %d", specMillisOf(got), wantMs)
		}
	})
}

func TestSpecVersionAndVariantBitsAreFixedRegardlessOfEntropy(t *testing.T) {
	cases := []struct {
		name string
		b    byte
	}{
		{"all-zero entropy still yields version 7 and variant 10", 0x00},
		{"all-ones entropy still yields version 7 and variant 10", 0xFF},
		{"alternating-bit entropy still yields version 7 and variant 10", 0x3D},
		{"another arbitrary constant entropy still yields version 7 and variant 10", 0xA5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			clk := newSpecControllableClock(specFixedInstant)
			gen := id.NewV7(clk, specConstReader{b: c.b})
			got := gen.NewID()
			if v := specVersionNibble(got); v != 0x7 {
				t.Fatalf("version nibble was %x, want 7: an entropy source must never be allowed to overwrite the version bits, since ID.Time keys off them", v)
			}
			if variant := specVariantBits(got); variant != 0b10 {
				t.Fatalf("variant bits were %02b, want 10: RFC 9562 fixes these bits regardless of the entropy fed in", variant)
			}
			if specMillisOf(got) != uint64(specFixedInstant.UnixMilli()) {
				t.Fatalf("timestamp bytes changed with the entropy source, which they must not: only rand_a/rand_b are entropy-derived")
			}
		})
	}
}

func TestSpecEntropyIsReflectedInRandBBits(t *testing.T) {
	t.Parallel()
	clkA := newSpecControllableClock(specFixedInstant)
	clkB := newSpecControllableClock(specFixedInstant)
	idA := id.NewV7(clkA, specConstReader{b: 0x00}).NewID()
	idB := id.NewV7(clkB, specConstReader{b: 0xFF}).NewID()

	if bytes.Equal(idA[9:16], idB[9:16]) {
		t.Fatal("rand_b (bytes 9-15) was identical under two different constant entropy sources: the entropy source does not appear to reach the identifier at all")
	}
	if idA[8]&0x3F == idB[8]&0x3F {
		t.Fatal("the low six bits of byte 8 (part of rand_b) were identical under two different entropy sources")
	}
	// The fixed bits must still agree even though the entropy differs.
	if specVariantBits(idA) != specVariantBits(idB) {
		t.Fatal("variant bits differed between two IDs minted with different entropy: they must be fixed regardless of entropy")
	}
}

// ---------------------------------------------------------------------------
// Round trips
// ---------------------------------------------------------------------------

func TestSpecStringParseRoundTrip(t *testing.T) {
	t.Parallel()
	seq := id.NewSequence(specFixedInstant)
	for _, v := range specGenerateN(seq, 50) {
		got, err := id.Parse(v.String())
		if err != nil {
			t.Fatalf("Parse(%q) returned an error for a string this package itself produced: %v", v.String(), err)
		}
		if got != v {
			t.Fatalf("Parse(String(x)) = %v, want %v: an identifier must survive its own canonical text form", got, v)
		}
	}
}

func TestSpecMarshalTextUnmarshalTextRoundTrip(t *testing.T) {
	t.Parallel()
	seq := id.NewSequence(specFixedInstant)
	for _, v := range specGenerateN(seq, 20) {
		text, err := v.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText returned an error for %v: %v", v, err)
		}
		var got id.ID
		if err := got.UnmarshalText(text); err != nil {
			t.Fatalf("UnmarshalText(%q) returned an error round-tripping a value this package produced: %v", text, err)
		}
		if got != v {
			t.Fatalf("UnmarshalText(MarshalText(x)) = %v, want %v", got, v)
		}
	}
}

func TestSpecJSONRoundTrip(t *testing.T) {
	t.Parallel()
	seq := id.NewSequence(specFixedInstant)
	for _, v := range specGenerateN(seq, 10) {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("json.Marshal(%v) failed: %v", v, err)
		}
		want := "\"" + v.String() + "\""
		if string(data) != want {
			t.Fatalf("json.Marshal(%v) = %s, want %s: TextMarshaler must carry the canonical form with no adapter", v, data, want)
		}
		var got id.ID
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("json.Unmarshal(%s) failed: %v", data, err)
		}
		if got != v {
			t.Fatalf("json round trip produced %v, want %v", got, v)
		}
	}
}

// ---------------------------------------------------------------------------
// Ordering and monotonicity
// ---------------------------------------------------------------------------

func TestSpecRawByteOrderMatchesCanonicalStringOrderMatchesIssueOrder(t *testing.T) {
	t.Parallel()
	seq := id.NewSequence(specFixedInstant)
	ids := specGenerateN(seq, 200)

	t.Run("raw byte order equals issue order", func(t *testing.T) {
		t.Parallel()
		byBytes := append([]id.ID{}, ids...)
		sort.Slice(byBytes, func(i, j int) bool {
			return bytes.Compare(byBytes[i][:], byBytes[j][:]) < 0
		})
		for i := range ids {
			if byBytes[i] != ids[i] {
				t.Fatalf("sorting by raw bytes reordered position %d: raw-byte order must equal issue order, which is the entire point of a time-ordered id as a primary key", i)
			}
		}
	})

	t.Run("canonical string order equals issue order", func(t *testing.T) {
		t.Parallel()
		strs := make([]string, len(ids))
		for i, v := range ids {
			strs[i] = v.String()
		}
		sorted := append([]string{}, strs...)
		sort.Strings(sorted)
		for i := range strs {
			if sorted[i] != strs[i] {
				t.Fatalf("sorting canonical strings reordered position %d: string order must match issue order too", i)
			}
		}
	})
}

func TestSpecMonotonicWithinSameMillisecond(t *testing.T) {
	t.Parallel()
	clk := newSpecControllableClock(specFixedInstant)
	gen := id.NewV7(clk, specConstReader{b: 0x11})
	ids := specGenerateN(gen, 500)

	if !specAllDistinct(ids) {
		t.Fatal("500 identifiers minted in the same millisecond were not all distinct")
	}
	if !specIsStrictlyIncreasing(ids) {
		t.Fatal("identifiers minted in the same millisecond did not sort in issue order: the 12-bit counter must keep them ordered")
	}
	base := specMillisOf(ids[0])
	for i, v := range ids {
		if specMillisOf(v) != base {
			t.Fatalf("identifier %d carried a different millisecond even though the clock never advanced and fewer than 4096 identifiers were issued", i)
		}
	}
}

func TestSpecCounterBorrowsAfterExhaustingMillisecond(t *testing.T) {
	t.Parallel()
	clk := newSpecControllableClock(specFixedInstant)
	gen := id.NewV7(clk, specConstReader{b: 0x22})
	const total = 4200
	ids := specGenerateN(gen, total)

	if !specAllDistinct(ids) {
		t.Fatal("identifiers spanning a counter overflow were not all distinct")
	}
	if !specIsStrictlyIncreasing(ids) {
		t.Fatal("identifiers spanning a counter overflow did not stay in issue order")
	}

	base := specMillisOf(ids[0])
	if specMillisOf(ids[4095]) != base {
		t.Fatalf("the 4096th identifier (index 4095) already carried a different millisecond than the first: the spec states 4,096 identifiers fit in one millisecond")
	}
	if specMillisOf(ids[4096]) <= base {
		t.Fatalf("the 4097th identifier (index 4096) did not borrow into a later millisecond after the counter's 4,096 values were exhausted")
	}
}

func TestSpecBackwardClockStepNeverReissuesAUsedMillisecond(t *testing.T) {
	t.Parallel()
	clk := newSpecControllableClock(specFixedInstant)
	gen := id.NewV7(clk, specConstReader{b: 0x33})

	before := specGenerateN(gen, 5)
	clk.set(specFixedInstant.Add(-50 * time.Millisecond))
	after := specGenerateN(gen, 5)

	all := append(append([]id.ID{}, before...), after...)
	if !specIsStrictlyIncreasing(all) {
		t.Fatal("identifiers minted after a backward clock step sorted before identifiers minted earlier: a clock correction must not undo issue order")
	}
	lastBefore := specMillisOf(before[len(before)-1])
	for i, v := range after {
		if specMillisOf(v) < lastBefore {
			t.Fatalf("identifier %d after the backward clock step carried millisecond %d, earlier than the high-water mark %d: a millisecond already used must not be reissued", i, specMillisOf(v), lastBefore)
		}
	}
}

// ---------------------------------------------------------------------------
// Observed time
// ---------------------------------------------------------------------------

func TestSpecTimeRoundTripsStampedInstantForV7(t *testing.T) {
	t.Parallel()
	instants := []time.Time{
		time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC),
		time.Date(2026, 9, 6, 12, 30, 0, 0, time.UTC),
		time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	for _, instant := range instants {
		clk := newSpecControllableClock(instant)
		gen := id.NewV7(clk, specConstReader{b: 0x55})
		got := gen.NewID()

		gotTime, ok := got.Time()
		if !ok {
			t.Fatalf("Time() reported ok=false for a version-7 identifier minted at %v", instant)
		}
		if !gotTime.Equal(instant) {
			t.Fatalf("Time() = %v, want %v (both ms-aligned): a v7 identifier's Time must round-trip the instant it was stamped with", gotTime, instant)
		}
	}
}

func TestSpecTimeTruncatesSubMillisecondPrecision(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ns   int
	}{
		{"nanoseconds just under a millisecond boundary are truncated away", 999999999},
		{"nanoseconds mid-millisecond are truncated away", 1500000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			instant := time.Date(2030, 1, 1, 0, 0, 0, c.ns, time.UTC)
			want := time.UnixMilli(instant.UnixMilli())

			clk := newSpecControllableClock(instant)
			gen := id.NewV7(clk, specConstReader{b: 0x66})
			got := gen.NewID()

			gotTime, ok := got.Time()
			if !ok {
				t.Fatal("Time() reported ok=false for a version-7 identifier")
			}
			if !gotTime.Equal(want) {
				t.Fatalf("Time() = %v, want %v (truncated to the millisecond)", gotTime, want)
			}
			if gotTime.Equal(instant) {
				t.Fatalf("Time() reported the full sub-millisecond instant %v: the format only carries whole milliseconds, so precision finer than that cannot survive", instant)
			}
		})
	}
}

func TestSpecTimeReportsFalseForNonVersion7(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T) id.ID
	}{
		{
			name:  "the nil identifier, which reads as version 0",
			build: func(t *testing.T) id.ID { return id.Nil },
		},
		{
			name: "a well-formed version 1 uuid",
			build: func(t *testing.T) id.ID {
				got, err := id.Parse("11111111-1111-1111-8111-111111111111")
				if err != nil {
					t.Fatalf("setup: could not parse a well-formed v1 uuid: %v", err)
				}
				return got
			},
		},
		{
			name: "a well-formed version 4 uuid",
			build: func(t *testing.T) id.ID {
				got, err := id.Parse("11111111-1111-4111-8111-111111111111")
				if err != nil {
					t.Fatalf("setup: could not parse a well-formed v4 uuid: %v", err)
				}
				return got
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			v := c.build(t)
			if _, ok := v.Time(); ok {
				t.Fatalf("Time() reported ok=true for %s: only version 7 carries a meaningful timestamp", c.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

func TestSpecParseRejectsMalformedInput(t *testing.T) {
	base := "01234567-89ab-7def-8123-456789abcdef"
	hex32 := "0123456789ab7def8123456789abcdef"
	wrongHyphens := hex32[0:7] + "-" + hex32[7:13] + "-" + hex32[13:17] + "-" + hex32[17:21] + "-" + hex32[21:32]

	cases := []struct {
		name  string
		input string
	}{
		{"empty string is not an identifier", ""},
		{"too short to be a canonical uuid", "01234567-89ab-7def-8123"},
		{"too long to be a canonical uuid", base + "0"},
		{"hyphens removed entirely", strings.ReplaceAll(base, "-", "")},
		{"hyphens at the wrong offsets", wrongHyphens},
		{"a non-hex character in place of a hex digit", "0123456g-89ab-7def-8123-456789abcdef"},
		{"the nil uuid, well-formed but refused", "00000000-0000-0000-0000-000000000000"},
		{"surrounding whitespace is not part of the canonical form", " " + base + " "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if _, err := id.Parse(c.input); err == nil {
				t.Fatalf("Parse(%q) succeeded; the spec requires this input to be rejected", c.input)
			}
		})
	}
}

func TestSpecParseRefusesNilUUIDSpecifically(t *testing.T) {
	t.Parallel()
	if _, err := id.Parse("00000000-0000-0000-0000-000000000000"); err == nil {
		t.Fatal("Parse accepted the all-zero uuid: an unset field must not be able to arrive at a repository wearing a real identifier's clothes")
	}
	// Contrast: a value that is merely close to nil, but not nil, must parse.
	got, err := id.Parse("00000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatalf("Parse rejected a non-nil, well-formed uuid adjacent to nil: %v", err)
	}
	if got.IsZero() {
		t.Fatal("a non-nil parsed value reported IsZero() == true")
	}
}

func TestSpecParseNormalizesCaseRegardlessOfInputCase(t *testing.T) {
	t.Parallel()
	lower := "01234567-89ab-7def-8123-456789abcdef"
	upper := strings.ToUpper(lower)
	mixed := "01234567-89AB-7dEf-8123-456789ABCDEF"

	var values []id.ID
	for _, in := range []string{lower, upper, mixed} {
		got, err := id.Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q) failed: %v", in, err)
		}
		if got.String() != lower {
			t.Fatalf("Parse(%q).String() = %q, want %q: Parse followed by String must return lowercase regardless of input case", in, got.String(), lower)
		}
		values = append(values, got)
	}
	for i := 1; i < len(values); i++ {
		if values[i] != values[0] {
			t.Fatal("the same uuid parsed in different cases produced unequal ID values: identifiers must be compared as ID values, never as strings")
		}
	}
}

func TestSpecParseAcceptsNonV7VersionsWithoutJudging(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"a well-formed version 1 uuid parses", "11111111-1111-1111-8111-111111111111"},
		{"a well-formed version 4 uuid parses", "11111111-1111-4111-8111-111111111111"},
		{"a well-formed version 5 uuid parses", "11111111-1111-5111-8111-111111111111"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if _, err := id.Parse(c.input); err != nil {
				t.Fatalf("Parse(%q) failed: %v; Parse validates format and non-nil-ness, not version", c.input, err)
			}
		})
	}
}

func TestSpecVersionExtractsFourBitsForEveryNibbleValue(t *testing.T) {
	const hexDigits = "0123456789abcdef"
	for v := 0; v <= 15; v++ {
		v := v
		t.Run("version nibble round-trips through Version()", func(t *testing.T) {
			t.Parallel()
			versionChar := string(hexDigits[v])
			s := "00000000-0000-" + versionChar + "000-8000-000000000001"
			got, err := id.Parse(s)
			if err != nil {
				t.Fatalf("Parse(%q) failed: %v", s, err)
			}
			if got.Version() != v {
				t.Fatalf("Version() = %d, want %d for nibble %s: Version must read the four bits present, whatever they are", got.Version(), v, versionChar)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// String / Marshal / Unmarshal
// ---------------------------------------------------------------------------

func TestSpecStringProducesCanonicalLowercaseHyphenatedForm(t *testing.T) {
	t.Parallel()
	seq := id.NewSequence(specFixedInstant)
	values := append([]id.ID{id.Nil}, specGenerateN(seq, 5)...)
	for _, v := range values {
		if s := v.String(); !specLooksCanonical(s) {
			t.Fatalf("String() = %q is not 8-4-4-4-12 lowercase hex with hyphens at 8,13,18,23", s)
		}
	}
}

func TestSpecMarshalTextMatchesString(t *testing.T) {
	t.Parallel()
	seq := id.NewSequence(specFixedInstant)
	values := append([]id.ID{id.Nil}, specGenerateN(seq, 3)...)
	for _, v := range values {
		b, err := v.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText() returned an error for %v: %v", v, err)
		}
		if string(b) != v.String() {
			t.Fatalf("MarshalText() = %q, want %q (String())", b, v.String())
		}
	}
}

func TestSpecUnmarshalTextLeavesReceiverUntouchedOnBadInput(t *testing.T) {
	t.Parallel()
	seq := id.NewSequence(specFixedInstant)
	original := seq.NewID()
	target := original

	if err := target.UnmarshalText([]byte("not-a-valid-identifier-at-all")); err == nil {
		t.Fatal("UnmarshalText accepted malformed input")
	}
	if target != original {
		t.Fatal("UnmarshalText mutated the receiver despite the input failing to parse: a partially decoded identifier is worse than none because it is well-formed enough to be stored")
	}
}

// ---------------------------------------------------------------------------
// Nil, IsZero, Version, comparability
// ---------------------------------------------------------------------------

func TestSpecNilIsZeroValueAndReadsAsVersionZero(t *testing.T) {
	t.Parallel()
	var zero id.ID
	if zero != id.Nil {
		t.Fatal("the zero value of ID is not equal to id.Nil")
	}
	if !id.Nil.IsZero() {
		t.Fatal("id.Nil.IsZero() == false")
	}
	if id.Nil.Version() != 0 {
		t.Fatalf("id.Nil.Version() = %d, want 0: nil reads as version 0 so an unset identifier never passes for a real one", id.Nil.Version())
	}
	want := "00000000-0000-0000-0000-000000000000"
	if id.Nil.String() != want {
		t.Fatalf("id.Nil.String() = %q, want %q", id.Nil.String(), want)
	}
}

func TestSpecIsZeroTrueOnlyForNilValue(t *testing.T) {
	t.Parallel()
	seq := id.NewSequence(specFixedInstant)
	nonNil := seq.NewID()

	if id.Nil.IsZero() != true {
		t.Fatal("id.Nil.IsZero() must be true")
	}
	if nonNil.IsZero() != false {
		t.Fatal("a minted, non-nil identifier reported IsZero() == true")
	}
	if (id.ID{}).IsZero() != true {
		t.Fatal("the ID zero value literal must report IsZero() == true")
	}
}

func TestSpecIDComparableAndValidMapKey(t *testing.T) {
	t.Parallel()
	seq := id.NewSequence(specFixedInstant)
	a := seq.NewID()
	b := seq.NewID()
	if a == b {
		t.Fatal("two consecutive Sequence identifiers compared equal")
	}

	reparsedA, err := id.Parse(a.String())
	if err != nil {
		t.Fatalf("Parse(a.String()) failed: %v", err)
	}
	if reparsedA != a {
		t.Fatal("a value parsed back from its own String() did not compare == to the original")
	}

	m := map[id.ID]int{}
	m[a] = 1
	m[b] = 2
	m[reparsedA] = 3
	if len(m) != 2 {
		t.Fatalf("map has %d entries for 2 distinct identifiers: equal IDs did not collapse to the same map key", len(m))
	}
	if m[a] != 3 {
		t.Fatal("writing through an equal-but-differently-constructed key did not update the same map entry")
	}
}

// ---------------------------------------------------------------------------
// Sequence
// ---------------------------------------------------------------------------

func TestSpecSequenceDeterministicAcrossInstances(t *testing.T) {
	t.Parallel()
	s1 := id.NewSequence(specFixedInstant)
	s2 := id.NewSequence(specFixedInstant)
	for i := 0; i < 25; i++ {
		a := s1.NewID()
		b := s2.NewID()
		if a != b {
			t.Fatalf("two Sequence generators built from the same instant diverged at position %d (%v vs %v): fixtures built from Sequence would not be reproducible across test runs", i, a, b)
		}
	}
}

func TestSpecSequenceOrderedAndWellFormedV7(t *testing.T) {
	t.Parallel()
	seq := id.NewSequence(specFixedInstant)
	ids := specGenerateN(seq, 3000)

	if !specAllDistinct(ids) {
		t.Fatal("Sequence produced duplicate identifiers")
	}
	if !specIsStrictlyIncreasing(ids) {
		t.Fatal("Sequence identifiers did not sort in issue order")
	}
	for i, v := range ids {
		if specVersionNibble(v) != 0x7 {
			t.Fatalf("identifier %d is not version 7", i)
		}
		if specVariantBits(v) != 0b10 {
			t.Fatalf("identifier %d does not carry variant bits 10", i)
		}
	}
}

func TestSpecSequenceSafeForConcurrentUse(t *testing.T) {
	t.Parallel()
	seq := id.NewSequence(specFixedInstant)
	const goroutines = 20
	const perGoroutine = 200

	var mu sync.Mutex
	var wg sync.WaitGroup
	all := make([]id.ID, 0, goroutines*perGoroutine)
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			local := make([]id.ID, perGoroutine)
			for i := range local {
				local[i] = seq.NewID()
			}
			mu.Lock()
			all = append(all, local...)
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(all) != goroutines*perGoroutine {
		t.Fatalf("collected %d identifiers, want %d", len(all), goroutines*perGoroutine)
	}
	if !specAllDistinct(all) {
		t.Fatal("concurrent callers of Sequence.NewID received duplicate identifiers: the spec says Sequence is safe for concurrent use")
	}
}

// ---------------------------------------------------------------------------
// V7: entropy contract and panic-on-failure
// ---------------------------------------------------------------------------

func TestSpecNewIDPanicsWhenEntropySourceFails(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewID did not panic when its entropy source failed: NewID has no error return, so a failing entropy source must panic rather than mint a partially-random identifier")
		}
	}()
	clk := newSpecControllableClock(specFixedInstant)
	gen := id.NewV7(clk, specFailingReader{})
	_ = gen.NewID()
}

func TestSpecEntropyReadDoesNotHoldGeneratorLock(t *testing.T) {
	t.Parallel()
	clk := newSpecControllableClock(specFixedInstant)
	reader := newSpecRendezvousReader()
	gen := id.NewV7(clk, reader)

	results := make(chan any, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			defer func() {
				results <- recover()
			}()
			gen.NewID()
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("two concurrent NewID calls did not both complete: V7 appears to serialize entropy reads behind its lock, which the spec says it must not do (the entropy source must be safe for concurrent use precisely because it is read without the lock held)")
	}
	close(results)
	for r := range results {
		if r != nil {
			t.Fatalf("a concurrent NewID call panicked (%v): consistent with the entropy source being starved by a lock held across the read", r)
		}
	}
}

// ---------------------------------------------------------------------------
// Inference tests
//
// The specification leaves two things unsettled. Each test below is marked
// INFERENCE at the point of use; a failure here is a question to resolve
// against the author, not an unambiguous defect.
// ---------------------------------------------------------------------------

// INFERENCE: the spec says Parse "returns an Invalid error naming the
// input, which is the caller's own and therefore safe to echo," but no
// Invalid type is exported. This test assumes "naming the input" means the
// offending string literally appears in Error(). If the message instead
// paraphrases or redacts it, this expectation — not necessarily the
// implementation — is wrong.
func TestSpecParseErrorMessageNamesOffendingInputINFERENCE(t *testing.T) {
	t.Parallel()
	bad := "totally-not-a-uuid"
	_, err := id.Parse(bad)
	if err == nil {
		t.Fatal("Parse accepted a clearly malformed string")
	}
	if !strings.Contains(err.Error(), bad) {
		t.Fatalf("INFERENCE: Parse's error %q does not contain the offending input %q; the spec says the error names the input", err.Error(), bad)
	}
}

// INFERENCE: the spec states Parse refuses the nil uuid, in the section
// about Parse specifically. It does not explicitly say UnmarshalText shares
// that refusal, only that UnmarshalText leaves the receiver untouched on
// failure. If UnmarshalText does not delegate to Parse's nil check, this
// test's expectation does not hold.
func TestSpecUnmarshalTextRefusesNilUUIDTextINFERENCE(t *testing.T) {
	t.Parallel()
	seq := id.NewSequence(specFixedInstant)
	target := seq.NewID()
	err := target.UnmarshalText([]byte("00000000-0000-0000-0000-000000000000"))
	if err == nil {
		t.Fatal("INFERENCE: UnmarshalText accepted the nil uuid's text form; the spec says Parse refuses nil but does not explicitly extend that to UnmarshalText")
	}
}

// INFERENCE: NewV7's signature returns only *V7, with no error, yet the
// spec says it "refuses a nil clock or a nil entropy source." A panic is
// the only construction-time refusal mechanism available without an error
// return; if the implementation instead returns a zero-valued or nil *V7
// that fails later, this test's expectation does not hold.
func TestSpecNewV7RefusesNilClockINFERENCE(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("INFERENCE: NewV7(nil, entropy) did not panic; the spec says NewV7 refuses a nil clock at construction, and with no error return, panic is the only construction-time refusal available")
		}
	}()
	_ = id.NewV7(nil, specConstReader{b: 0x00})
}

// INFERENCE: see TestSpecNewV7RefusesNilClockINFERENCE.
func TestSpecNewV7RefusesNilEntropyINFERENCE(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("INFERENCE: NewV7(clock, nil) did not panic; the spec says NewV7 refuses a nil entropy source at construction, and with no error return, panic is the only construction-time refusal available")
		}
	}()
	_ = id.NewV7(newSpecControllableClock(specFixedInstant), nil)
}
