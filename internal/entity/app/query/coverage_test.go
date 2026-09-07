// Author-written, and the oracle is decisions/0011's Verification block —
// written 2026-09-06 against packages that did not exist, and reproduced here
// line by line. It is as close to an independent specification as this tree has.
package query_test

import (
	"context"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/entity/app/query"
	"github.com/0xsj/overwatch-backend/internal/entity/domain"
	"github.com/0xsj/overwatch-backend/internal/entity/infra/memory"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var now = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

func nonZero(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}

// checks and checked are the two ports, as fixtures.
type checks []query.Check

func (c checks) ForCoverage(context.Context, id.ID) ([]query.Check, error) { return c, nil }

type checked []query.CheckedAt

func (c checked) LatestPerSubject(context.Context, id.ID, id.ID) ([]query.CheckedAt, error) {
	return c, nil
}

const ws = 200

// world builds an engagement: assets of the given kinds, all attributed, plus
// whatever checks and check-history the case needs.
func world(t *testing.T, kinds []string, cs checks, ch checked) (*query.Graph, []domain.Fragment) {
	t.Helper()
	store := memory.New()
	ctx := context.Background()

	root, err := domain.NewEntity(nonZero(1), nonZero(ws), "org", "acme", now)
	if err != nil {
		t.Fatal(err)
	}
	root = root.Root(nonZero(2))
	if err := store.CreateEntity(ctx, root); err != nil {
		t.Fatal(err)
	}

	out := make([]domain.Fragment, 0, len(kinds))
	for n, kind := range kinds {
		f, err := domain.NewFragment(nonZero(byte(10+n)), nonZero(ws), kind,
			kind+"-"+string(rune('a'+n))+".acme.test", domain.Observed, now)
		if err != nil {
			t.Fatal(err)
		}
		stored, _, err := store.Upsert(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		a, err := domain.Propose(nonZero(byte(60+n)), nonZero(ws), root.ID, stored.ID,
			domain.ByRule, nonZero(9), 0, false, "a rule permitted the run", now)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Attribute(ctx, a); err != nil {
			t.Fatal(err)
		}
		out = append(out, stored)
	}
	return query.NewGraph(store, cs, ch), out
}

func report(t *testing.T, g *query.Graph) query.Report {
	t.Helper()
	got, err := g.Compute(context.Background(), nonZero(ws), id.ID{}, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func check(n byte, name string, applies []string, interval time.Duration, human bool) query.Check {
	return query.Check{ID: nonZero(n), Name: name, AppliesTo: applies, Interval: interval, Human: human}
}

// 0011's headline: the denominator for 12 hosts, 1 cidr and 1 asn is NOT 84.
func TestTheDenominatorIsRaggedAndNotAProduct(t *testing.T) {
	kinds := make([]string, 0, 14)
	for i := 0; i < 12; i++ {
		kinds = append(kinds, "host")
	}
	kinds = append(kinds, "cidr", "asn")

	// The mock's six: DNS, PORTS, TLS, HTTP FINGER, CONTENT, READ BY YOU.
	// An ASN has no TLS certificate, no HTTP fingerprint and no page content.
	cs := checks{
		check(1, "dns", []string{"host"}, time.Hour, false),
		check(2, "ports", []string{"host", "cidr", "ip"}, time.Hour, false),
		check(3, "tls", []string{"host", "url"}, time.Hour, false),
		check(4, "http", []string{"host", "url"}, time.Hour, false),
		check(5, "content", []string{"host", "url"}, time.Hour, false),
		check(6, "read by you", everyKind(), 0, true),
	}
	g, _ := world(t, kinds, cs, nil)
	got := report(t, g)

	if got.Summary.Pairs == 14*6 {
		t.Fatal("the rectangle is back: 14 x 6 counts cells that could never have had an answer")
	}
	// 12 hosts x 6 applicable + cidr (ports, read) + asn (read).
	want := 12*6 + 2 + 1
	if got.Summary.Pairs != want {
		t.Fatalf("want %d pairs, got %d", want, got.Summary.Pairs)
	}
}

func everyKind() []string {
	return []string{"host", "cidr", "ip", "asn", "url", "repo", "email"}
}

// "a check not applicable to a kind produces NO CELL for that kind, rather than
// a cell in state never"
func TestAnInapplicableCheckProducesNoCellAtAll(t *testing.T) {
	cs := checks{
		check(1, "tls", []string{"host"}, time.Hour, false),
		check(2, "read by you", everyKind(), 0, true),
	}
	g, _ := world(t, []string{"asn"}, cs, nil)
	got := report(t, g)

	if len(got.Rows) != 1 {
		t.Fatalf("one asset, got %d rows", len(got.Rows))
	}
	if len(got.Rows[0].Cells) != 1 {
		t.Fatalf("an ASN has no TLS certificate — that is not a pair: %+v", got.Rows[0].Cells)
	}
	if got.Rows[0].Cells[0].CheckName != "read by you" {
		t.Fatalf("the surviving cell is the human check: %+v", got.Rows[0].Cells[0])
	}
	// "an n/a cell appears in neither the numerator nor the denominator"
	if got.Summary.Pairs != 1 {
		t.Fatalf("the denominator counted an impossible cell: %d", got.Summary.Pairs)
	}
}

// "READ BY YOU is applicable to every kind, including asn and cidr"
func TestTheHumanCheckIsUniversal(t *testing.T) {
	cs := checks{check(1, "read by you", everyKind(), 0, true)}
	g, _ := world(t, everyKind(), cs, nil)
	got := report(t, g)
	if len(got.Rows) != len(everyKind()) {
		t.Fatalf("a person can read anything: %d rows for %d kinds", len(got.Rows), len(everyKind()))
	}
	for _, row := range got.Rows {
		if len(row.Cells) != 1 {
			t.Fatalf("%s: %+v", row.Asset.Kind, row.Cells)
		}
	}
}

// "READ BY YOU never reports stale, on any input, at any age"
func TestTheHumanCheckNeverGoesStale(t *testing.T) {
	cs := checks{check(1, "read by you", everyKind(), 0, true)}
	store := memory.New()
	ctx := context.Background()
	root, _ := domain.NewEntity(nonZero(1), nonZero(ws), "org", "acme", now)
	root = root.Root(nonZero(2))
	store.CreateEntity(ctx, root)
	f, _ := domain.NewFragment(nonZero(10), nonZero(ws), "host", "a.acme.test", domain.Observed, now)
	// READ TEN YEARS AGO.
	f, err := f.Read(nonZero(5), now.Add(-10*365*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	stored, _, _ := store.Upsert(ctx, f)
	a, _ := domain.Propose(nonZero(60), nonZero(ws), root.ID, stored.ID,
		domain.ByRule, nonZero(9), 0, false, "b", now)
	store.Attribute(ctx, a)

	g := query.NewGraph(store, cs, checked(nil))
	got, err := g.Compute(ctx, nonZero(ws), id.ID{}, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Rows[0].Cells[0].State != query.Fresh {
		t.Fatalf("a human read does not go stale: %v", got.Rows[0].Cells[0].State)
	}
	if got.Summary.Stale != 0 {
		t.Fatal("and it never contributes to the stale count")
	}
}

// "never and stale are reported as two numbers and never summed into one"
func TestNeverAndStaleAreTwoNumbers(t *testing.T) {
	cs := checks{
		check(1, "fresh one", []string{"host"}, 24*time.Hour, false),
		check(2, "stale one", []string{"host"}, time.Hour, false),
		check(3, "never one", []string{"host"}, time.Hour, false),
	}
	g, frags := world(t, []string{"host"}, cs, nil)
	_ = frags
	// Rebuild with history: check 1 an hour ago (fresh), check 2 a day ago.
	g2, f := world(t, []string{"host"}, cs, nil)
	_ = g2
	value := f[0].Value
	g3, _ := world(t, []string{"host"}, cs, checked{
		{CheckID: nonZero(1), Kind: "host", Value: value, At: now.Add(-time.Hour)},
		{CheckID: nonZero(2), Kind: "host", Value: value, At: now.Add(-24 * time.Hour)},
	})
	got := report(t, g3)
	s := got.Summary
	if s.Fresh != 1 || s.Stale != 1 || s.Never != 1 {
		t.Fatalf("want 1/1/1 fresh/stale/never, got %d/%d/%d", s.Fresh, s.Stale, s.Never)
	}
	if s.Pairs != 3 {
		t.Fatalf("three applicable checks: %d", s.Pairs)
	}
	_ = g
}

// "a coverage percentage recomputed after making a check inapplicable to a kind
// has BOTH a smaller numerator and a smaller denominator"
func TestNarrowingAppliesToShrinksBothHalves(t *testing.T) {
	value := ""
	wide := checks{
		check(1, "dns", []string{"host", "asn"}, time.Hour, false),
		check(2, "read by you", everyKind(), 0, true),
	}
	g, f := world(t, []string{"host", "asn"}, wide, nil)
	value = f[0].Value
	history := checked{
		{CheckID: nonZero(1), Kind: "host", Value: value, At: now},
		{CheckID: nonZero(1), Kind: "asn", Value: f[1].Value, At: now},
	}
	g, _ = world(t, []string{"host", "asn"}, wide, history)
	before := report(t, g).Summary

	narrow := checks{
		check(1, "dns", []string{"host"}, time.Hour, false), // an ASN has no DNS
		check(2, "read by you", everyKind(), 0, true),
	}
	g, _ = world(t, []string{"host", "asn"}, narrow, history)
	after := report(t, g).Summary

	if !(after.Pairs < before.Pairs) {
		t.Fatalf("denominator did not shrink: %d -> %d", before.Pairs, after.Pairs)
	}
	if !(after.Fresh < before.Fresh) {
		t.Fatalf("numerator did not shrink: %d -> %d — 0011: any implementation "+
			"that reports a higher percentage without a smaller denominator has a bug",
			before.Fresh, after.Fresh)
	}
}

// "a subject with every applicable check fresh reports 100%, and a subject with
// no applicable checks at all is EXCLUDED rather than reported as 100%"
func TestAnAssetWithNoApplicableChecksIsExcluded(t *testing.T) {
	cs := checks{check(1, "dns", []string{"host"}, time.Hour, false)}
	g, _ := world(t, []string{"host", "asn"}, cs, nil)
	got := report(t, g)
	if len(got.Rows) != 1 {
		t.Fatalf("the ASN has no applicable check and must be excluded: %d rows", len(got.Rows))
	}
	if got.Rows[0].Asset.Kind != "host" {
		t.Fatalf("wrong row survived: %s", got.Rows[0].Asset.Kind)
	}
	if got.Summary.Assets != 1 {
		t.Fatalf("and it is not counted: %d", got.Summary.Assets)
	}
}

func TestEveryApplicableCheckFreshIsComplete(t *testing.T) {
	cs := checks{
		check(1, "dns", []string{"host"}, time.Hour, false),
		check(2, "ports", []string{"host"}, time.Hour, false),
	}
	g, f := world(t, []string{"host"}, cs, nil)
	g, _ = world(t, []string{"host"}, cs, checked{
		{CheckID: nonZero(1), Kind: "host", Value: f[0].Value, At: now},
		{CheckID: nonZero(2), Kind: "host", Value: f[0].Value, At: now},
	})
	got := report(t, g).Summary
	fresh, pairs := got.Percent()
	if fresh != pairs || pairs != 2 {
		t.Fatalf("want 2/2, got %d/%d", fresh, pairs)
	}
}

// 0037: a check with NO CLOCK runs when somebody asks and never lapses. Zero is
// "on demand" and not "immediately stale".
func TestACheckWithNoIntervalNeverLapses(t *testing.T) {
	cs := checks{check(1, "on demand", []string{"host"}, 0, false)}
	g, f := world(t, []string{"host"}, cs, nil)
	g, _ = world(t, []string{"host"}, cs, checked{
		{CheckID: nonZero(1), Kind: "host", Value: f[0].Value, At: now.Add(-10000 * time.Hour)},
	})
	got := report(t, g)
	if got.Rows[0].Cells[0].State != query.Fresh {
		t.Fatalf("a check with no clock does not lapse: %v", got.Rows[0].Cells[0].State)
	}
}

// 0037 §3: reading is not ruling, and ruling is not reading.
func TestReadingAndJudgingAreTwoActs(t *testing.T) {
	f, err := domain.NewFragment(nonZero(1), nonZero(2), "host", "a.acme.test", domain.Observed, now)
	if err != nil {
		t.Fatal(err)
	}
	read, err := f.Read(nonZero(5), now)
	if err != nil {
		t.Fatal(err)
	}
	if read.Judgement.State != domain.Unopened {
		t.Fatal("reading does not rule — a person can read and decline to rule")
	}

	ruled, err := domain.Rule(domain.Triaged, nonZero(5), "", now)
	if err != nil {
		t.Fatal(err)
	}
	judged := f.Judge(ruled)
	if judged.HasBeenRead() {
		t.Fatal("ruling does not set read_at — 0011 keeps `never read` and `no judgement` separable")
	}
}
