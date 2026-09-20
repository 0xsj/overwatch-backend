package command

import (
	"bytes"
	"context"
	stderrors "errors"
	"io"
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/internal/source/domain"
	"github.com/0xsj/overwatch-backend/pkg/blob"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type Draft = domain.Draft

type Repository interface {
	CreateSource(context.Context, domain.Source) error
	CreateCapture(context.Context, domain.Capture) error
	CreateIntakeCandidate(context.Context, domain.IntakeCandidate) error
	IntakeByID(context.Context, id.ID, id.ID) (domain.IntakeCandidate, error)
	ReviewIntakeCandidate(context.Context, domain.IntakeCandidate) error
	SetRetention(context.Context, id.ID, id.ID, id.ID, *time.Time, time.Time) error
	// NextVersion must lock the source row until the enclosing transaction ends.
	NextVersion(ctx context.Context, workspace, source id.ID) (int, error)
}

type WatchRepository interface {
	WatchBySource(context.Context, id.ID, id.ID) (domain.Watch, error)
	UpsertWatch(context.Context, domain.Watch) error
}

type DueWatchRepository interface {
	ClaimDueWatch(context.Context, id.ID, string, time.Time, time.Time) (domain.Watch, error)
}

type AlertRepository interface {
	CreateAlert(context.Context, domain.Alert) error
	MarkAlertSeen(context.Context, id.ID, id.ID, id.ID, time.Time) error
	DeactivateQuestionGapAlerts(context.Context, id.ID, []string) error
}

type DerivedAlertRepository interface {
	DeactivateDerivedGapAlerts(context.Context, id.ID, []string) error
}

type QuestionGapAlert struct {
	QuestionID id.ID
	DedupeKey  string
	Title      string
	Detail     string
}

type DerivedGapAlert struct {
	Kind      string
	TargetID  id.ID
	DedupeKey string
	Title     string
	Detail    string
}

// LifecycleRepository is optional so the original capture command port stays
// small for adapters and tests. The production PostgreSQL store implements it
// to make privacy changes and purge checks part of the same source boundary.
type LifecycleRepository interface {
	ByID(context.Context, id.ID, id.ID) (domain.Summary, error)
	LockForPurge(context.Context, id.ID, id.ID) (domain.Summary, domain.PurgeDependencies, error)
	SetDuplicatePolicy(context.Context, id.ID, id.ID, id.ID, string, time.Time) error
	SetPublication(context.Context, id.ID, id.ID, id.ID, *time.Time, time.Time) error
	SetPrivacy(context.Context, id.ID, id.ID, id.ID, string, bool, string, time.Time) error
	MarkPurged(context.Context, id.ID, id.ID, id.ID, string, time.Time) error
}

type DuplicateRepository interface {
	DuplicatePolicy(context.Context, id.ID, id.ID) (string, error)
	CaptureHashExists(context.Context, id.ID, id.ID, string) (bool, error)
}
type Blobs interface {
	Put(context.Context, io.Reader) (blob.Info, error)
}
type TextIndexer interface {
	IndexText(context.Context, id.ID, id.ID, id.ID, id.ID, string, int64, string, time.Time) error
}
type Transactor interface {
	InTx(context.Context, func(context.Context) error) error
}
type Minter interface{ NewID() id.ID }
type Clock interface{ Now() time.Time }

type Sources struct {
	repo      Repository
	blobs     Blobs
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

type PurgeResult struct {
	Review domain.PurgeReview `json:"review"`
	Purged bool               `json:"purged"`
}

type IntakeReviewResult struct {
	Candidate domain.IntakeCandidate `json:"candidate"`
	Source    *domain.Summary        `json:"source,omitempty"`
}

type WatchRunResult struct {
	Watch   domain.Watch    `json:"watch"`
	Changed bool            `json:"changed"`
	Capture *domain.Capture `json:"capture,omitempty"`
}

func NewSources(repo Repository, blobs Blobs, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Sources {
	if repo == nil || blobs == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("source: NewSources with a nil dependency")
	}
	return &Sources{repo, blobs, tx, publisher, ids, clock}
}

func (s *Sources) Create(ctx context.Context, workspace, author id.ID, in Draft) (domain.Summary, error) {
	source, err := domain.New(s.ids.NewID(), workspace, author, in, s.clock.Now())
	if err != nil {
		return domain.Summary{}, err
	}
	out := domain.Summary{Source: source}
	if in.Content != nil {
		capture, err := s.capture(ctx, workspace, source.ID, author, 1, in.MediaType, *in.Content)
		if err != nil {
			return domain.Summary{}, err
		}
		out.LatestCapture = &capture
	} else if len(in.ContentBytes) > 0 {
		capture, err := s.captureBytes(ctx, workspace, source.ID, author, 1, in.MediaType, in.ContentBytes)
		if err != nil {
			return domain.Summary{}, err
		}
		out.LatestCapture = &capture
	}
	// Blob storage is immutable and outside the database transaction. A failed
	// commit can leave unused bytes, never a visible source with missing metadata.
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.CreateSource(ctx, source); err != nil {
			return err
		}
		if out.LatestCapture != nil {
			if err := s.repo.CreateCapture(ctx, *out.LatestCapture); err != nil {
				return err
			}
			if err := s.indexText(ctx, *out.LatestCapture, captureContent(in)); err != nil {
				return err
			}
		}
		return s.emit(ctx, domain.EventCreated, workspace, author, source.ID, out.LatestCapture)
	})
	if err != nil {
		return domain.Summary{}, err
	}
	return out, nil
}

func (s *Sources) CreateIntakeCandidate(ctx context.Context, workspace, author id.ID, title, address, note string) (domain.IntakeCandidate, error) {
	candidate, err := domain.NewIntakeCandidate(s.ids.NewID(), workspace, author, title, address, note, s.clock.Now())
	if err != nil {
		return domain.IntakeCandidate{}, err
	}
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.CreateIntakeCandidate(ctx, candidate); err != nil {
			return err
		}
		return s.emitIntake(ctx, domain.EventIntakeCreated, candidate, author)
	}); err != nil {
		return domain.IntakeCandidate{}, err
	}
	return candidate, nil
}

func (s *Sources) CreateImportIntakeCandidate(ctx context.Context, workspace, author id.ID, title, filename, mediaType string, content []byte, note string) (domain.IntakeCandidate, error) {
	candidate, err := domain.NewImportIntakeCandidate(s.ids.NewID(), workspace, author, title, filename, mediaType, content, note, s.clock.Now())
	if err != nil {
		return domain.IntakeCandidate{}, err
	}
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.CreateIntakeCandidate(ctx, candidate); err != nil {
			return err
		}
		return s.emitIntake(ctx, domain.EventIntakeCreated, candidate, author)
	}); err != nil {
		return domain.IntakeCandidate{}, err
	}
	return candidate, nil
}

func (s *Sources) ReadWatch(ctx context.Context, workspace, source id.ID) (domain.Watch, error) {
	if workspace.IsZero() || source.IsZero() {
		return domain.Watch{}, domain.ErrInvalid
	}
	repo, ok := s.repo.(WatchRepository)
	if !ok {
		return domain.Watch{}, domain.ErrInvalid
	}
	return repo.WatchBySource(ctx, workspace, source)
}

func (s *Sources) ConfigureWatch(ctx context.Context, workspace, source, actor id.ID, enabled bool, intervalSeconds int) (domain.Watch, error) {
	if workspace.IsZero() || source.IsZero() || actor.IsZero() {
		return domain.Watch{}, domain.ErrInvalid
	}
	lifecycle, ok := s.repo.(LifecycleRepository)
	if !ok {
		return domain.Watch{}, domain.ErrInvalid
	}
	repo, ok := s.repo.(WatchRepository)
	if !ok {
		return domain.Watch{}, domain.ErrInvalid
	}
	summary, err := lifecycle.ByID(ctx, workspace, source)
	if err != nil {
		return domain.Watch{}, err
	}
	if summary.Origin != "reference" || summary.URL == "" || summary.PurgedAt != nil {
		return domain.Watch{}, domain.ErrWatchSourceInvalid
	}
	current, err := repo.WatchBySource(ctx, workspace, source)
	if err != nil && !stderrors.Is(err, domain.ErrNotFound) {
		return domain.Watch{}, err
	}
	at := s.clock.Now()
	if stderrors.Is(err, domain.ErrNotFound) {
		current, err = domain.NewWatch(workspace, source, actor, enabled, intervalSeconds, at)
	} else {
		current, err = current.Configure(actor, enabled, intervalSeconds, at)
	}
	if err != nil {
		return domain.Watch{}, err
	}
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := repo.UpsertWatch(ctx, current); err != nil {
			return err
		}
		return s.emitWatch(ctx, domain.EventWatchConfigured, current, actor, "")
	}); err != nil {
		return domain.Watch{}, err
	}
	return current, nil
}

func (s *Sources) RecordWatchRun(ctx context.Context, workspace, source, actor id.ID, status string, capture id.ID, runError string) (domain.Watch, error) {
	return s.recordWatchRun(ctx, workspace, source, actor, status, capture, runError, "")
}

func (s *Sources) RecordClaimedWatchRun(ctx context.Context, workspace, source, actor id.ID, status string, capture id.ID, runError, leaseOwner string) (domain.Watch, error) {
	return s.recordWatchRun(ctx, workspace, source, actor, status, capture, runError, strings.TrimSpace(leaseOwner))
}

func (s *Sources) recordWatchRun(ctx context.Context, workspace, source, actor id.ID, status string, capture id.ID, runError, leaseOwner string) (domain.Watch, error) {
	if workspace.IsZero() || source.IsZero() || actor.IsZero() {
		return domain.Watch{}, domain.ErrInvalid
	}
	repo, ok := s.repo.(WatchRepository)
	if !ok {
		return domain.Watch{}, domain.ErrInvalid
	}
	current, err := repo.WatchBySource(ctx, workspace, source)
	if err != nil {
		return domain.Watch{}, err
	}
	var next domain.Watch
	if leaseOwner == "" {
		next, err = current.RecordRun(actor, status, capture, runError, s.clock.Now())
	} else {
		next, err = current.RecordClaimedRun(actor, status, capture, runError, leaseOwner, s.clock.Now())
	}
	if err != nil {
		return domain.Watch{}, err
	}
	var alert domain.Alert
	alertRepo, alertsEnabled := s.repo.(AlertRepository)
	if alertsEnabled && (status == domain.WatchStatusChanged || status == domain.WatchStatusFailed) {
		kind, title, detail := domain.AlertKindCaptureChanged, "New monitored capture retained", "A monitored URL returned changed bytes and a new immutable capture was retained."
		if status == domain.WatchStatusFailed {
			kind, title, detail = domain.AlertKindWatchFailed, "Source monitoring failed", strings.TrimSpace(runError)
			if detail == "" {
				detail = "The monitored URL could not be checked."
			}
		}
		alert, err = domain.NewAlert(s.ids.NewID(), workspace, source, capture, actor, kind, title, detail, s.clock.Now())
		if err != nil {
			return domain.Watch{}, err
		}
	}
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := repo.UpsertWatch(ctx, next); err != nil {
			return err
		}
		if alertsEnabled {
			if status == domain.WatchStatusChanged || status == domain.WatchStatusFailed {
				if err := alertRepo.CreateAlert(ctx, alert); err != nil {
					return err
				}
			}
		}
		return s.emitWatch(ctx, domain.EventWatchRun, next, actor, runError)
	}); err != nil {
		return domain.Watch{}, err
	}
	return next, nil
}

func (s *Sources) ClaimDueWatch(ctx context.Context, workspace id.ID, owner string, lease time.Duration) (domain.Watch, error) {
	owner = strings.TrimSpace(owner)
	if workspace.IsZero() || owner == "" || lease <= 0 {
		return domain.Watch{}, domain.ErrInvalid
	}
	repo, ok := s.repo.(DueWatchRepository)
	if !ok {
		return domain.Watch{}, domain.ErrInvalid
	}
	now := s.clock.Now()
	return repo.ClaimDueWatch(ctx, workspace, owner, now, now.Add(lease))
}

func (s *Sources) MarkAlertSeen(ctx context.Context, workspace, alert, account id.ID) (time.Time, error) {
	if workspace.IsZero() || alert.IsZero() || account.IsZero() {
		return time.Time{}, domain.ErrInvalid
	}
	repo, ok := s.repo.(AlertRepository)
	if !ok {
		return time.Time{}, domain.ErrInvalid
	}
	at := s.clock.Now()
	if err := repo.MarkAlertSeen(ctx, workspace, alert, account, at); err != nil {
		return time.Time{}, err
	}
	return at, nil
}

// SyncQuestionGapAlerts materializes the current open-question gap projection
// as alert rows. The source domain owns persistence and lifecycle; the root
// layer owns the review/question calculation and supplies only the derived
// values. A status is part of the dedupe key so a meaningful transition is a
// fresh alert, while repeated refreshes of the same status remain idempotent.
func (s *Sources) SyncQuestionGapAlerts(ctx context.Context, workspace, actor id.ID, gaps []QuestionGapAlert) (int, error) {
	derived := make([]DerivedGapAlert, 0, len(gaps))
	for _, gap := range gaps {
		derived = append(derived, DerivedGapAlert{Kind: domain.AlertKindQuestionGap, TargetID: gap.QuestionID, DedupeKey: gap.DedupeKey, Title: gap.Title, Detail: gap.Detail})
	}
	return s.syncDerivedGapAlerts(ctx, workspace, actor, derived, false)
}

// SyncDerivedGapAlerts materializes current record, cluster, and question gap
// projections into the same durable alert inbox. Target and status are carried
// by the caller's dedupe key, so repeated refreshes are idempotent and stale
// projections can be retired atomically.
func (s *Sources) SyncDerivedGapAlerts(ctx context.Context, workspace, actor id.ID, gaps []DerivedGapAlert) (int, error) {
	return s.syncDerivedGapAlerts(ctx, workspace, actor, gaps, true)
}

func (s *Sources) syncDerivedGapAlerts(ctx context.Context, workspace, actor id.ID, gaps []DerivedGapAlert, derived bool) (int, error) {
	if workspace.IsZero() || actor.IsZero() {
		return 0, domain.ErrInvalid
	}
	repo, ok := s.repo.(AlertRepository)
	if !ok {
		return 0, domain.ErrInvalid
	}
	if derived {
		if _, ok := s.repo.(DerivedAlertRepository); !ok {
			return 0, domain.ErrInvalid
		}
	}
	at := s.clock.Now()
	alerts := make([]domain.Alert, 0, len(gaps))
	for _, gap := range gaps {
		alert, err := domain.NewDerivedGapAlert(s.ids.NewID(), workspace, gap.Kind, gap.TargetID, actor, gap.DedupeKey, gap.Title, gap.Detail, at)
		if err != nil {
			return 0, err
		}
		alerts = append(alerts, alert)
	}
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		activeKeys := make([]string, 0, len(alerts))
		for _, alert := range alerts {
			activeKeys = append(activeKeys, alert.DedupeKey)
		}
		if derived {
			if err := s.repo.(DerivedAlertRepository).DeactivateDerivedGapAlerts(ctx, workspace, activeKeys); err != nil {
				return err
			}
		} else if err := repo.DeactivateQuestionGapAlerts(ctx, workspace, activeKeys); err != nil {
			return err
		}
		for _, alert := range alerts {
			if err := repo.CreateAlert(ctx, alert); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return 0, err
	}
	return len(alerts), nil
}

func (s *Sources) ReviewIntakeCandidate(ctx context.Context, workspace, intake, reviewer id.ID, decision, note string) (IntakeReviewResult, error) {
	if workspace.IsZero() || intake.IsZero() || reviewer.IsZero() {
		return IntakeReviewResult{}, domain.ErrInvalid
	}
	held, err := s.repo.IntakeByID(ctx, workspace, intake)
	if err != nil {
		return IntakeReviewResult{}, err
	}
	at := s.clock.Now()
	var source domain.Source
	var sourceOut *domain.Summary
	sourceID := id.ID{}
	if decision == domain.IntakeApproved {
		draft := domain.Draft{Title: held.Title, Origin: held.Origin, URL: held.URL, Filename: held.Filename, MediaType: held.MediaType, ContentBytes: held.ContentBytes}
		if held.MediaType == "text/plain" || held.MediaType == "text/html" || held.MediaType == "application/json" {
			text := string(held.ContentBytes)
			draft.Content = &text
			draft.ContentBytes = nil
		}
		source, err = domain.New(s.ids.NewID(), workspace, reviewer, draft, at)
		if err != nil {
			return IntakeReviewResult{}, err
		}
		sourceID = source.ID
	}
	next, err := held.Review(reviewer, decision, note, sourceID, at)
	if err != nil {
		return IntakeReviewResult{}, err
	}
	if decision == domain.IntakeApproved {
		summary := domain.Summary{Source: source}
		if held.Origin == domain.IntakeImport {
			capture, captureErr := s.captureBytes(ctx, workspace, source.ID, reviewer, 1, held.MediaType, held.ContentBytes)
			if captureErr != nil {
				return IntakeReviewResult{}, captureErr
			}
			summary.LatestCapture = &capture
		}
		sourceOut = &summary
	}
	next.ContentBytes = nil
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if sourceOut != nil {
			if err := s.repo.CreateSource(ctx, source); err != nil {
				return err
			}
			if sourceOut.LatestCapture != nil {
				if err := s.repo.CreateCapture(ctx, *sourceOut.LatestCapture); err != nil {
					return err
				}
				if err := s.indexText(ctx, *sourceOut.LatestCapture, held.ContentBytes); err != nil {
					return err
				}
			}
			if err := s.emit(ctx, domain.EventCreated, workspace, reviewer, source.ID, sourceOut.LatestCapture); err != nil {
				return err
			}
		}
		if err := s.repo.ReviewIntakeCandidate(ctx, next); err != nil {
			return err
		}
		return s.emitIntake(ctx, map[string]string{domain.IntakeApproved: domain.EventIntakeApproved, domain.IntakeRejected: domain.EventIntakeRejected}[next.Status], next, reviewer)
	}); err != nil {
		return IntakeReviewResult{}, err
	}
	return IntakeReviewResult{Candidate: next, Source: sourceOut}, nil
}

func (s *Sources) AddCapture(ctx context.Context, workspace, source, author id.ID, mediaType, content string) (domain.Capture, error) {
	// Retain and validate bytes before acquiring the row lock; no external
	// process or network fetch is held inside the transaction.
	capture, err := s.capture(ctx, workspace, source, author, 1, mediaType, content)
	if err != nil {
		return domain.Capture{}, err
	}
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		version, err := s.repo.NextVersion(ctx, workspace, source)
		if err != nil {
			return err
		}
		if err := s.rejectDuplicate(ctx, workspace, source, capture.SHA256); err != nil {
			return err
		}
		capture.Version = version
		if err := s.repo.CreateCapture(ctx, capture); err != nil {
			return err
		}
		if err := s.indexText(ctx, capture, []byte(content)); err != nil {
			return err
		}
		return s.emit(ctx, domain.EventCaptured, workspace, author, source, &capture)
	})
	if err != nil {
		return domain.Capture{}, err
	}
	return capture, nil
}

func (s *Sources) AddBinaryCapture(ctx context.Context, workspace, source, author id.ID, mediaType string, content []byte) (domain.Capture, error) {
	if workspace.IsZero() || source.IsZero() || author.IsZero() {
		return domain.Capture{}, domain.ErrInvalid
	}
	if err := domain.ValidateBinary(mediaType, content); err != nil {
		return domain.Capture{}, err
	}
	capture, err := s.captureBytes(ctx, workspace, source, author, 1, mediaType, content)
	if err != nil {
		return domain.Capture{}, err
	}
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		version, err := s.repo.NextVersion(ctx, workspace, source)
		if err != nil {
			return err
		}
		if err := s.rejectDuplicate(ctx, workspace, source, capture.SHA256); err != nil {
			return err
		}
		capture.Version = version
		if err := s.repo.CreateCapture(ctx, capture); err != nil {
			return err
		}
		if err := s.indexText(ctx, capture, content); err != nil {
			return err
		}
		return s.emit(ctx, domain.EventCaptured, workspace, author, source, &capture)
	})
	if err != nil {
		return domain.Capture{}, err
	}
	return capture, nil
}

func (s *Sources) rejectDuplicate(ctx context.Context, workspace, source id.ID, hash string) error {
	repo, ok := s.repo.(DuplicateRepository)
	if !ok {
		return nil
	}
	policy, err := repo.DuplicatePolicy(ctx, workspace, source)
	if err != nil {
		return err
	}
	if policy != domain.DuplicatePolicyBlock {
		return nil
	}
	exists, err := repo.CaptureHashExists(ctx, workspace, source, hash)
	if err != nil {
		return err
	}
	if exists {
		return domain.ErrDuplicateCapture
	}
	return nil
}

func (s *Sources) SetRetention(ctx context.Context, workspace, source, editor id.ID, until *time.Time) error {
	if workspace.IsZero() || source.IsZero() || editor.IsZero() {
		return domain.ErrInvalid
	}
	at := s.clock.Now()
	if _, err := (domain.Source{}).SetRetention(editor, until, at); err != nil {
		return err
	}
	return s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.SetRetention(ctx, workspace, source, editor, until, at); err != nil {
			return err
		}
		return s.emitRetention(ctx, workspace, source, editor, until)
	})
}

func (s *Sources) SetPublication(ctx context.Context, workspace, source, editor id.ID, publishedAt *time.Time) error {
	if workspace.IsZero() || source.IsZero() || editor.IsZero() {
		return domain.ErrInvalid
	}
	repo, ok := s.repo.(LifecycleRepository)
	if !ok {
		return domain.ErrInvalid
	}
	at := s.clock.Now()
	publication, err := (domain.Source{}).SetPublication(editor, publishedAt, at)
	if err != nil {
		return err
	}
	return s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := repo.SetPublication(ctx, workspace, source, editor, publication.PublishedAt, at); err != nil {
			return err
		}
		return s.emitPublication(ctx, workspace, source, editor, publication.PublishedAt)
	})
}

func (s *Sources) SetDuplicatePolicy(ctx context.Context, workspace, source, editor id.ID, policy string) error {
	if workspace.IsZero() || source.IsZero() || editor.IsZero() {
		return domain.ErrInvalid
	}
	repo, ok := s.repo.(LifecycleRepository)
	if !ok {
		return domain.ErrInvalid
	}
	at := s.clock.Now()
	updated, err := (domain.Source{}).SetDuplicatePolicy(editor, policy, at)
	if err != nil {
		return err
	}
	return s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := repo.SetDuplicatePolicy(ctx, workspace, source, editor, updated.DuplicatePolicy, at); err != nil {
			return err
		}
		return s.emitDuplicatePolicy(ctx, workspace, source, editor, updated.DuplicatePolicy)
	})
}

func (s *Sources) SetPrivacy(ctx context.Context, workspace, source, editor id.ID, sensitivity string, legalHold bool, reason string) error {
	if workspace.IsZero() || source.IsZero() || editor.IsZero() {
		return domain.ErrInvalid
	}
	repo, ok := s.repo.(LifecycleRepository)
	if !ok {
		return domain.ErrInvalid
	}
	at := s.clock.Now()
	privacy, err := (domain.Source{}).SetPrivacy(editor, sensitivity, legalHold, reason, at)
	if err != nil {
		return err
	}
	reason = privacy.LegalHoldReason
	return s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := repo.SetPrivacy(ctx, workspace, source, editor, sensitivity, legalHold, reason, at); err != nil {
			return err
		}
		return s.emitPrivacy(ctx, workspace, source, editor, sensitivity, legalHold, reason)
	})
}

func (s *Sources) RetentionReview(ctx context.Context, workspace, source id.ID) (domain.PurgeReview, error) {
	if workspace.IsZero() || source.IsZero() {
		return domain.PurgeReview{}, domain.ErrInvalid
	}
	repo, ok := s.repo.(LifecycleRepository)
	if !ok {
		return domain.PurgeReview{}, domain.ErrInvalid
	}
	var out domain.PurgeReview
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		held, dependencies, err := repo.LockForPurge(ctx, workspace, source)
		if err != nil {
			return err
		}
		out = held.Source.ReviewPurge(s.clock.Now(), dependencies)
		return nil
	})
	return out, err
}

func (s *Sources) Purge(ctx context.Context, workspace, source, actor id.ID, reason string) (PurgeResult, error) {
	if workspace.IsZero() || source.IsZero() || actor.IsZero() {
		return PurgeResult{}, domain.ErrInvalid
	}
	repo, ok := s.repo.(LifecycleRepository)
	if !ok {
		return PurgeResult{}, domain.ErrInvalid
	}
	at := s.clock.Now()
	if _, err := (domain.Source{}).Purge(actor, reason, at); err != nil {
		return PurgeResult{}, err
	}
	var out PurgeResult
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		held, dependencies, err := repo.LockForPurge(ctx, workspace, source)
		if err != nil {
			return err
		}
		out.Review = held.Source.ReviewPurge(at, dependencies)
		if !out.Review.Eligible {
			return nil
		}
		purged, err := held.Source.Purge(actor, reason, at)
		if err != nil {
			return err
		}
		reason = purged.PurgeReason
		if err := repo.MarkPurged(ctx, workspace, source, actor, reason, at); err != nil {
			return err
		}
		out.Purged = true
		out.Review = purged.ReviewPurge(at, dependencies)
		return s.emitPurge(ctx, workspace, source, actor, reason, dependencies)
	})
	return out, err
}

func (s *Sources) capture(ctx context.Context, workspace, source, author id.ID, version int, mediaType, content string) (domain.Capture, error) {
	return s.captureBytes(ctx, workspace, source, author, version, mediaType, []byte(content))
}

func captureContent(in domain.Draft) []byte {
	if in.Content != nil {
		return []byte(*in.Content)
	}
	return in.ContentBytes
}

func (s *Sources) indexText(ctx context.Context, capture domain.Capture, content []byte) error {
	if capture.MediaType != "text/plain" && capture.MediaType != "text/html" && capture.MediaType != "application/json" {
		return nil
	}
	indexer, ok := s.repo.(TextIndexer)
	if !ok {
		return nil
	}
	return indexer.IndexText(ctx, capture.WorkspaceID, capture.SourceID, capture.ID, id.ID{}, capture.SHA256, int64(len(content)), string(content), capture.CapturedAt)
}

func (s *Sources) captureBytes(ctx context.Context, workspace, source, author id.ID, version int, mediaType string, content []byte) (domain.Capture, error) {
	if workspace.IsZero() || source.IsZero() || author.IsZero() {
		return domain.Capture{}, domain.ErrInvalid
	}
	if domainBinary := mediaType == "application/pdf" || mediaType == "image/png" || mediaType == "image/jpeg" || mediaType == "image/webp"; domainBinary {
		if err := domain.ValidateBinary(mediaType, content); err != nil {
			return domain.Capture{}, err
		}
	} else {
		var err error
		mediaType, err = domain.ValidateContent(mediaType, string(content))
		if err != nil {
			return domain.Capture{}, err
		}
	}
	info, err := s.blobs.Put(ctx, bytes.NewReader(content))
	if err != nil {
		return domain.Capture{}, err
	}
	return domain.NewCapture(s.ids.NewID(), source, workspace, author, version, mediaType, info, s.clock.Now())
}

func (s *Sources) emit(ctx context.Context, name string, workspace, author, source id.ID, capture *domain.Capture) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, s.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	payload := map[string]any{"workspace_id": workspace.String(), "source_id": source.String(), "author": author.String()}
	if capture != nil {
		payload["capture_id"] = capture.ID.String()
		payload["sha256"] = capture.SHA256
		payload["version"] = capture.Version
	}
	event, err := events.NewDecision(s.ids, s.clock, name, "workspace:"+workspace.String(), prov, payload)
	if err != nil {
		return err
	}
	return s.publisher.Publish(ctx, event)
}

func (s *Sources) emitIntake(ctx context.Context, name string, candidate domain.IntakeCandidate, actor id.ID) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, s.ids)
	}
	prov, err := prov.WithTenant(candidate.WorkspaceID.String())
	if err != nil {
		return err
	}
	payload := map[string]any{
		"workspace_id": candidate.WorkspaceID.String(), "intake_id": candidate.ID.String(),
		"actor": actor.String(), "title": candidate.Title, "url": candidate.URL, "status": candidate.Status,
	}
	if !candidate.SourceID.IsZero() {
		payload["source_id"] = candidate.SourceID.String()
	}
	if candidate.ReviewNote != "" {
		payload["review_note"] = candidate.ReviewNote
	}
	event, err := events.NewDecision(s.ids, s.clock, name, "workspace:"+candidate.WorkspaceID.String(), prov, payload)
	if err != nil {
		return err
	}
	return s.publisher.Publish(ctx, event)
}

func (s *Sources) emitWatch(ctx context.Context, name string, watch domain.Watch, actor id.ID, runError string) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, s.ids)
	}
	prov, err := prov.WithTenant(watch.WorkspaceID.String())
	if err != nil {
		return err
	}
	payload := map[string]any{
		"workspace_id": watch.WorkspaceID.String(), "source_id": watch.SourceID.String(),
		"actor": actor.String(), "enabled": watch.Enabled, "interval_seconds": watch.IntervalSeconds,
		"last_status": watch.LastStatus,
	}
	if !watch.LastCaptureID.IsZero() {
		payload["capture_id"] = watch.LastCaptureID.String()
	}
	if strings.TrimSpace(runError) != "" {
		payload["error"] = strings.TrimSpace(runError)
	}
	event, err := events.NewDecision(s.ids, s.clock, name, "workspace:"+watch.WorkspaceID.String(), prov, payload)
	if err != nil {
		return err
	}
	return s.publisher.Publish(ctx, event)
}

func (s *Sources) emitRetention(ctx context.Context, workspace, source, author id.ID, until *time.Time) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, s.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	payload := map[string]any{"workspace_id": workspace.String(), "source_id": source.String(), "author": author.String()}
	if until != nil {
		payload["retention_until"] = until.UTC().Format(time.RFC3339Nano)
	}
	event, err := events.NewDecision(s.ids, s.clock, domain.EventRetentionChanged, "workspace:"+workspace.String(), prov, payload)
	if err != nil {
		return err
	}
	return s.publisher.Publish(ctx, event)
}

func (s *Sources) emitPublication(ctx context.Context, workspace, source, author id.ID, publishedAt *time.Time) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, s.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	payload := map[string]any{"workspace_id": workspace.String(), "source_id": source.String(), "author": author.String(), "cleared": publishedAt == nil}
	if publishedAt != nil {
		payload["published_at"] = publishedAt.UTC().Format(time.RFC3339Nano)
	} else {
		payload["published_at"] = nil
	}
	event, err := events.NewDecision(s.ids, s.clock, domain.EventPublicationChanged, "workspace:"+workspace.String(), prov, payload)
	if err != nil {
		return err
	}
	return s.publisher.Publish(ctx, event)
}

func (s *Sources) emitDuplicatePolicy(ctx context.Context, workspace, source, author id.ID, policy string) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, s.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	payload := map[string]any{"workspace_id": workspace.String(), "source_id": source.String(), "author": author.String(), "duplicate_policy": policy}
	event, err := events.NewDecision(s.ids, s.clock, domain.EventDuplicatePolicyChanged, "workspace:"+workspace.String(), prov, payload)
	if err != nil {
		return err
	}
	return s.publisher.Publish(ctx, event)
}

func (s *Sources) emitPrivacy(ctx context.Context, workspace, source, author id.ID, sensitivity string, legalHold bool, reason string) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, s.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	payload := map[string]any{"workspace_id": workspace.String(), "source_id": source.String(), "author": author.String(), "sensitivity": sensitivity, "legal_hold": legalHold}
	if reason != "" {
		payload["legal_hold_reason"] = reason
	}
	event, err := events.NewDecision(s.ids, s.clock, domain.EventPrivacyChanged, "workspace:"+workspace.String(), prov, payload)
	if err != nil {
		return err
	}
	return s.publisher.Publish(ctx, event)
}

func (s *Sources) emitPurge(ctx context.Context, workspace, source, author id.ID, reason string, dependencies domain.PurgeDependencies) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, s.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	payload := map[string]any{"workspace_id": workspace.String(), "source_id": source.String(), "author": author.String(), "reason": reason, "dependencies": dependencies}
	event, err := events.NewDecision(s.ids, s.clock, domain.EventPurged, "workspace:"+workspace.String(), prov, payload)
	if err != nil {
		return err
	}
	return s.publisher.Publish(ctx, event)
}
