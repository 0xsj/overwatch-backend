package query_test

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/0xsj/overwatch-backend/internal/researchentity/app/query"
	"github.com/0xsj/overwatch-backend/internal/researchentity/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type recordPageReader struct {
	rows       []domain.Record
	limits     []int
	workspaces []id.ID
}

func (r *recordPageReader) Page(_ context.Context, workspace, before id.ID, _ string, _ domain.Kind, _ domain.CitationFilter, _ domain.ResolutionFilter, _ domain.ArchiveFilter, limit int, _ string) ([]domain.Record, error) {
	r.limits = append(r.limits, limit)
	r.workspaces = append(r.workspaces, workspace)
	visible := make([]domain.Record, 0, len(r.rows))
	for _, row := range r.rows {
		if row.WorkspaceID == workspace {
			visible = append(visible, row)
		}
	}
	start := 0
	if !before.IsZero() {
		for index, row := range visible {
			if row.ID == before {
				start = index + 1
				break
			}
		}
	}
	if start >= len(visible) {
		return []domain.Record{}, nil
	}
	end := start + limit
	if end > len(visible) {
		end = len(visible)
	}
	return visible[start:end], nil
}

func (*recordPageReader) Summary(context.Context, id.ID, string) (domain.BrowseSummary, error) {
	return domain.BrowseSummary{}, nil
}

func (r *recordPageReader) ByIDVisible(_ context.Context, workspace, want id.ID, _ string) (domain.Record, error) {
	for _, row := range r.rows {
		if row.WorkspaceID == workspace && row.ID == want {
			return row, nil
		}
	}
	return domain.Record{}, nil
}

func (*recordPageReader) Revisions(context.Context, id.ID, id.ID, string) ([]domain.Revision, error) {
	return []domain.Revision{}, nil
}

func recordID(n int) id.ID {
	var out id.ID
	binary.BigEndian.PutUint64(out[8:], uint64(n))
	return out
}

func TestListPagesLargeRecordSetWithoutDuplicatesOrUnboundedReads(t *testing.T) {
	workspace := recordID(1_000_000)
	otherWorkspace := recordID(2_000_000)
	const total = 10_001
	reader := &recordPageReader{}
	reader.rows = append(reader.rows,
		domain.Record{ID: recordID(total + 1), WorkspaceID: otherWorkspace, Kind: domain.Account, Name: "foreign account"},
		domain.Record{ID: recordID(total + 2), WorkspaceID: otherWorkspace, Kind: domain.Account, Name: "another foreign account"},
	)
	for n := total; n > 0; n-- {
		reader.rows = append(reader.rows, domain.Record{ID: recordID(n), WorkspaceID: workspace, Kind: domain.Account, Name: "account"})
	}

	service := query.NewRecords(reader)
	seen := make(map[id.ID]struct{}, total)
	var before id.ID
	pages := 0
	for {
		page, err := service.List(context.Background(), workspace, before, "", "", domain.CitationAny, domain.ResolutionAny, domain.ArchiveActive, 1_000, "internal")
		if err != nil {
			t.Fatal(err)
		}
		pages++
		if len(page.Items) > query.MaxPage {
			t.Fatalf("page exceeded max size: %d", len(page.Items))
		}
		for _, row := range page.Items {
			if row.WorkspaceID != workspace {
				t.Fatalf("record from workspace %s leaked into workspace %s", row.WorkspaceID, workspace)
			}
			if _, exists := seen[row.ID]; exists {
				t.Fatalf("record repeated across pages: %s", row.ID)
			}
			seen[row.ID] = struct{}{}
		}
		if page.NextCursor == nil {
			break
		}
		before = *page.NextCursor
	}

	if len(seen) != total {
		t.Fatalf("paged %d records, want %d", len(seen), total)
	}
	if pages != (total+query.MaxPage-1)/query.MaxPage {
		t.Fatalf("page count = %d, want %d", pages, (total+query.MaxPage-1)/query.MaxPage)
	}
	for _, limit := range reader.limits {
		if limit != query.MaxPage+1 {
			t.Fatalf("reader received limit %d, want bounded overfetch %d", limit, query.MaxPage+1)
		}
	}
	for _, requestedWorkspace := range reader.workspaces {
		if requestedWorkspace != workspace {
			t.Fatalf("reader received workspace %s, want %s", requestedWorkspace, workspace)
		}
	}
}
