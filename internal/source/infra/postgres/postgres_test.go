package postgres_test

import (
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/source/app/command"
	"github.com/0xsj/overwatch-backend/internal/source/app/query"
	"github.com/0xsj/overwatch-backend/internal/source/domain"
	sourcepg "github.com/0xsj/overwatch-backend/internal/source/infra/postgres"
	"github.com/0xsj/overwatch-backend/pkg/blob"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/outbox"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
	"github.com/0xsj/overwatch-backend/pkg/testx"
)

type publication struct {
	publisher events.Publisher
	fail      bool
}

func (p *publication) Publish(ctx context.Context, events ...events.Event) error {
	if err := p.publisher.Publish(ctx, events...); err != nil {
		return err
	}
	if p.fail {
		return errors.New("failure after audit insertion")
	}
	return nil
}

type fixture struct {
	pool       *postgres.Pool
	store      *sourcepg.Store
	blobs      *blob.Store
	writes     *command.Sources
	reads      *query.Sources
	pub        *publication
	ids        *id.Sequence
	ws, author id.ID
}

func setup(t *testing.T) fixture {
	t.Helper()
	pool := testx.Postgres(t, testx.Schema{Name: sourcepg.Schema, Migrations: sourcepg.Migrations}, testx.Schema{Migrations: outbox.Migrations, Unqualified: true})
	now := clock.NewFake(time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))
	ids := id.NewSequence(now.Now())
	store := sourcepg.NewStore(pool)
	blobs, err := blob.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pub := &publication{publisher: outbox.NewPublisher(outbox.NewPostgres(pool))}
	return fixture{pool: pool, store: store, blobs: blobs, writes: command.NewSources(store, blobs, pool, pub, ids, now), reads: query.NewSources(store, blobs), pub: pub, ids: ids, ws: ids.NewID(), author: ids.NewID()}
}
func (f fixture) create(t *testing.T, content string) domain.Summary {
	t.Helper()
	s, err := f.writes.Create(context.Background(), f.ws, f.author, command.Draft{Title: "Field report", Origin: "paste", Content: &content})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestPostgresSourceInitialCaptureAndOutboxRollbackTogether(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.pub.fail = true
	content := "retained text"
	if _, err := f.writes.Create(ctx, f.ws, f.author, command.Draft{Title: "Report", Origin: "paste", Content: &content}); err == nil {
		t.Fatal("expected forced audit failure")
	}
	for _, table := range []string{"source.source", "source.capture", "outbox"} {
		if got := testx.Count(t, f.pool, "select count(*) from "+table); got != 0 {
			t.Fatalf("%s retained %d rows after failure", table, got)
		}
	}
	f.pub.fail = false
	source := f.create(t, content)
	f.pub.fail = true
	if _, err := f.writes.AddCapture(ctx, f.ws, source.ID, f.author, "text/plain", "second"); err == nil {
		t.Fatal("expected append failure")
	}
	f.pub.fail = false
	next, err := f.writes.AddCapture(ctx, f.ws, source.ID, f.author, "text/plain", "third")
	if err != nil || next.Version != 2 {
		t.Fatalf("append after rollback: %+v err=%v", next, err)
	}
	if got := testx.Count(t, f.pool, "select count(*) from outbox"); got != 2 {
		t.Fatalf("audit count=%d", got)
	}
}
func TestPostgresConcurrentAppendsAllocateUniqueConsecutiveVersions(t *testing.T) {
	f := setup(t)
	source := f.create(t, "original")
	const count = 8
	versions := make([]int, count)
	errs := make([]error, count)
	var group sync.WaitGroup
	for i := range count {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			got, err := f.writes.AddCapture(context.Background(), f.ws, source.ID, f.author, "text/plain", "same retained bytes")
			versions[i], errs[i] = got.Version, err
		}(i)
	}
	group.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Ints(versions)
	for i, version := range versions {
		if version != i+2 {
			t.Fatalf("versions=%v", versions)
		}
	}
	original, err := f.reads.Content(context.Background(), f.ws, source.ID, source.LatestCapture.ID)
	if err != nil || original.Content != "original" || original.Version != 1 {
		t.Fatalf("old version changed: %+v err=%v", original, err)
	}
	detail, err := f.reads.Read(context.Background(), f.ws, source.ID)
	if err != nil || len(detail.Captures) != count+1 || detail.Source.LatestCapture.Version != count+1 {
		t.Fatalf("capture list: %+v err=%v", detail, err)
	}
}
func TestPostgresSourcePaginationIsolationAndExactContent(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	first := f.create(t, "🧭 exact\r\n\x00text")
	f.create(t, "second")
	third := f.create(t, "third")
	page, err := f.reads.List(ctx, f.ws, id.Nil, "", 2)
	if err != nil || len(page.Items) != 2 || page.NextCursor == nil || page.Items[0].ID != third.ID {
		t.Fatalf("first page: %+v err=%v", page, err)
	}
	last, err := f.reads.List(ctx, f.ws, *page.NextCursor, "", 2)
	if err != nil || len(last.Items) != 1 || last.Items[0].ID != first.ID || last.NextCursor != nil {
		t.Fatalf("last page: %+v err=%v", last, err)
	}
	content, err := f.reads.Content(ctx, f.ws, first.ID, first.LatestCapture.ID)
	if err != nil || content.Content != "🧭 exact\r\n\x00text" {
		t.Fatalf("content=%q err=%v", content.Content, err)
	}
	for _, args := range [][3]id.ID{{f.ids.NewID(), first.ID, first.LatestCapture.ID}, {f.ws, third.ID, first.LatestCapture.ID}} {
		if _, err := f.reads.Content(ctx, args[0], args[1], args[2]); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("cross-boundary capture read: %v", err)
		}
	}
	if _, err := f.writes.AddCapture(ctx, f.ids.NewID(), first.ID, f.author, "text/plain", "wrong workspace"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-workspace append: %v", err)
	}
	// Content-addressed metadata alone is not proof the bytes still match.
	corrupt := query.NewSources(f.store, corruptedBytes{})
	if _, err := corrupt.Content(ctx, f.ws, first.ID, first.LatestCapture.ID); !errors.Is(err, blob.ErrCorrupted) {
		t.Fatalf("corrupt bytes: %v", err)
	}
}

type corruptedBytes struct{}

func (corruptedBytes) Open(context.Context, blob.Ref) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("changed")), nil
}
