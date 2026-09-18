package command_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/source/app/command"
	"github.com/0xsj/overwatch-backend/internal/source/domain"
	"github.com/0xsj/overwatch-backend/pkg/blob"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type transactionKey struct{}
type sourceStore struct {
	sources      []domain.Source
	captures     []domain.Capture
	events       []events.Event
	publishError error
}

func (s *sourceStore) InTx(ctx context.Context, fn func(context.Context) error) error {
	n, c, e := len(s.sources), len(s.captures), len(s.events)
	err := fn(context.WithValue(ctx, transactionKey{}, true))
	if err != nil {
		s.sources = s.sources[:n]
		s.captures = s.captures[:c]
		s.events = s.events[:e]
	}
	return err
}
func (s *sourceStore) CreateSource(ctx context.Context, in domain.Source) error {
	s.sources = append(s.sources, in)
	return nil
}
func (s *sourceStore) CreateCapture(ctx context.Context, in domain.Capture) error {
	s.captures = append(s.captures, in)
	return nil
}

func (s *sourceStore) SetRetention(context.Context, id.ID, id.ID, id.ID, *time.Time, time.Time) error {
	return nil
}
func (s *sourceStore) NextVersion(ctx context.Context, workspace, source id.ID) (int, error) {
	found := false
	for _, one := range s.sources {
		if one.ID == source && one.WorkspaceID == workspace {
			found = true
		}
	}
	if !found {
		return 0, domain.ErrNotFound
	}
	version := 1
	for _, one := range s.captures {
		if one.SourceID == source && one.Version >= version {
			version = one.Version + 1
		}
	}
	return version, nil
}
func (s *sourceStore) Publish(ctx context.Context, event ...events.Event) error {
	if ctx.Value(transactionKey{}) != true {
		return errors.New("publication escaped the business transaction")
	}
	if s.publishError != nil {
		return s.publishError
	}
	s.events = append(s.events, event...)
	return nil
}
func setup(t *testing.T) (*command.Sources, *sourceStore, *blob.Store, *id.Sequence) {
	t.Helper()
	clock := clock.NewFake(time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC))
	ids := id.NewSequence(clock.Now())
	repo := &sourceStore{}
	blobs, err := blob.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return command.NewSources(repo, blobs, repo, repo, ids, clock), repo, blobs, ids
}
func TestSourceAndInitialCaptureRollbackWhenAuditFails(t *testing.T) {
	service, repo, _, ids := setup(t)
	repo.publishError = errors.New("outbox unavailable")
	content := "retained source"
	out, err := service.Create(context.Background(), ids.NewID(), ids.NewID(), command.Draft{Title: "Report", Origin: "paste", Content: &content})
	if !errors.Is(err, repo.publishError) || !out.ID.IsZero() || len(repo.sources) != 0 || len(repo.captures) != 0 {
		t.Fatalf("partial create: %#v, sources=%d captures=%d err=%v", out, len(repo.sources), len(repo.captures), err)
	}
}
func TestCaptureVersionsRetainOldMetadataAndFailedAppendDoesNotConsumeVersion(t *testing.T) {
	service, repo, _, ids := setup(t)
	ctx := context.Background()
	ws, author := ids.NewID(), ids.NewID()
	content := " first capture\r\n🧭 "
	source, err := service.Create(ctx, ws, author, command.Draft{Title: "Report", Origin: "paste", Content: &content})
	if err != nil {
		t.Fatal(err)
	}
	initial := *source.LatestCapture
	repo.publishError = errors.New("outbox unavailable")
	if _, err := service.AddCapture(ctx, ws, source.ID, author, "text/plain", "failed version"); err == nil {
		t.Fatal("expected publication failure")
	}
	repo.publishError = nil
	next, err := service.AddCapture(ctx, ws, source.ID, author, "text/plain", "second capture")
	if err != nil {
		t.Fatal(err)
	}
	if next.Version != 2 || repo.captures[0] != initial || initial.SHA256 == next.SHA256 || len(repo.events) != 2 {
		t.Fatalf("versions/audit: initial=%+v next=%+v events=%d", initial, next, len(repo.events))
	}
	if _, err := service.AddCapture(ctx, ids.NewID(), source.ID, author, "text/plain", "cross-workspace"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("wrong workspace: %v", err)
	}
	if len(repo.captures) != 2 {
		t.Fatal("wrong-workspace append wrote metadata")
	}
}
func TestReferenceStartsWithoutBytesAndCanReceiveFirstCapture(t *testing.T) {
	service, repo, _, ids := setup(t)
	ctx := context.Background()
	ws, author := ids.NewID(), ids.NewID()
	source, err := service.Create(ctx, ws, author, command.Draft{Title: "Public page", Origin: "reference", URL: "https://example.test/report"})
	if err != nil {
		t.Fatal(err)
	}
	if source.LatestCapture != nil || len(repo.captures) != 0 {
		t.Fatal("reference invented capture")
	}
	capture, err := service.AddCapture(ctx, ws, source.ID, author, "text/plain", "manually retained")
	if err != nil || capture.Version != 1 {
		t.Fatalf("capture=%+v err=%v", capture, err)
	}
}
func TestImportRetainsValidatedBinaryCapture(t *testing.T) {
	service, repo, _, ids := setup(t)
	content := []byte("%PDF-1.7\nretained")

	source, err := service.Create(context.Background(), ids.NewID(), ids.NewID(), command.Draft{
		Title:        "PDF notice",
		Origin:       "import",
		Filename:     "notice.pdf",
		MediaType:    "application/pdf",
		ContentBytes: content,
	})
	if err != nil {
		t.Fatal(err)
	}
	if source.LatestCapture == nil || source.LatestCapture.MediaType != "application/pdf" {
		t.Fatalf("capture=%+v", source.LatestCapture)
	}
	if len(repo.captures) != 1 || repo.captures[0].Bytes != int64(len(content)) {
		t.Fatalf("captures=%+v", repo.captures)
	}
}
