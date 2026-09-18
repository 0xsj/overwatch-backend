package root

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	entquery "github.com/0xsj/overwatch-backend/internal/entity/app/query"
	entdomain "github.com/0xsj/overwatch-backend/internal/entity/domain"
	entpg "github.com/0xsj/overwatch-backend/internal/entity/infra/postgres"
	findingquery "github.com/0xsj/overwatch-backend/internal/finding/app/query"
	findingdomain "github.com/0xsj/overwatch-backend/internal/finding/domain"
	findingpg "github.com/0xsj/overwatch-backend/internal/finding/infra/postgres"
	notequery "github.com/0xsj/overwatch-backend/internal/note/app/query"
	notedomain "github.com/0xsj/overwatch-backend/internal/note/domain"
	notepg "github.com/0xsj/overwatch-backend/internal/note/infra/postgres"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/testx"
)

func reportTestID(n int) id.ID {
	var out id.ID
	binary.BigEndian.PutUint64(out[8:], uint64(n))
	return out
}

type reportChecks struct{}

func (reportChecks) ForCoverage(context.Context, id.ID) ([]entquery.Check, error) {
	return []entquery.Check{{ID: reportTestID(100), Name: "Review", AppliesTo: []string{"host"}, Human: true}}, nil
}

type reportChecked struct{}

func (reportChecked) LatestPerSubject(context.Context, id.ID, id.ID) ([]entquery.CheckedAt, error) {
	return nil, nil
}

type reportPermits struct{}

func (reportPermits) Permitted(context.Context, id.ID, id.ID, []entquery.Subject) (map[entquery.Subject]bool, error) {
	return nil, nil
}

// This crosses the actual entity/finding/note SQL adapters. A small in-memory
// board cannot reveal either the old caps or a missing SQL workspace predicate.
func TestReportSectionsAreCompleteAndFindingsBelongToTheSelectedTarget(t *testing.T) {
	p := testx.Postgres(t,
		testx.Schema{Name: entpg.Schema, Migrations: entpg.Migrations},
		testx.Schema{Name: findingpg.Schema, Migrations: findingpg.Migrations},
		testx.Schema{Name: notepg.Schema, Migrations: notepg.Migrations},
	)
	ctx := context.Background()
	at := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	workspace, target, otherTarget, otherWorkspace := reportTestID(1), reportTestID(2), reportTestID(3), reportTestID(4)
	entities, findings, notes := entpg.NewStore(p), findingpg.NewStore(p), notepg.NewStore(p)
	graph := entquery.NewGraph(entities, reportPermits{}, reportChecks{}, reportChecked{})
	s := sections{graph: graph, findings: findingquery.NewFindings(findings), engagementNotes: engagementNotes{notes: notequery.NewNotes(notes)}}
	newRoot := func(space, target id.ID, n int) entdomain.Entity {
		root, err := entdomain.NewEntity(reportTestID(n), space, "org", fmt.Sprintf("target-%d", n), at)
		if err != nil {
			t.Fatal(err)
		}
		root = root.Root(target)
		if err := entities.CreateEntity(ctx, root); err != nil {
			t.Fatal(err)
		}
		return root
	}
	root := newRoot(workspace, target, 10)
	other := newRoot(workspace, otherTarget, 11)
	foreign := newRoot(otherWorkspace, reportTestID(5), 12)
	addFragment := func(root entdomain.Entity, n int, kind string, proposed bool) entdomain.Fragment {
		f, err := entdomain.NewFragment(reportTestID(n), root.WorkspaceID, kind, fmt.Sprintf("subject-%d.example", n), entdomain.Manual, at)
		if err != nil {
			t.Fatal(err)
		}
		f, _, err = entities.Upsert(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		claimant, confidence := entdomain.ByHuman, 0.0
		if proposed {
			claimant, confidence = entdomain.ByModel, 0.5
		}
		a, err := entdomain.Propose(reportTestID(n+100000), root.WorkspaceID, root.ID, f.ID, claimant, reportTestID(99), confidence, proposed, "reviewed source", at)
		if err != nil {
			t.Fatal(err)
		}
		if err := entities.Attribute(ctx, a); err != nil {
			t.Fatal(err)
		}
		return f
	}
	addFinding := func(f entdomain.Fragment, n int, dismissed bool) {
		finding, err := findingdomain.New(reportTestID(n), f.WorkspaceID, reportTestID(20), f.ID, fmt.Sprintf("claim-%d", n), f.Kind, f.Value, findingdomain.SeverityHigh, "source states this", reportTestID(21), reportTestID(22), reportTestID(23), at, at)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := findings.Record(ctx, finding); err != nil {
			t.Fatal(err)
		}
		if dismissed {
			finding, err = finding.Dismiss(reportTestID(99), "reviewed and excluded", at)
			if err != nil {
				t.Fatal(err)
			}
			if err := findings.Save(ctx, finding); err != nil {
				t.Fatal(err)
			}
		}
	}
	// 1,001 assets, 501 findings and 501 notes cross each previous page cap.
	for n := 0; n <= entquery.MaxPage; n++ {
		f := addFragment(root, 1000+n, "host", false)
		if n <= findingquery.MaxPage {
			addFinding(f, 10000+n, false)
		}
	}
	// Findings may be on a non-asset kind and still belong to the target.
	addFinding(addFragment(root, 3000, "document", false), 30000, false)
	addFinding(addFragment(root, 3001, "document", false), 30001, true)
	addFinding(addFragment(root, 3002, "document", true), 30002, false)
	addFinding(addFragment(other, 3003, "host", false), 30003, false)
	addFinding(addFragment(other, 3004, "host", false), 30004, true)
	addFinding(addFragment(foreign, 3005, "host", false), 30005, false)
	for n := 0; n <= notequery.MaxPage; n++ {
		note, err := notedomain.New(reportTestID(40000+n), workspace, reportTestID(99), "", "", fmt.Sprintf("Summary %d", n), at)
		if err != nil {
			t.Fatal(err)
		}
		if err := notes.Create(ctx, note); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.InSnapshot(ctx, func(ctx context.Context) error {
		assets, err := s.Assets(ctx, workspace, target)
		if err != nil || len(assets) != 1001 {
			t.Fatalf("assets: %d, %v", len(assets), err)
		}
		claims, err := s.Attribution(ctx, workspace, target)
		if err != nil || len(claims) != 1001 {
			t.Fatalf("attribution: %d, %v", len(claims), err)
		}
		cells, err := s.Coverage(ctx, workspace, target)
		if err != nil || len(cells) != 1001 {
			t.Fatalf("coverage: %d, %v", len(cells), err)
		}
		found, dismissed, err := s.Findings(ctx, workspace, target)
		if err != nil || len(found) != 502 || dismissed != 1 {
			t.Fatalf("target findings: %d open, %d dismissed, %v", len(found), dismissed, err)
		}
		for _, f := range found {
			if f.Signature == "claim-30002" || f.Signature == "claim-30003" || f.Signature == "claim-30005" {
				t.Fatalf("unattributed or unrelated finding included: %+v", f)
			}
		}
		empty, excluded, err := s.Findings(ctx, otherWorkspace, target)
		if err != nil || len(empty) != 0 || excluded != 0 {
			t.Fatalf("workspace isolation: %d/%d, %v", len(empty), excluded, err)
		}
		summary, err := s.Notes(ctx, workspace, target)
		if err != nil || len(summary) != 501 {
			t.Fatalf("summary notes: %d, %v", len(summary), err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
