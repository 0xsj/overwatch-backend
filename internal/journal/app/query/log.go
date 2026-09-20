package query

import (
	"context"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/journal/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// WorkspaceReader is the journal's bounded read side. It deliberately takes a
// workspace string rather than an id: journal rows may describe work before a
// workspace aggregate has been materialised, and the root owns the permission
// check before this reader is called.
type WorkspaceReader interface {
	ForWorkspace(ctx context.Context, workspace string, after time.Time, afterID id.ID, limit int32) ([]domain.Line, error)
}

// Cursor names the last line a reader saw. Zero means "from the head".
type Cursor struct {
	OccurredAt time.Time
	ID         id.ID
}

// Record is the causal journal's screen-shaped view. It keeps the provenance
// fields that audit intentionally drops: origin, depth, attempt, decision and
// causation are how a reader distinguishes work from a choice and follows the
// path that led here.
type Record struct {
	ID          id.ID
	Action      string
	Subject     string
	Origin      string
	Actor       string
	OnBehalfOf  string
	WorkspaceID string
	Depth       int
	Attempt     int
	Decision    bool
	Correlation id.ID
	Causation   id.ID
	Detail      []byte
	OccurredAt  time.Time
	RecordedAt  time.Time
}

type Page struct {
	Records []Record
	Next    Cursor
	More    bool
}

type Log struct{ reader WorkspaceReader }

func NewLog(reader WorkspaceReader) *Log {
	if reader == nil {
		panic("journal: NewLog with a nil reader")
	}
	return &Log{reader: reader}
}

// ForWorkspace returns newest work first. Access is deliberately outside this
// package: the composition root resolves the owning org and applies the grant
// before a line is readable.
func (l *Log) ForWorkspace(ctx context.Context, workspace string, after Cursor, size int) (Page, error) {
	if workspace == "" {
		return Page{}, domain.ErrLineGone
	}
	limit := clamp(size)
	found, err := l.reader.ForWorkspace(ctx, workspace, after.OccurredAt, after.ID, limit+1)
	if err != nil {
		return Page{}, fmt.Errorf("journal: for workspace: %w", err)
	}

	more := len(found) > int(limit)
	if more {
		found = found[:limit]
	}
	out := Page{Records: make([]Record, 0, len(found)), More: more}
	for _, line := range found {
		out.Records = append(out.Records, Record{
			ID: line.ID, Action: line.Action, Subject: line.Subject,
			Origin: line.Origin, Actor: line.Actor, OnBehalfOf: line.OnBehalfOf,
			WorkspaceID: line.WorkspaceID, Depth: line.Depth, Attempt: line.Attempt,
			Decision: line.Decision, Correlation: line.Correlation,
			Causation: line.Causation, Detail: line.Detail,
			OccurredAt: line.OccurredAt, RecordedAt: line.RecordedAt,
		})
	}
	if len(out.Records) > 0 {
		last := out.Records[len(out.Records)-1]
		out.Next = Cursor{OccurredAt: last.OccurredAt, ID: last.ID}
	}
	return out, nil
}

const (
	defaultLogPage = 50
	maxLogPage     = 200
)

func clamp(size int) int32 {
	switch {
	case size <= 0:
		return defaultLogPage
	case size > maxLogPage:
		return maxLogPage
	default:
		return int32(size)
	}
}
