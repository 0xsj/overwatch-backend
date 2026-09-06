package provenance_test

// Author-written, after the mutation round of 2026-09-06. Each test here closes
// a REAL GAP — a rule the specification already stated that no barrier-derived
// test asserted, so a mutation removing it survived. The barrier-derived suites
// are spec_test.go and spec_identity_test.go; this file is not one of them and
// the filename is what carries that distinction.

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

func minter(t *testing.T) provenance.Minter {
	t.Helper()
	return id.NewSequence(time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC))
}

// M22 survived: FieldDepth appeared only in tests naming the constant, never in
// a test that read an emitted record. Depth is the cycle bound — a line that
// omits it cannot answer the question the field exists for.
func TestAttrsAlwaysEmitDepthAndAttemptIncludingAtTheRoot(t *testing.T) {
	root := provenance.New(provenance.OriginStartup, minter(t))
	for _, want := range []string{provenance.FieldDepth, provenance.FieldAttempt} {
		found := false
		for _, a := range root.Attrs() {
			if a.Key == want {
				found = true
				if a.Value.Kind() != slog.KindUint64 {
					t.Errorf("%s is a %s; depth and attempt are counts and a string cannot be compared or bounded in a query", want, a.Value.Kind())
				}
			}
		}
		if !found {
			t.Errorf("a root record omits %s: 0 and 1 are real values here, not absences, and the bound is unreadable without them", want)
		}
	}
}

// M24 survived: nothing asserted the round trip, so ParseOrigin could refuse the
// one name String is guaranteed to produce for the zero value.
func TestParseOriginRoundTripsEveryNameStringProduces(t *testing.T) {
	all := append([]provenance.Origin{provenance.OriginUnknown}, provenance.Origins...)
	for _, o := range all {
		got, ok := provenance.ParseOrigin(o.String())
		if !ok {
			t.Errorf("ParseOrigin(%q) refused a name String itself produced: a value that renders into a log must read back out of one", o.String())
			continue
		}
		if got != o {
			t.Errorf("ParseOrigin(%q) = %v, want %v", o.String(), got, o)
		}
	}
	if _, ok := provenance.ParseOrigin("nonsense"); ok {
		t.Error("ParseOrigin accepted a name no Origin renders")
	}
}

// M29 survived: the suites probed only the bytes the document names as
// rejected. A charset is closed in both directions, and admitting one extra byte
// is the shape an injection actually takes.
func TestValidIDRejectsEveryByteOutsideTheClosedSet(t *testing.T) {
	permitted := func(c byte) bool {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			return true
		}
		return strings.IndexByte("._:/-", c) >= 0
	}
	for c := 0; c < 256; c++ {
		b := byte(c)
		_, err := provenance.Service(string([]byte{b}))
		if permitted(b) && err != nil {
			t.Errorf("byte %q is in the documented set and was rejected: %v", b, err)
		}
		if !permitted(b) && err == nil {
			t.Errorf("byte %q is outside the documented set and was accepted; the charset is the log-injection surface and it is closed in both directions", b)
		}
	}
	long := strings.Repeat("a", provenance.MaxIDLength+1)
	if _, err := provenance.Service(long); err == nil {
		t.Errorf("an id of %d characters was accepted; MaxIDLength is %d", len(long), provenance.MaxIDLength)
	}
}

// M41 survived, and it is the sharpest of the four: doc.go says outright that
// attempt is never 0, and no test read a record back to check it.
func TestUnmarshalRefusesARecordThatViolatesAStatedInvariant(t *testing.T) {
	good := provenance.New(provenance.OriginRequest, minter(t))
	raw, err := json.Marshal(good)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		from string
		to   string
		why  string
	}{
		{"attempt 0", `"attempt":1`, `"attempt":0`,
			"attempt is documented as 1 at the origin and never 0; reading one back as valid launders a broken record into a trusted one"},
		{"depth past MaxDepth", `"depth":0`, `"depth":250`,
			"a depth past MaxDepth has escaped the cycle bound, and accepting it on the way back removes the only control that detects a cycle"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := strings.Replace(string(raw), tc.from, tc.to, 1)
			if mutated == string(raw) {
				t.Fatalf("the record does not contain %s; this test no longer probes anything: %s", tc.from, raw)
			}
			var back provenance.Provenance
			err := json.Unmarshal([]byte(mutated), &back)
			if err == nil {
				t.Fatalf("accepted %s: %s", tc.name, tc.why)
			}
			if !errors.IsKind(err, errors.Internal) {
				t.Errorf("reported %v; these are our own bytes, so a violation is corruption rather than input", errors.KindOf(err))
			}
		})
	}
}

// Found by reading, not by a test: UnmarshalJSON accepted `"origin":"unknown"`
// because ParseOrigin accepts every name String produces, then MarshalJSON
// omitted the key because the value was OriginUnknown. Absent and explicitly
// unknown collapsed, and the value vanished mid-round-trip.
func TestUnmarshalRefusesAnOriginNewCouldNotHaveProduced(t *testing.T) {
	good := provenance.New(provenance.OriginRequest, minter(t))
	raw, err := json.Marshal(good)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ name, to string }{
		{"the zero origin, spelled out", `"origin":"unknown"`},
		{"a name no Origin renders", `"origin":"whatever"`},
		{"absent entirely", ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mutated := strings.Replace(string(raw), `"origin":"request"`, tc.to, 1)
			mutated = strings.Replace(mutated, `,,`, `,`, 1)
			var back provenance.Provenance
			if err := json.Unmarshal([]byte(mutated), &back); err == nil {
				out, _ := json.Marshal(back)
				t.Fatalf("accepted %s and round-tripped it to %s: New refuses this origin, so no record this package wrote can carry it, and storing one collapses absent with explicitly unknown", tc.name, out)
			} else if !errors.IsKind(err, errors.Internal) {
				t.Errorf("reported %v; these are our own bytes", errors.KindOf(err))
			}
		})
	}
}

// The zero receiver must panic whatever else is wrong with the call. It used to
// depend on the argument: DeriveFrom(m, id.Nil) returned an error while
// DeriveFrom(m, real) panicked, so the contract varied by the thing it was not
// about.
func TestAZeroReceiverPanicsRegardlessOfTheArguments(t *testing.T) {
	var zero provenance.Provenance
	m := minter(t)
	for _, tc := range []struct {
		name string
		call func()
	}{
		{"DeriveFrom with a zero cause", func() { zero.DeriveFrom(m, id.Nil) }},
		{"DeriveFrom with a real cause", func() { zero.DeriveFrom(m, m.NewID()) }},
		{"Derive", func() { zero.Derive(m) }},
		{"Retry", func() { zero.Retry() }},
		{"Adopt", func() { zero.Adopt(provenance.Adopted{}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("returned instead of panicking: a zero Provenance is a value no constructor produces, so reaching one is a programming error and must not vary with the arguments")
				}
			}()
			tc.call()
		})
	}
}
