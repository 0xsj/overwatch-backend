package query

import (
	"context"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/journal/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type logReader struct {
	rows  []domain.Line
	limit int32
}

func (r *logReader) ForWorkspace(_ context.Context, _ string, _ time.Time, _ id.ID, limit int32) ([]domain.Line, error) {
	r.limit = limit
	if int(limit) < len(r.rows) {
		return r.rows[:limit], nil
	}
	return r.rows, nil
}

func TestForWorkspaceUsesOverfetchAndReturnsKeysetCursor(t *testing.T) {
	first, _ := id.Parse("018f0e12-0000-7000-8000-000000000001")
	second, _ := id.Parse("018f0e12-0000-7000-8000-000000000002")
	third, _ := id.Parse("018f0e12-0000-7000-8000-000000000003")
	now := time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC)
	reader := &logReader{rows: []domain.Line{
		{ID: first, Action: "run.started", OccurredAt: now},
		{ID: second, Action: "invocation.refused", OccurredAt: now.Add(-time.Second)},
		{ID: third, Action: "run.finished", OccurredAt: now.Add(-2 * time.Second)},
	}}

	page, err := NewLog(reader).ForWorkspace(context.Background(), "workspace-1", Cursor{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if reader.limit != 3 {
		t.Fatalf("reader limit = %d, want overfetch of 3", reader.limit)
	}
	if !page.More || len(page.Records) != 2 {
		t.Fatalf("page = %#v, want two records and more", page)
	}
	if page.Next.ID != second || !page.Next.OccurredAt.Equal(now.Add(-time.Second)) {
		t.Fatalf("next = %#v, want the second record", page.Next)
	}
}
