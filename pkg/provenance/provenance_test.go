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
