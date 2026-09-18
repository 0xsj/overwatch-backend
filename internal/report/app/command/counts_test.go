// Author-written, from decisions/0042 §4. The COUNTS are asserted here rather
// than end-to-end because an engagement with no scans makes every number zero,
// and zero equals zero however the arithmetic is wrong — a mutation round found
// exactly that: folding `dismissed` into `open` survived every e2e test.
package command_test

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/report/app/command"
	"github.com/0xsj/overwatch-backend/internal/report/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var at = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func an(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}

// ---------------------------------------------------------------- the fakes

type store struct {
	held      domain.Report
	revisions []domain.Revision
}

func (s *store) Create(_ context.Context, r domain.Report) error { s.held = r; return nil }
func (s *store) ByID(context.Context, id.ID, id.ID) (domain.Report, error) {
	return s.held, nil
}
func (s *store) Save(_ context.Context, r domain.Report) error { s.held = r; return nil }
func (s *store) AddRevision(_ context.Context, rev domain.Revision) error {
	s.revisions = append(s.revisions, rev)
	return nil
}
func (s *store) NextRevision(context.Context, id.ID) (int, error) {
	return len(s.revisions) + 1, nil
}

// said is what the seven ports answer. Every field is set explicitly so a test
// asserting a count is asserting arithmetic rather than a fixture.
type said struct {
	rules     []domain.Rule
	claims    []domain.Claim
	assets    []domain.Asset
	open      []domain.Finding
	dismissed int
	cells     []domain.Cell
	notes     []domain.Note
	runs      []domain.Invocation
	artifacts []domain.Artifact
}

func (s said) Scope(context.Context, id.ID, id.ID) ([]domain.Rule, error) { return s.rules, nil }
func (s said) Attribution(context.Context, id.ID, id.ID) ([]domain.Claim, error) {
	return s.claims, nil
}
func (s said) Assets(context.Context, id.ID, id.ID) ([]domain.Asset, error) { return s.assets, nil }
func (s said) Findings(context.Context, id.ID, id.ID) ([]domain.Finding, int, error) {
	return s.open, s.dismissed, nil
}
func (s said) Coverage(context.Context, id.ID, id.ID) ([]domain.Cell, error) { return s.cells, nil }
func (s said) Notes(context.Context, id.ID, id.ID) ([]domain.Note, error)    { return s.notes, nil }
func (s said) Invocations(context.Context, id.ID, id.ID) ([]domain.Invocation, error) {
	return s.runs, nil
}
func (s said) Artifacts(context.Context, id.ID, id.ID) ([]domain.Artifact, error) {
	return s.artifacts, nil
}

// keptBytes records what was frozen, which is how a test reads the document
// without a blob store.
type keptBytes struct{ body []byte }

func (k *keptBytes) Put(_ context.Context, r io.Reader) (string, int64, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return "", 0, err
	}
	k.body = body
	return "sha256:test", int64(len(body)), nil
}

type direct struct{}

func (direct) InSnapshot(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

func (direct) InTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

type minter struct{ n byte }

func (m *minter) NewID() id.ID { m.n++; return an(0x80 + m.n) }

type frozen struct{}

func (frozen) Now() time.Time { return at }

type quiet struct{}

func (quiet) Publish(context.Context, ...events.Event) error { return nil }

func rig(t *testing.T, answers said) (*command.Reports, *keptBytes) {
	t.Helper()
	kept := &keptBytes{}
	return command.NewReports(&store{}, answers, kept, direct{}, quiet{}, &minter{}, frozen{}), kept
}

// countsOf reads one section's counts out of a rendered document.
func countsOf(t *testing.T, doc domain.Document, key string) map[string]int {
	t.Helper()
	for _, one := range doc.Sections {
		if one.Key != key {
			continue
		}
		out := map[string]int{}
		for _, c := range one.Counts {
			out[c.Label] = c.Value
		}
		return out
	}
	t.Fatalf("no section %q in %v", key, doc.Sections)
	return nil
}

// ---------------------------------------------------------------- the claims

// **"8 open · 1 dismissed and excluded" is ONE SENTENCE saying both.** A count
// that folded them together would say something false, and a silently shorter
// list would say neither.
func TestDismissedFindingsAreExcludedFromTheRowsAndCountedApart(t *testing.T) {
	reports, _ := rig(t, said{
		open: []domain.Finding{
			{Signature: "CVE-1", Severity: "critical"},
			{Signature: "CVE-2", Severity: "low"},
		},
		dismissed: 3,
	})
	opened, err := reports.Open(context.Background(), an(1), an(2), command.Draft{
		TargetID: an(3), Title: "Acme Q3",
	})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := reports.Render(context.Background(), an(1), opened.ID)
	if err != nil {
		t.Fatal(err)
	}
	counts := countsOf(t, doc, "findings_by_severity")
	if counts["open"] != 2 {
		t.Fatalf("open counts the ROWS, got %d", counts["open"])
	}
	if counts["dismissed and excluded"] != 3 {
		t.Fatalf("dismissed is counted APART, got %d", counts["dismissed and excluded"])
	}
}

// The asset count PARTITIONS, and the two halves add up. `14 assets · 11 in
// scope, 3 not` is the draft's own example of where the report has to reconcile.
func TestTheAssetCountPartitions(t *testing.T) {
	reports, _ := rig(t, said{
		assets: []domain.Asset{
			{Kind: "host", Value: "a.acme.test", InScope: true},
			{Kind: "host", Value: "b.acme.test", InScope: true},
			{Kind: "host", Value: "c.acme.test", InScope: false},
		},
	})
	opened, _ := reports.Open(context.Background(), an(1), an(2), command.Draft{
		TargetID: an(3), Title: "Acme Q3",
	})
	doc, err := reports.Render(context.Background(), an(1), opened.ID)
	if err != nil {
		t.Fatal(err)
	}
	counts := countsOf(t, doc, "asset_inventory")
	if counts["assets"] != 3 || counts["in scope"] != 2 || counts["not in scope"] != 1 {
		t.Fatalf("the halves must add up: %v", counts)
	}
}

// `never` and `stale` are DIFFERENT FAILURES — nobody asked, versus the answer
// is old — and `0011` says both belong in the summary. Every cell is an
// applicable pair, because inapplicability is the ABSENCE of a cell.
func TestCoverageKeepsNeverAndStaleApart(t *testing.T) {
	reports, _ := rig(t, said{
		cells: []domain.Cell{
			{Asset: "a", Check: "tls", State: "never"},
			{Asset: "b", Check: "tls", State: "stale"},
			{Asset: "c", Check: "tls", State: "fresh"},
			{Asset: "d", Check: "tls", State: "never"},
		},
	})
	opened, _ := reports.Open(context.Background(), an(1), an(2), command.Draft{
		TargetID: an(3), Title: "Acme Q3",
	})
	doc, err := reports.Render(context.Background(), an(1), opened.ID)
	if err != nil {
		t.Fatal(err)
	}
	counts := countsOf(t, doc, "coverage")
	if counts["never attempted"] != 2 || counts["stale"] != 1 || counts["applicable pairs"] != 4 {
		t.Fatalf("never and stale are different failures: %v", counts)
	}
}

// The attribution count partitions by CLAIMANT — `14 claims · 11 rule · 1 human
// · 2 model` — which is the other denominator the draft says has to reconcile.
func TestTheAttributionCountPartitionsByClaimant(t *testing.T) {
	reports, _ := rig(t, said{
		claims: []domain.Claim{
			{Claimant: "rule"}, {Claimant: "rule"}, {Claimant: "human"}, {Claimant: "model"},
		},
	})
	opened, _ := reports.Open(context.Background(), an(1), an(2), command.Draft{
		TargetID: an(3), Title: "Acme Q3",
	})
	doc, err := reports.Render(context.Background(), an(1), opened.ID)
	if err != nil {
		t.Fatal(err)
	}
	counts := countsOf(t, doc, "attribution_evidence")
	if counts["claims"] != 4 || counts["rule"] != 2 || counts["human"] != 1 || counts["model"] != 1 {
		t.Fatalf("the claimants must add up to the total: %v", counts)
	}
}

// The eighth section is SOURCED, not free text — decisions/0043 §3. Each note
// carries who wrote it and when, which is the standard the other seven meet.
func TestTheNotesSectionCountsAuthorsAsWellAsNotes(t *testing.T) {
	reports, _ := rig(t, said{
		notes: []domain.Note{
			{Body: "scope widened on the 3rd", Author: "acct_sam", At: "2026-09-03"},
			{Body: "client asked us to skip staging", Author: "acct_sam", At: "2026-09-04"},
			{Body: "handover to kit", Author: "acct_kit", At: "2026-09-05"},
		},
	})
	opened, _ := reports.Open(context.Background(), an(1), an(2), command.Draft{
		TargetID: an(3), Title: "Acme Q3",
	})
	doc, err := reports.Render(context.Background(), an(1), opened.ID)
	if err != nil {
		t.Fatal(err)
	}
	counts := countsOf(t, doc, "engagement_notes")
	if counts["notes"] != 3 {
		t.Fatalf("notes: %v", counts)
	}
	// AUTHORS, because "three notes by one person" and "three by three" are
	// different documents and the count alone hides it.
	if counts["authors"] != 2 {
		t.Fatalf("authors: %v", counts)
	}
}

// **The frozen bytes are the rendered document**, and the hash is of what a
// reader opening the file would see.
func TestTheFrozenBytesAreTheDocument(t *testing.T) {
	reports, kept := rig(t, said{
		assets: []domain.Asset{{Kind: "host", Value: "a.acme.test", InScope: true}},
	})
	opened, _ := reports.Open(context.Background(), an(1), an(2), command.Draft{
		TargetID: an(3), Title: "Acme Q3",
	})
	rev, err := reports.Issue(context.Background(), an(1), opened.ID, an(9))
	if err != nil {
		t.Fatal(err)
	}
	if rev.Hash == "" || rev.Bytes != int64(len(kept.body)) {
		t.Fatalf("the revision addresses what was stored: %+v", rev)
	}
	var doc domain.Document
	if err := json.Unmarshal(kept.body, &doc); err != nil {
		t.Fatalf("the frozen bytes must be the document: %v", err)
	}
	// SIX now — decisions/0043 added the engagement notes, on by default.
	if len(doc.Sections) != 6 || doc.Revision != 1 {
		t.Fatalf("six sections, revision one: %d / %d", len(doc.Sections), doc.Revision)
	}
	if len(doc.Withheld) != 2 {
		t.Fatalf("the document names what it left out: %v", doc.Withheld)
	}
}
