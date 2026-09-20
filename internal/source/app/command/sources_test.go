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
	watches      []domain.Watch
	alerts       []domain.Alert
	intakes      []domain.IntakeCandidate
	captures     []domain.Capture
	events       []events.Event
	publishError error
}

func (s *sourceStore) InTx(ctx context.Context, fn func(context.Context) error) error {
	n, w, a, i, c, e := len(s.sources), len(s.watches), len(s.alerts), len(s.intakes), len(s.captures), len(s.events)
	err := fn(context.WithValue(ctx, transactionKey{}, true))
	if err != nil {
		s.sources = s.sources[:n]
		s.watches = s.watches[:w]
		s.alerts = s.alerts[:a]
		s.intakes = s.intakes[:i]
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

func (s *sourceStore) UpsertWatch(_ context.Context, in domain.Watch) error {
	for index := range s.watches {
		if s.watches[index].WorkspaceID == in.WorkspaceID && s.watches[index].SourceID == in.SourceID {
			s.watches[index] = in
			return nil
		}
	}
	s.watches = append(s.watches, in)
	return nil
}

func (s *sourceStore) WatchBySource(_ context.Context, workspace, source id.ID) (domain.Watch, error) {
	for _, one := range s.watches {
		if one.WorkspaceID == workspace && one.SourceID == source {
			return one, nil
		}
	}
	return domain.DefaultWatch(workspace, source), nil
}

func (s *sourceStore) CreateAlert(_ context.Context, in domain.Alert) error {
	for index := range s.alerts {
		if s.alerts[index].WorkspaceID == in.WorkspaceID && s.alerts[index].DedupeKey == in.DedupeKey {
			in.ID = s.alerts[index].ID
			s.alerts[index] = in
			return nil
		}
	}
	s.alerts = append(s.alerts, in)
	return nil
}

func (s *sourceStore) DeactivateQuestionGapAlerts(_ context.Context, workspace id.ID, activeKeys []string) error {
	active := make(map[string]struct{}, len(activeKeys))
	for _, key := range activeKeys {
		active[key] = struct{}{}
	}
	for index := range s.alerts {
		if s.alerts[index].WorkspaceID == workspace && s.alerts[index].Kind == domain.AlertKindQuestionGap {
			_, keep := active[s.alerts[index].DedupeKey]
			s.alerts[index].Active = keep
		}
	}
	return nil
}

func (s *sourceStore) DeactivateDerivedGapAlerts(_ context.Context, workspace id.ID, activeKeys []string) error {
	active := make(map[string]struct{}, len(activeKeys))
	for _, key := range activeKeys {
		active[key] = struct{}{}
	}
	for index := range s.alerts {
		if s.alerts[index].WorkspaceID != workspace {
			continue
		}
		switch s.alerts[index].Kind {
		case domain.AlertKindQuestionGap, domain.AlertKindRecordGap, domain.AlertKindClusterGap:
			_, keep := active[s.alerts[index].DedupeKey]
			s.alerts[index].Active = keep
		}
	}
	return nil
}

func (s *sourceStore) MarkAlertSeen(_ context.Context, workspace, alert, account id.ID, at time.Time) error {
	for index := range s.alerts {
		if s.alerts[index].WorkspaceID == workspace && s.alerts[index].ID == alert {
			return nil
		}
	}
	return domain.ErrNotFound
}

func (s *sourceStore) CreateIntakeCandidate(_ context.Context, in domain.IntakeCandidate) error {
	s.intakes = append(s.intakes, in)
	return nil
}

func (s *sourceStore) IntakeByID(_ context.Context, workspace, intake id.ID) (domain.IntakeCandidate, error) {
	for _, one := range s.intakes {
		if one.WorkspaceID == workspace && one.ID == intake {
			return one, nil
		}
	}
	return domain.IntakeCandidate{}, domain.ErrNotFound
}

func (s *sourceStore) ReviewIntakeCandidate(_ context.Context, in domain.IntakeCandidate) error {
	for index := range s.intakes {
		if s.intakes[index].WorkspaceID == in.WorkspaceID && s.intakes[index].ID == in.ID {
			s.intakes[index] = in
			return nil
		}
	}
	return domain.ErrNotFound
}

func (s *sourceStore) SetRetention(context.Context, id.ID, id.ID, id.ID, *time.Time, time.Time) error {
	return nil
}

func (s *sourceStore) SetPublication(_ context.Context, workspace, source, _ id.ID, publishedAt *time.Time, _ time.Time) error {
	for index := range s.sources {
		if s.sources[index].WorkspaceID == workspace && s.sources[index].ID == source {
			s.sources[index].PublishedAt = publishedAt
			return nil
		}
	}
	return domain.ErrNotFound
}

func (s *sourceStore) SetDuplicatePolicy(_ context.Context, workspace, source, _ id.ID, policy string, _ time.Time) error {
	for index := range s.sources {
		if s.sources[index].WorkspaceID == workspace && s.sources[index].ID == source {
			s.sources[index].DuplicatePolicy = policy
			return nil
		}
	}
	return domain.ErrNotFound
}

func (s *sourceStore) DuplicatePolicy(_ context.Context, workspace, source id.ID) (string, error) {
	for _, one := range s.sources {
		if one.WorkspaceID == workspace && one.ID == source {
			return one.DuplicatePolicy, nil
		}
	}
	return "", domain.ErrNotFound
}

func (s *sourceStore) CaptureHashExists(_ context.Context, workspace, source id.ID, hash string) (bool, error) {
	for _, one := range s.captures {
		if one.WorkspaceID == workspace && one.SourceID == source && one.SHA256 == hash {
			return true, nil
		}
	}
	return false, nil
}

func (s *sourceStore) ByID(_ context.Context, workspace, source id.ID) (domain.Summary, error) {
	for _, one := range s.sources {
		if one.WorkspaceID != workspace || one.ID != source {
			continue
		}
		out := domain.Summary{Source: one}
		for _, capture := range s.captures {
			if capture.WorkspaceID == workspace && capture.SourceID == source && (out.LatestCapture == nil || capture.Version > out.LatestCapture.Version) {
				copy := capture
				out.LatestCapture = &copy
			}
		}
		return out, nil
	}
	return domain.Summary{}, domain.ErrNotFound
}
func (s *sourceStore) LockForPurge(context.Context, id.ID, id.ID) (domain.Summary, domain.PurgeDependencies, error) {
	return domain.Summary{}, domain.PurgeDependencies{}, nil
}
func (s *sourceStore) SetPrivacy(context.Context, id.ID, id.ID, id.ID, string, bool, string, time.Time) error {
	return nil
}
func (s *sourceStore) MarkPurged(context.Context, id.ID, id.ID, id.ID, string, time.Time) error {
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

func TestSourceWatchConfigurationAndRunMetadataAreAudited(t *testing.T) {
	service, repo, _, ids := setup(t)
	ctx := context.Background()
	workspace, author := ids.NewID(), ids.NewID()
	source, err := service.Create(ctx, workspace, author, command.Draft{Title: "Monitored page", Origin: "reference", URL: "https://example.test/monitored"})
	if err != nil {
		t.Fatal(err)
	}
	watch, err := service.ConfigureWatch(ctx, workspace, source.ID, author, true, domain.MinWatchIntervalSeconds)
	if err != nil || !watch.Enabled || watch.NextRunAt == nil || len(repo.watches) != 1 {
		t.Fatalf("watch configuration: %+v, error=%v", watch, err)
	}
	updated, err := service.RecordWatchRun(ctx, workspace, source.ID, author, domain.WatchStatusUnchanged, id.ID{}, "")
	if err != nil || updated.LastStatus != domain.WatchStatusUnchanged || updated.LastRunAt == nil || len(repo.events) != 3 {
		t.Fatalf("watch run metadata: %+v, error=%v, events=%d", updated, err, len(repo.events))
	}
	changed, err := service.RecordWatchRun(ctx, workspace, source.ID, author, domain.WatchStatusChanged, ids.NewID(), "")
	if err != nil || changed.LastStatus != domain.WatchStatusChanged || len(repo.alerts) != 1 || repo.alerts[0].Kind != domain.AlertKindCaptureChanged {
		t.Fatalf("changed watch did not create an alert: %+v alerts=%+v error=%v", changed, repo.alerts, err)
	}
	failed, err := service.RecordWatchRun(ctx, workspace, source.ID, author, domain.WatchStatusFailed, id.ID{}, "timeout")
	if err != nil || failed.LastStatus != domain.WatchStatusFailed || len(repo.alerts) != 2 || repo.alerts[1].Kind != domain.AlertKindWatchFailed {
		t.Fatalf("failed watch did not create an alert: %+v alerts=%+v error=%v", failed, repo.alerts, err)
	}
	if _, err := service.ConfigureWatch(ctx, workspace, source.ID, author, true, 60); err == nil {
		t.Fatal("invalid watch interval accepted")
	}
}

func TestQuestionGapAlertsAreIdempotentAndRetireStaleStatuses(t *testing.T) {
	service, repo, _, ids := setup(t)
	ctx := context.Background()
	workspace, author, question := ids.NewID(), ids.NewID(), ids.NewID()
	gap := command.QuestionGapAlert{
		QuestionID: question,
		DedupeKey:  "question-gap:" + question.String() + ":not_compared",
		Title:      "Open question has not been compared",
		Detail:     "Which notice timing is reliable?",
	}
	count, err := service.SyncQuestionGapAlerts(ctx, workspace, author, []command.QuestionGapAlert{gap})
	if err != nil || count != 1 || len(repo.alerts) != 1 || !repo.alerts[0].Active {
		t.Fatalf("initial gap sync: count=%d alerts=%+v err=%v", count, repo.alerts, err)
	}
	count, err = service.SyncQuestionGapAlerts(ctx, workspace, author, []command.QuestionGapAlert{gap})
	if err != nil || count != 1 || len(repo.alerts) != 1 {
		t.Fatalf("idempotent gap sync: count=%d alerts=%+v err=%v", count, repo.alerts, err)
	}
	count, err = service.SyncQuestionGapAlerts(ctx, workspace, author, nil)
	if err != nil || count != 0 || repo.alerts[0].Active {
		t.Fatalf("stale gap was not retired: count=%d alerts=%+v err=%v", count, repo.alerts, err)
	}
}

func TestDerivedRecordAndClusterGapAlertsAreTargetedAndIdempotent(t *testing.T) {
	service, repo, _, ids := setup(t)
	ctx := context.Background()
	workspace, author, record, cluster := ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID()
	gaps := []command.DerivedGapAlert{
		{Kind: domain.AlertKindRecordGap, TargetID: record, DedupeKey: "record-gap:" + record.String() + ":unresolved", Title: "Record has unresolved evidence", Detail: "record detail"},
		{Kind: domain.AlertKindClusterGap, TargetID: cluster, DedupeKey: "cluster-gap:" + cluster.String() + ":review_incomplete", Title: "Evidence cluster review is incomplete", Detail: "cluster detail"},
	}
	count, err := service.SyncDerivedGapAlerts(ctx, workspace, author, gaps)
	if err != nil || count != 2 || len(repo.alerts) != 2 {
		t.Fatalf("initial derived gap sync: count=%d alerts=%+v err=%v", count, repo.alerts, err)
	}
	if repo.alerts[0].RecordID == nil || repo.alerts[0].ClusterID != nil || repo.alerts[1].ClusterID == nil || repo.alerts[1].RecordID != nil {
		t.Fatalf("derived target shape: %+v", repo.alerts)
	}
	count, err = service.SyncDerivedGapAlerts(ctx, workspace, author, gaps)
	if err != nil || count != 2 || len(repo.alerts) != 2 {
		t.Fatalf("idempotent derived gap sync: count=%d alerts=%+v err=%v", count, repo.alerts, err)
	}
	count, err = service.SyncDerivedGapAlerts(ctx, workspace, author, nil)
	if err != nil || count != 0 || repo.alerts[0].Active || repo.alerts[1].Active {
		t.Fatalf("stale derived gaps were not retired: count=%d alerts=%+v err=%v", count, repo.alerts, err)
	}
}

func TestPublicationEditPersistsAndEmitsDecision(t *testing.T) {
	service, repo, _, ids := setup(t)
	ctx := context.Background()
	workspace, author := ids.NewID(), ids.NewID()
	source, err := service.Create(ctx, workspace, author, command.Draft{Title: "Public page", Origin: "reference", URL: "https://example.test/report"})
	if err != nil {
		t.Fatal(err)
	}
	published := time.Date(2026, 9, 16, 14, 0, 0, 0, time.FixedZone("source", 2*60*60))
	if err := service.SetPublication(ctx, workspace, source.ID, author, &published); err != nil {
		t.Fatal(err)
	}
	if repo.sources[0].PublishedAt == nil || !repo.sources[0].PublishedAt.Equal(published.UTC()) || len(repo.events) != 2 || repo.events[1].Name != domain.EventPublicationChanged || !repo.events[1].Decision {
		t.Fatalf("publication edit was not persisted and audited: source=%+v events=%+v", repo.sources[0], repo.events)
	}
}

func TestBlockedDuplicateCaptureLeavesHistoryUntouched(t *testing.T) {
	service, repo, _, ids := setup(t)
	ctx := context.Background()
	workspace, author := ids.NewID(), ids.NewID()
	content := "same retained bytes"
	source, err := service.Create(ctx, workspace, author, command.Draft{Title: "Public page", Origin: "paste", Content: &content})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetDuplicatePolicy(ctx, workspace, source.ID, author, domain.DuplicatePolicyBlock); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddCapture(ctx, workspace, source.ID, author, "text/plain", content); !errors.Is(err, domain.ErrDuplicateCapture) {
		t.Fatalf("expected duplicate conflict, got %v", err)
	}
	if len(repo.captures) != 1 || len(repo.events) != 2 || repo.sources[0].DuplicatePolicy != domain.DuplicatePolicyBlock {
		t.Fatalf("duplicate append changed history: captures=%d events=%d source=%+v", len(repo.captures), len(repo.events), repo.sources[0])
	}
}

func TestIntakeApprovalCreatesReferenceSourceAndPreservesReview(t *testing.T) {
	service, repo, _, ids := setup(t)
	ctx := context.Background()
	workspace, author, reviewer := ids.NewID(), ids.NewID(), ids.NewID()
	candidate, err := service.CreateIntakeCandidate(ctx, workspace, author, "Harbor bulletin", "https://example.test/harbor", "Found during monitored source review.")
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ReviewIntakeCandidate(ctx, workspace, candidate.ID, reviewer, domain.IntakeApproved, "The URL is relevant to this investigation.")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source == nil || result.Source.Origin != "reference" || result.Source.URL != candidate.URL || result.Candidate.Status != domain.IntakeApproved || result.Candidate.SourceID != result.Source.ID {
		t.Fatalf("approval did not create linked source: %+v", result)
	}
	if len(repo.sources) != 1 || len(repo.intakes) != 1 || len(repo.events) != 3 || repo.intakes[0].ReviewNote == "" {
		t.Fatalf("approval history incomplete: sources=%d intakes=%d events=%+v", len(repo.sources), len(repo.intakes), repo.events)
	}
	if _, err := service.ReviewIntakeCandidate(ctx, workspace, candidate.ID, reviewer, domain.IntakeRejected, "Too late to use."); !errors.Is(err, domain.ErrIntakeAlreadyReviewed) {
		t.Fatalf("reviewed intake was reopened: %v", err)
	}
}

func TestRejectedIntakeDoesNotCreateSource(t *testing.T) {
	service, repo, _, ids := setup(t)
	ctx := context.Background()
	workspace, author, reviewer := ids.NewID(), ids.NewID(), ids.NewID()
	candidate, err := service.CreateIntakeCandidate(ctx, workspace, author, "Irrelevant bulletin", "https://example.test/irrelevant", "Candidate needs review.")
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ReviewIntakeCandidate(ctx, workspace, candidate.ID, reviewer, domain.IntakeRejected, "The material is outside the investigation scope.")
	if err != nil || result.Source != nil || result.Candidate.Status != domain.IntakeRejected || len(repo.sources) != 0 {
		t.Fatalf("rejection retained source: result=%+v err=%v sources=%d", result, err, len(repo.sources))
	}
}

func TestImportedIntakeRetainsBytesOnlyAfterApproval(t *testing.T) {
	service, repo, _, ids := setup(t)
	ctx := context.Background()
	workspace, author, reviewer := ids.NewID(), ids.NewID(), ids.NewID()
	candidate, err := service.CreateImportIntakeCandidate(ctx, workspace, author, "Notice PDF", "notice.pdf", "application/pdf", []byte("%PDF-1.7\nnotice"), "Imported for review.")
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.captures) != 0 || len(repo.intakes) != 1 || len(repo.intakes[0].ContentBytes) == 0 {
		t.Fatalf("import was retained before review: captures=%d intakes=%+v", len(repo.captures), repo.intakes)
	}
	result, err := service.ReviewIntakeCandidate(ctx, workspace, candidate.ID, reviewer, domain.IntakeApproved, "The document is relevant and can enter the source history.")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source == nil || result.Source.Origin != domain.IntakeImport || result.Source.Filename != candidate.Filename || result.Source.LatestCapture == nil || result.Source.LatestCapture.MediaType != "application/pdf" || len(repo.captures) != 1 {
		t.Fatalf("approved import did not create its first capture: result=%+v captures=%+v", result, repo.captures)
	}
	if len(repo.intakes[0].ContentBytes) != 0 {
		t.Fatal("staged import bytes remained after approval")
	}
}

func TestImportedTextIntakeBecomesTextCaptureOnlyAfterApproval(t *testing.T) {
	service, repo, _, ids := setup(t)
	ctx := context.Background()
	workspace, author, reviewer := ids.NewID(), ids.NewID(), ids.NewID()
	candidate, err := service.CreateImportIntakeCandidate(ctx, workspace, author, "Notice text", "notice.txt", "text/plain", []byte("retained text"), "Imported for review.")
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ReviewIntakeCandidate(ctx, workspace, candidate.ID, reviewer, domain.IntakeApproved, "The text is relevant and can enter source history.")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source == nil || result.Source.LatestCapture == nil || result.Source.LatestCapture.MediaType != "text/plain" || len(repo.captures) != 1 || len(repo.intakes[0].ContentBytes) != 0 {
		t.Fatalf("approved text import did not create a text capture: result=%+v captures=%+v intakes=%+v", result, repo.captures, repo.intakes)
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
