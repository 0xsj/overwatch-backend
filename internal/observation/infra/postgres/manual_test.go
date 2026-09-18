package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/observation/app/command"
	"github.com/0xsj/overwatch-backend/internal/observation/app/query"
	"github.com/0xsj/overwatch-backend/internal/observation/domain"
	observationpg "github.com/0xsj/overwatch-backend/internal/observation/infra/postgres"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/outbox"
	"github.com/0xsj/overwatch-backend/pkg/testx"
)

type capturePort struct{ capture command.RetainedCapture }

func (p capturePort) Retained(context.Context, id.ID, id.ID, id.ID, id.ID) (command.RetainedCapture, error) {
	return p.capture, nil
}

type manualPublication struct {
	publisher events.Publisher
	fail      bool
}

func (p *manualPublication) Publish(ctx context.Context, events ...events.Event) error {
	if err := p.publisher.Publish(ctx, events...); err != nil {
		return err
	}
	if p.fail {
		return errors.New("failure after audit insertion")
	}
	return nil
}
func TestPostgresManualCitationRollbackPaginationAndExactUnicode(t *testing.T) {
	pool := testx.Postgres(t, testx.Schema{Name: observationpg.Schema, Migrations: observationpg.Migrations}, testx.Schema{Migrations: outbox.Migrations, Unqualified: true})
	ctx := context.Background()
	clock := clock.NewFake(time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))
	ids := id.NewSequence(clock.Now())
	ws, source, capture, author := ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID()
	port := capturePort{command.RetainedCapture{WorkspaceID: ws, SourceID: source, CaptureID: capture, MediaType: "text/plain", Content: "🧭 first \x00 then café"}}
	store := observationpg.NewStore(pool)
	pub := &manualPublication{publisher: outbox.NewPublisher(outbox.NewPostgres(pool)), fail: true}
	writes := command.NewManualObservations(store, port, pool, pub, ids, clock)
	reads := query.NewManualObservations(store)
	draft := command.ManualDraft{CaptureID: capture, Statement: "Source contains exact text", Quote: "\x00"}
	if _, err := writes.Record(ctx, ws, source, author, draft); err == nil {
		t.Fatal("expected forced publication failure")
	}
	if testx.Count(t, pool, "select count(*) from observation.manual") != 0 || testx.Count(t, pool, "select count(*) from outbox") != 0 {
		t.Fatal("citation or audit survived rollback")
	}
	pub.fail = false
	first, err := writes.Record(ctx, ws, source, author, draft)
	if err != nil {
		t.Fatal(err)
	}
	draft.Quote = "café"
	for range 2 {
		if _, err := writes.Record(ctx, ws, source, author, draft); err != nil {
			t.Fatal(err)
		}
	}
	page, err := reads.ForSource(ctx, ws, source, id.Nil, 2)
	if err != nil || len(page.Items) != 2 || page.NextCursor == nil {
		t.Fatalf("first page: %+v err=%v", page, err)
	}
	last, err := reads.ForSource(ctx, ws, source, *page.NextCursor, 2)
	if err != nil || len(last.Items) != 1 || last.Items[0].ID != first.ID || last.Items[0].CaptureID != first.CaptureID || !last.Items[0].RecordedAt.Equal(first.RecordedAt) || last.Items[0].Quote != "\x00" || last.NextCursor != nil {
		t.Fatalf("exact retained citation: %+v err=%v", last, err)
	}
	exact, err := reads.ByID(ctx, ws, source, first.ID)
	if err != nil || exact.ID != first.ID || exact.Quote != first.Quote || exact.CaptureID != capture {
		t.Fatalf("citation lookup: %+v err=%v", exact, err)
	}
	for _, args := range [][3]id.ID{{ids.NewID(), source, first.ID}, {ws, ids.NewID(), first.ID}, {ws, source, ids.NewID()}} {
		if _, err := reads.ByID(ctx, args[0], args[1], args[2]); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("citation lookup escaped boundary: %v", err)
		}
	}
	if testx.Count(t, pool, "select count(*) from outbox") != 3 {
		t.Fatal("missing citation audit")
	}
	for _, args := range [][2]id.ID{{ids.NewID(), source}, {ws, ids.NewID()}} {
		page, err := reads.ForSource(ctx, args[0], args[1], id.Nil, 2)
		if err != nil || len(page.Items) != 0 || page.NextCursor != nil {
			t.Fatalf("citation isolation: %+v err=%v", page, err)
		}
	}
}
