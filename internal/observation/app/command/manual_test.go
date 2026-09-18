package command_test

import (
	"context"
	"errors"
	"testing"

	"github.com/0xsj/overwatch-backend/internal/observation/app/command"
	"github.com/0xsj/overwatch-backend/internal/observation/domain"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type manualTxKey struct{}
type manualStore struct {
	rows       []domain.Manual
	eventCount int
	failure    error
}

func (s *manualStore) CreateManual(ctx context.Context, in domain.Manual) error {
	s.rows = append(s.rows, in)
	return nil
}
func (s *manualStore) InTx(ctx context.Context, fn func(context.Context) error) error {
	n, e := len(s.rows), s.eventCount
	err := fn(context.WithValue(ctx, manualTxKey{}, true))
	if err != nil {
		s.rows = s.rows[:n]
		s.eventCount = e
	}
	return err
}
func (s *manualStore) Publish(ctx context.Context, event ...events.Event) error {
	if ctx.Value(manualTxKey{}) != true {
		return errors.New("event outside transaction")
	}
	if s.failure != nil {
		return s.failure
	}
	s.eventCount += len(event)
	return nil
}

type retained struct{ value command.RetainedCapture }

func (r *retained) Retained(context.Context, id.ID, id.ID, id.ID, id.ID) (command.RetainedCapture, error) {
	return r.value, nil
}
func TestManualObservationRejectsPortProvenanceMismatchBeforeWriting(t *testing.T) {
	ctx := context.Background()
	ids := id.NewSequence(at)
	ws, source, capture, author := ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID()
	port := &retained{command.RetainedCapture{WorkspaceID: ws, SourceID: source, CaptureID: capture, MediaType: "text/plain", Content: "passage"}}
	repo := &manualStore{}
	service := command.NewManualObservations(repo, port, repo, repo, ids, clock.NewFake(at))
	draft := command.ManualDraft{CaptureID: capture, Statement: "Source says passage", Quote: "passage"}
	original := port.value
	for _, field := range []string{"workspace", "source", "capture"} {
		port.value = original
		switch field {
		case "workspace":
			port.value.WorkspaceID = ids.NewID()
		case "source":
			port.value.SourceID = ids.NewID()
		case "capture":
			port.value.CaptureID = ids.NewID()
		}
		if _, err := service.Record(ctx, ws, source, author, draft); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("%s mismatch: %v", field, err)
		}
	}
	if len(repo.rows) != 0 || repo.eventCount != 0 {
		t.Fatal("mismatched capture persisted")
	}
	port.value = original
	draft.Quote = "not in source"
	if _, err := service.Record(ctx, ws, source, author, draft); !errors.Is(err, domain.ErrCitation) {
		t.Fatalf("absent quote: %v", err)
	}
}
func TestManualObservationAndAuditRollbackTogether(t *testing.T) {
	ctx := context.Background()
	ids := id.NewSequence(at)
	ws, source, capture, author := ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID()
	port := &retained{command.RetainedCapture{WorkspaceID: ws, SourceID: source, CaptureID: capture, MediaType: "text/plain", Content: "🧭 cited passage"}}
	repo := &manualStore{failure: errors.New("outbox down")}
	service := command.NewManualObservations(repo, port, repo, repo, ids, clock.NewFake(at))
	draft := command.ManualDraft{CaptureID: capture, Statement: "Source says passage", Quote: "passage"}
	got, err := service.Record(ctx, ws, source, author, draft)
	if !errors.Is(err, repo.failure) || !got.ID.IsZero() || len(repo.rows) != 0 {
		t.Fatalf("partial observation: %+v err=%v rows=%d", got, err, len(repo.rows))
	}
	repo.failure = nil
	got, err = service.Record(ctx, ws, source, author, draft)
	if err != nil || len(repo.rows) != 1 || repo.eventCount != 1 || got.QuoteStart != 8 {
		t.Fatalf("observation: %+v err=%v", got, err)
	}
}

func TestManualObservationCanCiteAnExtraction(t *testing.T) {
	ctx := context.Background()
	ids := id.NewSequence(at)
	ws, source, capture, extraction, author := ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID()
	port := &retained{command.RetainedCapture{WorkspaceID: ws, SourceID: source, CaptureID: capture, ExtractionID: extraction, MediaType: "text/plain", Content: "derived passage"}}
	repo := &manualStore{}
	service := command.NewManualObservations(repo, port, repo, repo, ids, clock.NewFake(at))
	got, err := service.Record(ctx, ws, source, author, command.ManualDraft{CaptureID: capture, ExtractionID: extraction, Statement: "The PDF says passage", Quote: "derived passage"})
	if err != nil || got.ExtractionID == nil || *got.ExtractionID != extraction || got.QuoteStart != 0 {
		t.Fatalf("derived citation: %+v err=%v", got, err)
	}
}
