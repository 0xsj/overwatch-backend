package query_test

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/run/app/query"
	"github.com/0xsj/overwatch-backend/internal/run/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type pagedRuns struct {
	query.Reader
	rows   []domain.Run
	calls  int
	fail   error
	repeat bool
}

func runID(n int) id.ID {
	var out id.ID
	binary.BigEndian.PutUint64(out[8:], uint64(n))
	return out
}

func (s *pagedRuns) Page(_ context.Context, workspace, target id.ID, before time.Time, beforeID id.ID, limit int) ([]domain.Run, error) {
	s.calls++
	if s.calls > 1 && s.fail != nil {
		return nil, s.fail
	}
	out := []domain.Run{}
	for _, row := range s.rows {
		if row.WorkspaceID != workspace || row.TargetID != target {
			continue
		}
		if !s.repeat && !before.IsZero() && (row.StartedAt.After(before) || (row.StartedAt.Equal(before) && row.ID.String() >= beforeID.String())) {
			continue
		}
		out = append(out, row)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

type noRunBytes struct{}

func (noRunBytes) Open(context.Context, string) (io.ReadCloser, error) { panic("not used") }

func runPages(count int) *pagedRuns {
	s := &pagedRuns{}
	at := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	for n := count; n > 0; n-- {
		s.rows = append(s.rows, domain.Run{ID: runID(n), WorkspaceID: runID(1000), TargetID: runID(1001), StartedAt: at})
	}
	return s
}

func TestAllRunsExhaustsPagesIncludingTiedTimestamps(t *testing.T) {
	for _, count := range []int{0, query.MaxPage, query.MaxPage + 1, query.MaxPage*2 + 1} {
		s := runPages(count)
		q := query.NewRuns(s, noRunBytes{})
		got, err := q.All(context.Background(), runID(1000), runID(1001))
		if err != nil || len(got) != count {
			t.Fatalf("%d rows: got %d, %v", count, len(got), err)
		}
		seen := map[id.ID]bool{}
		for _, row := range got {
			if seen[row.ID] {
				t.Fatalf("duplicate run %s", row.ID)
			}
			seen[row.ID] = true
		}
		if s.calls != count/query.MaxPage+1 {
			t.Fatalf("did not exhaust cursor at boundary: %d calls for %d", s.calls, count)
		}
	}
}

func TestAllRunsDoesNotReturnAPartialReportAfterPageFailure(t *testing.T) {
	s := runPages(query.MaxPage + 1)
	s.fail = errors.New("page failed")
	q := query.NewRuns(s, noRunBytes{})
	got, err := q.All(context.Background(), runID(1000), runID(1001))
	if !errors.Is(err, s.fail) || got != nil {
		t.Fatalf("partial result returned: %d rows, %v", len(got), err)
	}
}

func TestAllRunsRefusesAStuckCursor(t *testing.T) {
	s := runPages(query.MaxPage)
	s.repeat = true
	q := query.NewRuns(s, noRunBytes{})
	if _, err := q.All(context.Background(), runID(1000), runID(1001)); err == nil {
		t.Fatal("repeated page should fail instead of looping")
	}
}
