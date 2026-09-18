package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/artifactcleanup/app"
	"github.com/0xsj/overwatch-backend/internal/artifactcleanup/domain"
	"github.com/0xsj/overwatch-backend/pkg/blob"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type cleanupRepo struct {
	items        []domain.Candidate
	references   []domain.Reference
	last         map[string]domain.SweepItem
	itemsWritten []domain.SweepItem
	review       domain.Review
	started      []domain.SweepRun
	finished     []domain.SweepRun
	failed       []domain.SweepRun
	events       []events.Event
}

func (r *cleanupRepo) Inventory(context.Context, id.ID, int) (domain.Inventory, error) {
	return domain.Inventory{Items: append([]domain.Candidate(nil), r.items...)}, nil
}

func (r *cleanupRepo) References(context.Context, id.ID, string, string, int) ([]domain.Reference, error) {
	return append([]domain.Reference(nil), r.references...), nil
}

func (r *cleanupRepo) LastSweepItems(context.Context, id.ID, int) (map[string]domain.SweepItem, error) {
	if r.last == nil {
		return map[string]domain.SweepItem{}, nil
	}
	return r.last, nil
}

func (r *cleanupRepo) OpenReview(context.Context, id.ID) (domain.Review, error) {
	if r.review.ID.IsZero() {
		return domain.Review{Status: domain.ReviewNone, Items: []domain.Candidate{}, SelectedRefs: []string{}}, nil
	}
	return r.review, nil
}

func (r *cleanupRepo) LatestReview(context.Context, id.ID) (domain.Review, error) {
	if r.review.ID.IsZero() {
		return domain.Review{Status: domain.ReviewNone, Items: []domain.Candidate{}, SelectedRefs: []string{}}, nil
	}
	return r.review, nil
}

func (r *cleanupRepo) ReadReview(_ context.Context, _ id.ID, reviewID id.ID) (domain.Review, error) {
	if r.review.ID != reviewID {
		return domain.Review{}, errors.New("review not found")
	}
	return r.review, nil
}

func (r *cleanupRepo) SaveReview(_ context.Context, review domain.Review) error {
	r.review = review
	return nil
}

func (r *cleanupRepo) RecentReviews(context.Context, id.ID, int) ([]domain.ReviewSummary, error) {
	if r.review.ID.IsZero() {
		return []domain.ReviewSummary{}, nil
	}
	var bytes int64
	for _, item := range r.review.Items {
		bytes += item.Bytes
	}
	return []domain.ReviewSummary{{ID: r.review.ID, WorkspaceID: r.review.WorkspaceID, CreatedBy: r.review.CreatedBy, UpdatedBy: r.review.UpdatedBy, Status: r.review.Status, ItemCount: len(r.review.Items), ItemBytes: bytes, CreatedAt: r.review.CreatedAt, UpdatedAt: r.review.UpdatedAt, CompletedAt: r.review.CompletedAt, SweepID: r.review.SweepID, DiscardedBy: r.review.DiscardedBy, DiscardedAt: r.review.DiscardedAt, DiscardReason: r.review.DiscardReason}}, nil
}

func (r *cleanupRepo) DiscardReview(_ context.Context, review domain.Review) error {
	r.review = review
	return nil
}

func (r *cleanupRepo) RecordSweepItem(_ context.Context, item domain.SweepItem) error {
	r.itemsWritten = append(r.itemsWritten, item)
	return nil
}

func (r *cleanupRepo) StartSweep(_ context.Context, run domain.SweepRun) error {
	r.started = append(r.started, run)
	return nil
}

func (r *cleanupRepo) FinishSweep(_ context.Context, run domain.SweepRun) error {
	r.finished = append(r.finished, run)
	return nil
}

func (r *cleanupRepo) FailSweep(_ context.Context, run domain.SweepRun) error {
	r.failed = append(r.failed, run)
	return nil
}

func (r *cleanupRepo) RecentSweeps(context.Context, id.ID, int) ([]domain.SweepRun, error) {
	return []domain.SweepRun{}, nil
}

func (r *cleanupRepo) Publish(_ context.Context, event ...events.Event) error {
	r.events = append(r.events, event...)
	return nil
}

type cleanupBlobs struct {
	info      blob.Info
	verifyErr error
	removed   bool
}

func (b *cleanupBlobs) Stat(context.Context, blob.Ref) (blob.Info, error) {
	if b.removed {
		return blob.Info{}, blob.ErrNotFound
	}
	return b.info, nil
}

func (b *cleanupBlobs) Verify(context.Context, blob.Ref) error { return b.verifyErr }

func (b *cleanupBlobs) Remove(context.Context, blob.Ref) error {
	b.removed = true
	return nil
}

type lifecycleBlobs struct{ infos map[string]blob.Info }

func (b *lifecycleBlobs) Stat(_ context.Context, ref blob.Ref) (blob.Info, error) {
	info, ok := b.infos[ref.String()]
	if !ok {
		return blob.Info{}, blob.ErrNotFound
	}
	return info, nil
}

func (b *lifecycleBlobs) Verify(_ context.Context, ref blob.Ref) error {
	if _, ok := b.infos[ref.String()]; !ok {
		return blob.ErrNotFound
	}
	return nil
}

func (b *lifecycleBlobs) Remove(context.Context, blob.Ref) error { return nil }

func TestStatusExplainsProtectionEligibilityAndSweptBytes(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	ids := id.NewSequence(clk.Now())
	workspace := ids.NewID()
	live, err := blob.ParseRef("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	eligible, err := blob.ParseRef("sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	swept, err := blob.ParseRef("sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	if err != nil {
		t.Fatal(err)
	}
	purged := clk.Now().Add(-time.Hour)
	repo := &cleanupRepo{
		references: []domain.Reference{
			{Ref: live.String(), Kind: domain.KindSourceCapture, WorkspaceID: workspace, SourceID: ids.NewID(), Bytes: 5, ReferenceCount: 1, LiveSourceCount: 1, CreatedAt: clk.Now()},
			{Ref: eligible.String(), Kind: domain.KindSourceCapture, WorkspaceID: workspace, SourceID: ids.NewID(), Bytes: 7, ReferenceCount: 1, PurgedAt: &purged, CreatedAt: clk.Now()},
			{Ref: swept.String(), Kind: domain.KindSourceCapture, WorkspaceID: workspace, SourceID: ids.NewID(), Bytes: 9, ReferenceCount: 1, PurgedAt: &purged, CreatedAt: clk.Now()},
		},
		last: map[string]domain.SweepItem{swept.String(): {SweepID: ids.NewID(), Ref: swept.String(), Outcome: domain.OutcomeDeleted, RecordedAt: clk.Now()}},
	}
	service := app.New(repo, &lifecycleBlobs{infos: map[string]blob.Info{eligible.String(): {Ref: eligible, Size: 7}}}, repo, ids, clk)

	page, err := service.Status(context.Background(), workspace, "", "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if page.Count != 3 || page.StateCounts[domain.StateProtected] != 1 || page.StateCounts[domain.StateEligible] != 1 || page.StateCounts[domain.StateSwept] != 1 {
		t.Fatalf("unexpected lifecycle states: %+v", page)
	}
	for _, item := range page.Items {
		if item.Ref == swept.String() && item.LastSweepOutcome != domain.OutcomeDeleted {
			t.Fatalf("swept item lost its durable outcome: %+v", item)
		}
	}
	filtered, err := service.Status(context.Background(), workspace, "", eligible.String(), domain.StateEligible, 100)
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Count != 1 || filtered.Items[0].Ref != eligible.String() {
		t.Fatalf("exact reference/state filter failed: %+v", filtered)
	}
}

func TestSweepPersistsCompletedRunAndPublishesAuditEvent(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	ids := id.NewSequence(clk.Now())
	ref, err := blob.ParseRef("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	repo := &cleanupRepo{items: []domain.Candidate{{Ref: ref.String(), Kind: domain.KindSourceCapture, Bytes: 42}}}
	blobs := &cleanupBlobs{info: blob.Info{Ref: ref, Size: 42}}
	service := app.New(repo, blobs, repo, ids, clk)
	workspace, actor := ids.NewID(), ids.NewID()

	result, err := service.Sweep(context.Background(), workspace, actor, 7, nil, id.Nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.Status != domain.SweepCompleted || result.Run.Limit != 7 || result.Run.DeletedCount != 1 || result.Run.DeletedBytes != 42 {
		t.Fatalf("unexpected completed run: %+v", result.Run)
	}
	if len(repo.started) != 1 || len(repo.finished) != 1 || len(repo.failed) != 0 {
		t.Fatalf("run lifecycle: started=%d finished=%d failed=%d", len(repo.started), len(repo.finished), len(repo.failed))
	}
	if len(repo.events) != 1 || repo.events[0].Name != domain.EventSwept {
		t.Fatalf("audit events: %+v", repo.events)
	}
	if len(repo.itemsWritten) != 1 || repo.itemsWritten[0].Outcome != domain.OutcomeDeleted {
		t.Fatalf("sweep item history: %+v", repo.itemsWritten)
	}
	if !blobs.removed {
		t.Fatal("verified candidate was not removed")
	}
}

func TestSweepHonorsExplicitSelectionAndRejectsStaleReferences(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	ids := id.NewSequence(clk.Now())
	selected, err := blob.ParseRef("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	unselected, err := blob.ParseRef("sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	repo := &cleanupRepo{items: []domain.Candidate{
		{Ref: selected.String(), Kind: domain.KindSourceCapture, Bytes: 42},
		{Ref: unselected.String(), Kind: domain.KindSourceCapture, Bytes: 99},
	}}
	blobs := &cleanupBlobs{info: blob.Info{Ref: selected, Size: 42}}
	service := app.New(repo, blobs, repo, ids, clk)
	workspace, actor := ids.NewID(), ids.NewID()

	review, err := service.SaveReview(context.Background(), workspace, actor, []string{selected.String()})
	if err != nil {
		t.Fatal(err)
	}
	if review.Status != domain.ReviewOpen || review.ID.IsZero() || len(review.SelectedRefs) != 1 || review.SelectedRefs[0] != selected.String() {
		t.Fatalf("unexpected saved review: %+v", review)
	}
	result, err := service.Sweep(context.Background(), workspace, actor, 100, []string{selected.String()}, review.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.Status != domain.SweepCompleted || result.Run.ReviewID == nil || *result.Run.ReviewID != review.ID || result.Run.CandidateCount != 1 || len(result.Deleted) != 1 || result.Deleted[0].Ref != selected.String() {
		t.Fatalf("explicit selection was not applied: %+v", result)
	}
	if len(repo.itemsWritten) != 1 || repo.itemsWritten[0].Ref != selected.String() {
		t.Fatalf("unexpected sweep item history: %+v", repo.itemsWritten)
	}

	stale, err := blob.ParseRef("sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	if err != nil {
		t.Fatal(err)
	}
	result, err = service.Sweep(context.Background(), workspace, actor, 100, []string{stale.String()}, id.Nil)
	if err == nil || err.Error() != "one or more selected artifacts are no longer eligible for cleanup" {
		t.Fatalf("expected stale selection rejection, got result=%+v err=%v", result, err)
	}
	if result.Run.Status != domain.SweepFailed || len(repo.failed) != 1 || blobs.removed != true {
		t.Fatalf("stale selection lifecycle: result=%+v failed=%+v removed=%v", result, repo.failed, blobs.removed)
	}
}

func TestCleanupReviewHistoryRecordsExplicitDiscard(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	ids := id.NewSequence(clk.Now())
	ref, err := blob.ParseRef("sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")
	if err != nil {
		t.Fatal(err)
	}
	repo := &cleanupRepo{items: []domain.Candidate{{Ref: ref.String(), Kind: domain.KindSourceCapture, Bytes: 12}}}
	service := app.New(repo, &cleanupBlobs{info: blob.Info{Ref: ref, Size: 12}}, repo, ids, clk)
	workspace, actor := ids.NewID(), ids.NewID()

	review, err := service.SaveReview(context.Background(), workspace, actor, []string{ref.String()})
	if err != nil {
		t.Fatal(err)
	}
	history, err := service.ReviewHistory(context.Background(), workspace, 10)
	if err != nil || history.Count != 1 || history.Items[0].Status != domain.ReviewOpen || history.Items[0].ItemBytes != 12 {
		t.Fatalf("saved review missing from history: page=%+v err=%v", history, err)
	}
	if err := service.DiscardReview(context.Background(), workspace, actor, review.ID, "No longer needed for this investigation."); err != nil {
		t.Fatal(err)
	}
	history, err = service.ReviewHistory(context.Background(), workspace, 10)
	if err != nil || history.Count != 1 || history.Items[0].Status != domain.ReviewDiscarded || history.Items[0].DiscardReason != "No longer needed for this investigation." {
		t.Fatalf("discarded review missing from history: page=%+v err=%v", history, err)
	}
	if repo.review.Status != domain.ReviewDiscarded || repo.review.DiscardedBy == nil || repo.review.DiscardedAt == nil {
		t.Fatalf("discard transition was not persisted: %+v", repo.review)
	}
}

func TestSweepPersistsPartialFailureAndPublishesFailureAuditEvent(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	ids := id.NewSequence(clk.Now())
	ref, err := blob.ParseRef("sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	repo := &cleanupRepo{items: []domain.Candidate{{Ref: ref.String(), Kind: domain.KindSourceCapture, Bytes: 19}}}
	blobs := &cleanupBlobs{info: blob.Info{Ref: ref, Size: 19}, verifyErr: errors.New("storage read failed")}
	service := app.New(repo, blobs, repo, ids, clk)
	workspace, actor := ids.NewID(), ids.NewID()

	result, err := service.Sweep(context.Background(), workspace, actor, 1, nil, id.Nil)
	if err == nil || err.Error() != "storage read failed" {
		t.Fatalf("expected storage failure, got %v", err)
	}
	if result.Run.Status != domain.SweepFailed || result.Run.Error != "storage read failed" || result.Run.FinishedAt == nil {
		t.Fatalf("unexpected failed run: %+v", result.Run)
	}
	if len(repo.failed) != 1 || repo.failed[0].Status != domain.SweepFailed || repo.failed[0].CandidateCount != 1 {
		t.Fatalf("failure was not durably recorded: %+v", repo.failed)
	}
	if len(repo.events) != 1 || repo.events[0].Name != domain.EventSweepFailed {
		t.Fatalf("failure audit events: %+v", repo.events)
	}
	if len(repo.itemsWritten) != 1 || repo.itemsWritten[0].Outcome != domain.OutcomeFailed {
		t.Fatalf("failed sweep item history: %+v", repo.itemsWritten)
	}
}
