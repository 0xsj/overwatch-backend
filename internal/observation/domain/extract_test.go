// Author-written, from decisions/0035's Verification block, which was written
// before the code. `Extract` is a pure function of bytes and mappings, which is
// what lets the whole field-accounting be tested against literal output.
package domain_test

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/observation/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var at = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

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

// subject is the mapping that says what a record is ABOUT — decisions/0040.
//
// It used to be spelled by NAMING the field after the tool's produces-kind, and
// every test below said which mapping was the subject by calling it `url` or
// `host`. The role is now a declaration, so the tests say so out loud — and
// `TestTheSubjectIsFoundByRoleAndNotByFieldName` is the one that would have
// caught the old convention leaking back.
func subject(t *testing.T, n byte, field, expression string) domain.Mapping {
	t.Helper()
	m := mapping(t, n, field, expression)
	m.Role = domain.RoleSubject
	return m
}

// source is the mapping naming what a record was READ OUT OF — 0040.
func source(t *testing.T, n byte, field, expression string) domain.Mapping {
	t.Helper()
	m := mapping(t, n, field, expression)
	m.Role = domain.RoleDerivedFrom
	return m
}

// A real httpx line, and the shape the whole design is built around.
const httpxLine = `{"url":"https://acme.test/","host":"acme.test","port":443,` +
	`"status_code":200,"webserver":"nginx/1.24","title":"Acme","tls":{"issuer":"R3"},` +
	`"tech":["nginx","php"],"failed":false}`

func extract(t *testing.T, body string, ms []domain.Mapping) domain.Extraction {
	t.Helper()
	got, err := domain.Extract([]byte(body), ms, "url", domain.ShapeJSON)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// "six mapped fields over one record produce SIX observations" — 0035.
func TestOneRecordProducesOneReadingPerMappedField(t *testing.T) {
	got := extract(t, httpxLine, []domain.Mapping{
		subject(t, 1, "url", ".url"),
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
		subject(t, 1, "url", ".url"),
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
		subject(t, 1, "url", ".url"),
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
		subject(t, 1, "url", ".url"),
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
		subject(t, 1, "url", ".url"),
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
		[]domain.Mapping{subject(t, 1, "host", ".host")}, "host", domain.ShapeJSON)
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
		[]domain.Mapping{subject(t, 1, "host", ".host")}, "host", domain.ShapeJSON)
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
		[]domain.Mapping{subject(t, 1, "host", ".host")}, "host", domain.ShapeJSON)
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

// 0040 §1: the subject is the mapping DECLARING the role — not the first
// mapping, not the first path that reads, and no longer the one whose field
// happens to be spelled like the tool's produces-kind. Every other test here
// lists the subject mapping first, so a mutant taking `mappings[0]` survives all
// of them; this is the one that kills it.
func TestTheSubjectComesFromTheDeclaredRoleAndNotTheFirstMapping(t *testing.T) {
	got, err := domain.Extract([]byte(httpxLine), []domain.Mapping{
		// Deliberately first, and deliberately reading a DIFFERENT value.
		mapping(t, 1, "webserver", ".webserver"),
		mapping(t, 2, "host", ".host"),
		subject(t, 3, "url", ".url"),
	}, "url", domain.ShapeJSON)
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

// 0040 §1, and THE test of that record: the subject is found by ROLE and the
// field may be called anything. Under `0035`'s name match this mapping — field
// `whatever`, on a tool producing `url` — could not have been the subject, and
// the extraction would have been refused outright.
func TestTheSubjectIsFoundByRoleAndNotByFieldName(t *testing.T) {
	got, err := domain.Extract([]byte(httpxLine), []domain.Mapping{
		mapping(t, 1, "webserver", ".webserver"),
		subject(t, 2, "whatever_this_is_called", ".url"),
	}, "url", domain.ShapeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Records) != 1 {
		t.Fatalf("the role resolves whatever the field is called: %+v", got.Records)
	}
	if got.Records[0].SubjectValue != "https://acme.test/" {
		t.Fatalf("subject value: %q", got.Records[0].SubjectValue)
	}
	// The KIND still comes from the tool, not from the mapping — it is what the
	// value is a value OF, and no mapping knows it.
	if got.Records[0].SubjectKind != "url" {
		t.Fatalf("subject kind: %q", got.Records[0].SubjectKind)
	}
}

// 0040: `httpx` echoes the host it was handed in `.input`, and that is the whole
// connection a derivation is drawn from.
func TestAProvenanceMappingReadsWhatTheRecordWasReadOutOf(t *testing.T) {
	line := `{"url":"https://a.acme.test/","input":"a.acme.test"}`
	got, err := domain.Extract([]byte(line), []domain.Mapping{
		subject(t, 1, "url", ".url"),
		source(t, 2, "input", ".input"),
	}, "url", domain.ShapeJSON)
	if err != nil {
		t.Fatal(err)
	}
	rec := got.Records[0]
	if rec.DerivedFrom != "a.acme.test" {
		t.Fatalf("want the input it was handed, got %q", rec.DerivedFrom)
	}
	// THE LABEL IS THE FIELD NAME — 0003 requires an edge to name the act, and
	// a separate column would be a second place to write the same word.
	if rec.DerivedLabel != "input" {
		t.Fatalf("the label is the mapping's field: %q", rec.DerivedLabel)
	}
	if rec.DerivedMapping != nonZero(2) {
		t.Fatalf("the edge cites the version that read it: %v", rec.DerivedMapping)
	}
	// It is ALSO an ordinary reading, so it lands in the observation table and
	// in the field accounting like everything else.
	var found bool
	for _, r := range rec.Readings {
		if r.Mapping.Field == "input" && r.Value == "a.acme.test" {
			found = true
		}
	}
	if !found {
		t.Fatal("a provenance reading is still a reading")
	}
}

// A tool with NO provenance mapping, and a record missing the field, are the
// same absence here — and they are separated one level up, where a tool with no
// mapping produces no unresolved rows and a tool with one that read nothing does.
func TestNoProvenanceMappingAndNoProvenanceValueBothLeaveItEmpty(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		ms   []domain.Mapping
	}{
		{"no mapping", `{"url":"https://a.acme.test/","input":"a.acme.test"}`,
			[]domain.Mapping{subject(t, 1, "url", ".url")}},
		{"no value", `{"url":"https://a.acme.test/"}`,
			[]domain.Mapping{subject(t, 1, "url", ".url"), source(t, 2, "input", ".input")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := domain.Extract([]byte(tc.body), tc.ms, "url", domain.ShapeJSON)
			if err != nil {
				t.Fatal(err)
			}
			if got.Records[0].DerivedFrom != "" {
				t.Fatalf("nothing said where it came from: %q", got.Records[0].DerivedFrom)
			}
			if got.Records[0].DerivedLabel != "" {
				t.Fatalf("no provenance means no label: %q", got.Records[0].DerivedLabel)
			}
		})
	}
}

// And a record that carries every other field but NOT the subject's path yields
// nothing, even though other mappings would have read plenty.
func TestNoSubjectPathMeansNoReadingsEvenWhenOtherFieldsRead(t *testing.T) {
	got, err := domain.Extract([]byte(`{"host":"acme.test","webserver":"nginx"}`),
		[]domain.Mapping{
			mapping(t, 1, "webserver", ".webserver"),
			subject(t, 2, "url", ".url"),
		}, "url", domain.ShapeJSON)
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
		[]domain.Mapping{mapping(t, 1, "webserver", ".webserver")}, "url", domain.ShapeJSON)
	if !errors.Is(err, domain.ErrNoSubjectMapping) {
		t.Fatalf("want ErrNoSubjectMapping, got %v", err)
	}
}

// An object or an array is a BRANCH, not a value. Stringifying one would put a
// JSON blob in a field a person is going to read as a server header.
func TestABranchIsNotAValue(t *testing.T) {
	got := extract(t, httpxLine, []domain.Mapping{
		subject(t, 1, "url", ".url"),
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
		[]domain.Mapping{subject(t, 1, "host", ".host"), mapping(t, 2, "title", ".title")},
		"host", domain.ShapeJSON)
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

// signature and severity are the two roles a FINDING is built from —
// decisions/0041 §2. `nuclei` writes both on every match.
func signature(t *testing.T, n byte, field, expression string) domain.Mapping {
	t.Helper()
	m := mapping(t, n, field, expression)
	m.Role = domain.RoleSignature
	return m
}

func severity(t *testing.T, n byte, field, expression string) domain.Mapping {
	t.Helper()
	m := mapping(t, n, field, expression)
	m.Role = domain.RoleSeverity
	return m
}

// A real nuclei line, and the shape 0041 is built around. `matched-at` is the
// url the finding is ON; `template-id` is what nuclei calls the problem.
const nucleiLine = `{"template-id":"CVE-2021-44228","matched-at":"https://a.acme.test/",` +
	`"info":{"name":"Log4j RCE","severity":"critical"},"type":"http"}`

// 0041 §1: the signature is HALF THE IDENTITY, and it is what makes a rescan a
// sighting rather than a new row. If it is not read, every match on one fragment
// collapses into one finding whose identity is a lie.
func TestAFindingsSignatureAndSeverityAreReadFromTheirRoles(t *testing.T) {
	got, err := domain.Extract([]byte(nucleiLine), []domain.Mapping{
		subject(t, 1, "matched", ".matched-at"),
		signature(t, 2, "template", ".template-id"),
		severity(t, 3, "level", ".info.severity"),
		mapping(t, 4, "name", ".info.name"),
	}, "url", domain.ShapeJSON)
	if err != nil {
		t.Fatal(err)
	}
	rec := got.Records[0]
	if rec.Signature != "CVE-2021-44228" {
		t.Fatalf("the signature is what the tool called the problem: %q", rec.Signature)
	}
	if rec.SignatureMapping != nonZero(2) {
		t.Fatalf("a finding cites the version that read it: %v", rec.SignatureMapping)
	}
	if rec.Severity != "critical" {
		t.Fatalf("severity: %q", rec.Severity)
	}
	// The SUBJECT is the url the finding is on — 0041 §2 makes it the tool's
	// CONSUMES, and it arrives here as the kind argument.
	if rec.SubjectKind != "url" || rec.SubjectValue != "https://a.acme.test/" {
		t.Fatalf("a finding is ON a fragment: %s/%q", rec.SubjectKind, rec.SubjectValue)
	}
}

// Neither role is required for an ordinary tool, and an ordinary tool's records
// carry neither. That is what keeps `httpx` out of the findings table.
func TestAToolWithNoFindingRolesCarriesNoSignature(t *testing.T) {
	got := extract(t, httpxLine, []domain.Mapping{
		subject(t, 1, "url", ".url"),
		mapping(t, 2, "webserver", ".webserver"),
	})
	if got.Records[0].Signature != "" || got.Records[0].Severity != "" {
		t.Fatalf("an ordinary tool produces no finding: %+v", got.Records[0])
	}
}

// The zero role is the HARMLESS one. If it were `derived_from`, every mapping
// anybody forgot to classify would start drawing edges in the entity graph —
// and a default that creates graph edges is the failure `0040` §1 rejected the
// reserved-field-name design to avoid.
func TestTheZeroRoleIsAttribute(t *testing.T) {
	var zero domain.Role
	if zero != domain.RoleAttribute || zero.String() != "attribute" {
		t.Fatalf("the zero role must be attribute, got %s", zero)
	}
}

func TestEveryRoleRoundTripsAndAnUnknownOneIsHarmless(t *testing.T) {
	for _, role := range []domain.Role{
		domain.RoleAttribute, domain.RoleSubject, domain.RoleDerivedFrom,
	} {
		got, err := domain.ParseRole(role.String())
		if err != nil || got != role {
			t.Fatalf("%s round-tripped to %s (%v)", role, got, err)
		}
	}
	if _, err := domain.ParseRole("provenance"); !errors.Is(err, domain.ErrRoleUnknown) {
		t.Fatalf("want ErrRoleUnknown, got %v", err)
	}
	// AND IT FALLS BACK TO THE HARMLESS ONE. The store's row mapper discards
	// this error deliberately — an observation is a statement already made and
	// is not lost to an enum added later — so what it falls back TO is the only
	// thing standing between a bad column and an invented edge.
	if got, _ := domain.ParseRole("nonsense"); got != domain.RoleAttribute {
		t.Fatalf("an unknown role must not become derived_from, got %s", got)
	}
}

// The role travels ONTO THE ROW, not just through the extraction. It is what
// `ProvenanceForInvocation` filters on, so an observation that forgot it is a
// derivation that silently never happens.
func TestAnObservationRecordsTheRoleThatReadIt(t *testing.T) {
	o, err := domain.New(nonZero(1), nonZero(2), nonZero(3), nonZero(4), nonZero(5), 7,
		"url", "https://a.acme.test/", "input", "a.acme.test",
		domain.RoleDerivedFrom, at, at)
	if err != nil {
		t.Fatal(err)
	}
	if o.Role != domain.RoleDerivedFrom {
		t.Fatalf("the role the mapping declared is what this row IS: %s", o.Role)
	}
	plain, err := domain.New(nonZero(1), nonZero(2), nonZero(3), nonZero(4), nonZero(5), 7,
		"url", "https://a.acme.test/", "title", "Acme",
		domain.RoleAttribute, at, at)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Role != domain.RoleAttribute {
		t.Fatalf("an ordinary reading is an attribute: %s", plain.Role)
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
