// Author-written, against a live database. The upsert IS decisions/0041 §1 —
// "a rescan is a sighting rather than a new row" is a unique index and an
// `on conflict` clause, and neither can be checked anywhere else.
package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/finding/domain"
	findingpg "github.com/0xsj/overwatch-backend/internal/finding/infra/postgres"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/testx"
)

var (
	monday = time.Date(2026, 9, 8, 2, 0, 0, 0, time.UTC)
	friday = time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC)
)

func an(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}

func store(t *testing.T) (*findingpg.Store, context.Context) {
	t.Helper()
	p := testx.Postgres(t, testx.Schema{
		Name: findingpg.Schema, Migrations: findingpg.Migrations,
	})
	return findingpg.NewStore(p), context.Background()
}

// sighting builds what extraction would hand the store. `n` varies only the
// row's own id, so two calls are two DIFFERENT rows arriving for the SAME
// identity — which is exactly what a second night's scan looks like.
//
// **The FRAGMENT ID is derived from the value**, because the identity keys on
// the id and not on the string. The first draft of this helper hardcoded it, so
// "a different fragment" passed a different URL to the same fragment and the
// upsert correctly treated it as one problem — a test asserting the opposite of
// what it set up.
func sighting(t *testing.T, n byte, signature, value string, at time.Time) domain.Finding {
	t.Helper()
	f, err := domain.New(an(n), an(2), an(3), fragmentFor(value), signature,
		"url", value, domain.SeverityCritical, "reported by "+signature,
		an(5+n), an(6), an(7), at, at)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// fragmentFor stands in for the lookup the command does against `entity`. One
// value is one fragment, which is what `0036` makes true.
func fragmentFor(value string) id.ID {
	var out id.ID
	out[0] = 0x60
	for i := 0; i < len(value); i++ {
		out[1] ^= value[i]
	}
	return out
}

// **THE test of this migration.** Thirty nights is one row.
func TestTheSameProblemOnTheSameFragmentIsOneRow(t *testing.T) {
	s, ctx := store(t)

	first, opened, err := s.Record(ctx, sighting(t, 1, "CVE-2021-44228", "https://a.acme.test/", monday))
	if err != nil {
		t.Fatal(err)
	}
	if !opened {
		t.Fatal("the first sighting OPENS the finding")
	}
	if first.Sightings != 1 {
		t.Fatalf("sightings: %d", first.Sightings)
	}

	second, opened, err := s.Record(ctx, sighting(t, 9, "CVE-2021-44228", "https://a.acme.test/", friday))
	if err != nil {
		t.Fatal(err)
	}
	if opened {
		t.Fatal("a rescan is a SIGHTING, not a new problem")
	}
	if second.ID != first.ID {
		t.Fatalf("a rescan must not mint a second row: %v vs %v", second.ID, first.ID)
	}
	if second.Sightings != 2 {
		t.Fatalf("two sightings are two sightings, got %d", second.Sightings)
	}
	if !second.LastSeen.Equal(friday) {
		t.Fatalf("last_seen moves: %v", second.LastSeen)
	}
	if !second.FirstSeen.Equal(monday) {
		t.Fatalf("first_seen never moves forward: %v", second.FirstSeen)
	}
	// The citation follows the newest evidence.
	if second.Invocation != an(14) {
		t.Fatalf("the citation follows the newest run: %v", second.Invocation)
	}
}

// A RE-EXTRACTION OF AN OLDER ARTIFACT must not make a stale run look like
// tonight's. `greatest`/`least` in the upsert are what hold it, and a mutation
// round found both of them unkilled: every other test here only ever moves time
// forward, so the ordering was asserted by nothing.
func TestAnOlderSightingMovesFirstSeenAndNeverTheCitation(t *testing.T) {
	s, ctx := store(t)
	if _, _, err := s.Record(ctx, sighting(t, 1, "CVE-2021-44228", "https://a.acme.test/", friday)); err != nil {
		t.Fatal(err)
	}
	// The same problem, re-extracted from an artifact four days older.
	after, opened, err := s.Record(ctx, sighting(t, 9, "CVE-2021-44228", "https://a.acme.test/", monday))
	if err != nil {
		t.Fatal(err)
	}
	if opened {
		t.Fatal("still one problem")
	}
	if !after.FirstSeen.Equal(monday) {
		t.Fatalf("an older sighting moves first_seen BACK: %v", after.FirstSeen)
	}
	if !after.LastSeen.Equal(friday) {
		t.Fatalf("and never moves last_seen back: %v", after.LastSeen)
	}
	// THE CITATION STAYS ON THE NEWEST RUN. A reader following it must not land
	// on an artifact older than the one that most recently saw this.
	if after.Invocation != an(6) {
		t.Fatalf("a stale re-extraction stole the citation: %v", after.Invocation)
	}
	if after.Sightings != 2 {
		t.Fatalf("it is still a sighting: %d", after.Sightings)
	}
}

// The identity is the whole tuple. Change any part of it and it is a different
// problem — which is what stops the board merging two real things.
func TestADifferentSignatureOrFragmentIsADifferentFinding(t *testing.T) {
	s, ctx := store(t)
	if _, _, err := s.Record(ctx, sighting(t, 1, "CVE-2021-44228", "https://a.acme.test/", monday)); err != nil {
		t.Fatal(err)
	}
	// DISTINCT row ids per case: both are genuinely new findings, so both
	// INSERT, and reusing one id collides on the primary key rather than on the
	// identity index this test is about.
	for n, tc := range []struct{ name, signature, value string }{
		{"another signature", "CVE-2020-1234", "https://a.acme.test/"},
		{"another fragment", "CVE-2021-44228", "https://b.acme.test/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, opened, err := s.Record(ctx, sighting(t, byte(0x20+n), tc.signature, tc.value, friday))
			if err != nil {
				t.Fatal(err)
			}
			if !opened {
				t.Fatal("a different tuple is a different finding")
			}
		})
	}
}

// TRIAGE STICKS. This is the reason §1 chose one row per problem, and it is the
// one behaviour a board is unusable without.
func TestARescanDoesNotUndoATriageInTheDatabase(t *testing.T) {
	s, ctx := store(t)
	first, _, err := s.Record(ctx, sighting(t, 1, "CVE-2021-44228", "https://a.acme.test/", monday))
	if err != nil {
		t.Fatal(err)
	}
	triaged, err := first.Triage(an(9), monday)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, triaged); err != nil {
		t.Fatal(err)
	}

	after, _, err := s.Record(ctx, sighting(t, 9, "CVE-2021-44228", "https://a.acme.test/", friday))
	if err != nil {
		t.Fatal(err)
	}
	if after.State != domain.StateTriaged {
		t.Fatalf("tonight's scan reopened a triaged finding: %s", after.State)
	}
	if after.DecidedBy != an(9) {
		t.Fatalf("and lost who ruled: %v", after.DecidedBy)
	}
}

// A resolved finding seen again reopens, and the ruling is CLEARED rather than
// left standing as a lie beside an `open` state.
func TestAResolvedFindingSeenAgainReopensInTheDatabase(t *testing.T) {
	s, ctx := store(t)
	first, _, err := s.Record(ctx, sighting(t, 1, "CVE-2021-44228", "https://a.acme.test/", monday))
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := first.Resolve(an(9), "patched", monday)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, fixed); err != nil {
		t.Fatal(err)
	}

	after, _, err := s.Record(ctx, sighting(t, 9, "CVE-2021-44228", "https://a.acme.test/", friday))
	if err != nil {
		t.Fatal(err)
	}
	if after.State != domain.StateOpen {
		t.Fatalf("a fix that did not hold is open again, got %s", after.State)
	}
	if !after.DecidedBy.IsZero() || after.Reason != "" {
		t.Fatalf("the stale ruling must be cleared: %+v", after)
	}
	if !after.FirstSeen.Equal(monday) || after.Sightings != 2 {
		t.Fatalf("and the history survives: %v / %d", after.FirstSeen, after.Sightings)
	}
}

// A rescan must not overwrite a human's severity with the template's opinion.
func TestARescanDoesNotOverwriteAHumanSeverityInTheDatabase(t *testing.T) {
	s, ctx := store(t)
	first, _, err := s.Record(ctx, sighting(t, 1, "CVE-2021-44228", "https://a.acme.test/", monday))
	if err != nil {
		t.Fatal(err)
	}
	lowered, err := first.Reassess(domain.SeverityLow, domain.Assessment{
		Claimant: domain.ByHuman, Actor: an(9), Basis: "behind auth", At: monday,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, lowered); err != nil {
		t.Fatal(err)
	}

	after, _, err := s.Record(ctx, sighting(t, 9, "CVE-2021-44228", "https://a.acme.test/", friday))
	if err != nil {
		t.Fatal(err)
	}
	if after.Severity != domain.SeverityLow {
		t.Fatalf("the template's opinion overwrote a person's: %s", after.Severity)
	}
	if after.Superseded == nil || after.SupersededSeverity != domain.SeverityCritical {
		t.Fatalf("and 0004's kept prior claim was lost: %+v", after.Superseded)
	}
}

// 0009's badge: a count per fragment, live states only.
func TestTheBadgeCountsOnlyLiveFindings(t *testing.T) {
	s, ctx := store(t)
	one, _, err := s.Record(ctx, sighting(t, 1, "sig-a", "https://a.acme.test/", monday))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Record(ctx, sighting(t, 2, "sig-b", "https://a.acme.test/", monday)); err != nil {
		t.Fatal(err)
	}
	badges, err := s.Live(ctx, an(2))
	if err != nil {
		t.Fatal(err)
	}
	if badges[fragmentFor("https://a.acme.test/")].Live != 2 {
		t.Fatalf("two open findings on one fragment: %+v", badges)
	}
	if badges[fragmentFor("https://a.acme.test/")].Worst != domain.SeverityCritical {
		t.Fatalf("the badge carries the WORST live severity: %s", badges[fragmentFor("https://a.acme.test/")].Worst)
	}

	fixed, err := one.Resolve(an(9), "", monday)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, fixed); err != nil {
		t.Fatal(err)
	}
	badges, err = s.Live(ctx, an(2))
	if err != nil {
		t.Fatal(err)
	}
	if badges[fragmentFor("https://a.acme.test/")].Live != 1 {
		t.Fatalf("a resolved finding stops counting against the estate: %+v", badges)
	}
}

// A detail is ONE ROW PER FIELD. A nightly rescan updates rather than appends,
// so this stays bounded while `sightings` grows.
func TestADetailIsOneRowPerFieldHoweverManyNights(t *testing.T) {
	s, ctx := store(t)
	first, _, err := s.Record(ctx, sighting(t, 1, "CVE-2021-44228", "https://a.acme.test/", monday))
	if err != nil {
		t.Fatal(err)
	}
	for n, tc := range []struct {
		value string
		at    time.Time
	}{{"Log4j RCE", monday}, {"Log4j RCE (updated)", friday}} {
		d, err := domain.NewDetail(an(byte(0x40+n)), first.ID, "name", tc.value,
			an(7), an(6), tc.at)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SaveDetail(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	details, err := s.Details(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(details) != 1 {
		t.Fatalf("two nights of one field is one row, got %d", len(details))
	}
	if details[0].Value != "Log4j RCE (updated)" {
		t.Fatalf("the newest reading wins: %q", details[0].Value)
	}

	// AND AN OLDER RE-EXTRACTION DOES NOT WIN. The `where excluded.seen_at >=`
	// guard is what stops re-reading a stale artifact rewriting tonight's value.
	stale, err := domain.NewDetail(an(0x50), first.ID, "name", "ancient", an(7), an(6),
		monday.Add(-72*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveDetail(ctx, stale); err != nil {
		t.Fatal(err)
	}
	details, err = s.Details(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if details[0].Value != "Log4j RCE (updated)" {
		t.Fatalf("a stale re-extraction overwrote a newer reading: %q", details[0].Value)
	}
}
