// Author-written, from decisions/0035's Verification block, which was written
// before the code. `Extract` is a pure function of bytes and mappings, which is
// what lets the whole field-accounting be tested against literal output.
package domain_test

import (
	"testing"

	"github.com/0xsj/overwatch-backend/internal/observation/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func nonZero(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}

func mapping(t *testing.T, n byte, field, expression string) domain.Mapping {
	t.Helper()
	p, err := domain.ParsePath(expression)
	if err != nil {
		t.Fatalf("%q: %v", expression, err)
	}
	return domain.Mapping{ID: nonZero(n), Field: field, Version: 1, Path: p}
}

// A real httpx line, and the shape the whole design is built around.
const httpxLine = `{"url":"https://acme.test/","host":"acme.test","port":443,` +
	`"status_code":200,"webserver":"nginx/1.24","title":"Acme","tls":{"issuer":"R3"},` +
	`"tech":["nginx","php"],"failed":false}`

func extract(t *testing.T, body string, ms []domain.Mapping) domain.Extraction {
	t.Helper()
	got, err := domain.Extract([]byte(body), ms, "url", "url")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// "six mapped fields over one record produce SIX observations" — 0035.
func TestOneRecordProducesOneReadingPerMappedField(t *testing.T) {
	got := extract(t, httpxLine, []domain.Mapping{
		mapping(t, 1, "url", ".url"),
		mapping(t, 2, "webserver", ".webserver"),
		mapping(t, 3, "status", ".status_code"),
		mapping(t, 4, "title", ".title"),
		mapping(t, 5, "issuer", ".tls.issuer"),
		mapping(t, 6, "tech", ".tech[]"),
	})
	if len(got.Records) != 1 {
		t.Fatalf("one line is one record, got %d", len(got.Records))
	}
	rec := got.Records[0]
	if rec.SubjectValue != "https://acme.test/" || rec.SubjectKind != "url" {
		t.Fatalf("subject came from the produces-kind mapping: %+v", rec)
	}
	// Five scalar fields plus TWO from `.tech[]` — a flatten is one reading per
	// element, which is the point of the `[]`.
	if len(rec.Readings) != 7 {
		for _, r := range rec.Readings {
			t.Logf("  %s = %q", r.Mapping.Field, r.Value)
		}
		t.Fatalf("want 7 readings, got %d", len(rec.Readings))
	}
}

func TestANestedPathAndAFlattenBothRead(t *testing.T) {
	got := extract(t, httpxLine, []domain.Mapping{
		mapping(t, 1, "url", ".url"),
		mapping(t, 2, "issuer", ".tls.issuer"),
		mapping(t, 3, "tech", ".tech[]"),
	})
	values := map[string][]string{}
	for _, r := range got.Records[0].Readings {
		values[r.Mapping.Field] = append(values[r.Mapping.Field], r.Value)
	}
	if len(values["issuer"]) != 1 || values["issuer"][0] != "R3" {
		t.Errorf(".tls.issuer: %v", values["issuer"])
	}
	if len(values["tech"]) != 2 {
		t.Errorf(".tech[] over two elements: %v", values["tech"])
	}
}

// "a path with no mapping produces an `unmapped` row and NO observation", and
// FIELDS SEEN = MAPPED + LEFT ALONE.
func TestEveryUnclaimedPathIsRecordedAndNeverGuessed(t *testing.T) {
	got := extract(t, httpxLine, []domain.Mapping{
		mapping(t, 1, "url", ".url"),
		mapping(t, 2, "webserver", ".webserver"),
	})
	if len(got.Seen) != len(got.Mapped)+len(got.LeftAlone) {
		t.Fatalf("FIELDS SEEN %d != MAPPED %d + LEFT ALONE %d",
			len(got.Seen), len(got.Mapped), len(got.LeftAlone))
	}
	left := map[string]domain.Unclaimed{}
	for _, u := range got.LeftAlone {
		left[u.Path] = u
	}
	for _, want := range []string{".host", ".port", ".status_code", ".title", ".tls.issuer", ".tech[]", ".failed"} {
		if _, ok := left[want]; !ok {
			t.Errorf("%s was emitted and is neither mapped nor recorded as left alone", want)
		}
	}
	// And NONE of them became a reading. This is the rule the whole product
	// rests on: a field nobody mapped is not guessed at.
	for _, r := range got.Records[0].Readings {
		if r.Mapping.Field != "url" && r.Mapping.Field != "webserver" {
			t.Errorf("an unmapped field was guessed into an observation: %q", r.Mapping.Field)
		}
	}
	// The sample is kept so a person can decide without opening the artifact.
	if left[".title"].Sample != "Acme" {
		t.Errorf("want a sample value, got %q", left[".title"].Sample)
	}
}

// A number is rendered without a decimal point: `443.000000` in a port field is
// the record lying about what the source said.
func TestAnIntegerIsNotRenderedAsAFloat(t *testing.T) {
	got := extract(t, httpxLine, []domain.Mapping{
		mapping(t, 1, "url", ".url"),
		mapping(t, 2, "port", ".port"),
	})
	for _, r := range got.Records[0].Readings {
		if r.Mapping.Field == "port" && r.Value != "443" {
			t.Fatalf("want 443, got %q", r.Value)
		}
	}
}

// "a mapping naming a path the record lacks produces neither" — it is the tool
// not having said that, which is different from the mapping being wrong.
func TestAMappingThatMatchesNothingIsSilent(t *testing.T) {
	got := extract(t, httpxLine, []domain.Mapping{
		mapping(t, 1, "url", ".url"),
		mapping(t, 2, "cname", ".dns.cname"),
	})
	for _, r := range got.Records[0].Readings {
		if r.Mapping.Field == "cname" {
			t.Fatal("a path the record lacks read something")
		}
	}
	for _, u := range got.LeftAlone {
		if u.Path == ".dns.cname" {
			t.Fatal("a path the record never emitted is not an unmapped field")
		}
	}
}

// JSONL is what every tool here emits with -json.
func TestJSONLIsThreeRecordsAndABannerIsSkipped(t *testing.T) {
	body := "subfinder v2.6 starting\n" +
		`{"host":"a.acme.test"}` + "\n\n" +
		`{"host":"b.acme.test"}` + "\n" +
		`{"host":"c.acme.test"}` + "\n"
	got, err := domain.Extract([]byte(body),
		[]domain.Mapping{mapping(t, 1, "host", ".host")}, "host", "host")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Records) != 3 {
		t.Fatalf("a non-JSON banner must not lose the findings behind it: %d records", len(got.Records))
	}
	if got.Records[2].SubjectValue != "c.acme.test" {
		t.Fatalf("third record: %+v", got.Records[2])
	}
}

func TestATopLevelArrayIsItsElements(t *testing.T) {
	got, err := domain.Extract([]byte(`[{"host":"a.acme.test"},{"host":"b.acme.test"}]`),
		[]domain.Mapping{mapping(t, 1, "host", ".host")}, "host", "host")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Records) != 2 {
		t.Fatalf("want 2, got %d", len(got.Records))
	}
}

// A record with no subject yields NO readings — a value with no subject is a
// statement about nothing — while its paths are still COUNTED, because the tool
// did say them.
func TestARecordWithNoSubjectYieldsNothingButIsStillCounted(t *testing.T) {
	got, err := domain.Extract([]byte(`{"error":"timeout","input":"acme.test"}`),
		[]domain.Mapping{mapping(t, 1, "host", ".host")}, "host", "host")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Records) != 0 {
		t.Fatalf("no subject, so no readings: %+v", got.Records)
	}
	if len(got.Seen) != 2 {
		t.Fatalf("the tool said two things and both are counted: %v", got.Seen)
	}
	if len(got.LeftAlone) != 2 {
		t.Fatalf("neither was mapped: %+v", got.LeftAlone)
	}
}

// 0035 §2: the subject is the mapping whose FIELD IS THE PRODUCES KIND — not
// the first mapping, not the first path that reads. Every other test here
// happens to list the subject mapping first, so a mutant taking `mappings[0]`
// survived all of them; this is the one that kills it.
func TestTheSubjectComesFromTheProducesKindMappingAndNotTheFirstOne(t *testing.T) {
	got, err := domain.Extract([]byte(httpxLine), []domain.Mapping{
		// Deliberately first, and deliberately reading a DIFFERENT value.
		mapping(t, 1, "webserver", ".webserver"),
		mapping(t, 2, "host", ".host"),
		mapping(t, 3, "url", ".url"),
	}, "url", "url")
	if err != nil {
		t.Fatal(err)
	}
	if got.Records[0].SubjectValue != "https://acme.test/" {
		t.Fatalf("the subject must come from the `url` mapping wherever it sits: %q",
			got.Records[0].SubjectValue)
	}
	if got.Records[0].SubjectKind != "url" {
		t.Fatalf("subject kind: %q", got.Records[0].SubjectKind)
	}
}

// And a record that carries every other field but NOT the subject's path yields
// nothing, even though other mappings would have read plenty.
func TestNoSubjectPathMeansNoReadingsEvenWhenOtherFieldsRead(t *testing.T) {
	got, err := domain.Extract([]byte(`{"host":"acme.test","webserver":"nginx"}`),
		[]domain.Mapping{
			mapping(t, 1, "webserver", ".webserver"),
			mapping(t, 2, "url", ".url"),
		}, "url", "url")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Records) != 0 {
		t.Fatalf("a value with no subject is a statement about nothing: %+v", got.Records)
	}
}

// "a tool with no mapping for its own produces-kind is refused, with a message"
// — the one place extraction refuses rather than records.
func TestAToolWithNoSubjectMappingIsRefused(t *testing.T) {
	_, err := domain.Extract([]byte(httpxLine),
		[]domain.Mapping{mapping(t, 1, "webserver", ".webserver")}, "url", "url")
	if !errors.Is(err, domain.ErrNoSubjectMapping) {
		t.Fatalf("want ErrNoSubjectMapping, got %v", err)
	}
}

// An object or an array is a BRANCH, not a value. Stringifying one would put a
// JSON blob in a field a person is going to read as a server header.
func TestABranchIsNotAValue(t *testing.T) {
	got := extract(t, httpxLine, []domain.Mapping{
		mapping(t, 1, "url", ".url"),
		mapping(t, 2, "tls", ".tls"),
		mapping(t, 3, "techlist", ".tech"),
	})
	for _, r := range got.Records[0].Readings {
		if r.Mapping.Field == "tls" || r.Mapping.Field == "techlist" {
			t.Errorf("%s read a branch as a value: %q", r.Mapping.Field, r.Value)
		}
	}
}

// A JSON null is the source DECLINING to say, which is not a value.
func TestANullIsNotAValue(t *testing.T) {
	got, err := domain.Extract([]byte(`{"host":"a.acme.test","title":null}`),
		[]domain.Mapping{mapping(t, 1, "host", ".host"), mapping(t, 2, "title", ".title")},
		"host", "host")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range got.Records[0].Readings {
		if r.Mapping.Field == "title" {
			t.Fatalf("a null became a value: %q", r.Value)
		}
	}
	for _, path := range got.Seen {
		if path == ".title" {
			t.Fatal("a null is not a field the source said")
		}
	}
}

func TestPathParsing(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{".host", ".host"},
		{"host", ".host"},
		{".a.b", ".a.b"},
		{".a[].b", ".a[].b"},
		{"  .a[]  ", ".a[]"},
	} {
		got, err := domain.ParsePath(tc.in)
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if got.String() != tc.want {
			t.Errorf("%q parsed to %q, want %q", tc.in, got.String(), tc.want)
		}
	}
	for _, bad := range []string{"", ".", "  ", ".a..b", ".a[][]", ".a[b]", ".[]"} {
		if _, err := domain.ParsePath(bad); err == nil {
			t.Errorf("%q parsed and should not have", bad)
		}
	}
}

// An array is ONE path with `[]`, not one per index: `.a[0].b` and `.a[1].b` are
// the same field said twice, and a mapping names the shape rather than the
// position.
func TestLeavesCollapseArrayIndexes(t *testing.T) {
	leaves := domain.Leaves(map[string]any{
		"tech": []any{"nginx", "php", "redis"},
	})
	if len(leaves) != 1 || leaves[0] != ".tech[]" {
		t.Fatalf("want one .tech[], got %v", leaves)
	}
}
