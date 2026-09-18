package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/internal/artifactcleanup/domain"
	"github.com/0xsj/overwatch-backend/pkg/blob"
	pkgerrors "github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type Repository interface {
	Inventory(context.Context, id.ID, int) (domain.Inventory, error)
	References(context.Context, id.ID, string, string, int) ([]domain.Reference, error)
	LastSweepItems(context.Context, id.ID, int) (map[string]domain.SweepItem, error)
	OpenReview(context.Context, id.ID) (domain.Review, error)
	LatestReview(context.Context, id.ID) (domain.Review, error)
	ReadReview(context.Context, id.ID, id.ID) (domain.Review, error)
	SaveReview(context.Context, domain.Review) error
	RecentReviews(context.Context, id.ID, int) ([]domain.ReviewSummary, error)
	DiscardReview(context.Context, domain.Review) error
	RecordSweepItem(context.Context, domain.SweepItem) error
	StartSweep(context.Context, domain.SweepRun) error
	FinishSweep(context.Context, domain.SweepRun) error
	FailSweep(context.Context, domain.SweepRun) error
	RecentSweeps(context.Context, id.ID, int) ([]domain.SweepRun, error)
}

type Blobs interface {
	Stat(context.Context, blob.Ref) (blob.Info, error)
	Verify(context.Context, blob.Ref) error
	Remove(context.Context, blob.Ref) error
}

type Publisher interface {
	Publish(context.Context, ...events.Event) error
}

type Minter interface{ NewID() id.ID }
type Clock interface{ Now() time.Time }

type Service struct {
	repo      Repository
	blobs     Blobs
	publisher Publisher
	ids       Minter
	clock     Clock
}

func New(repo Repository, blobs Blobs, publisher Publisher, ids Minter, clock Clock) *Service {
	if repo == nil || blobs == nil || publisher == nil || ids == nil || clock == nil {
		panic("artifact cleanup: New with a nil dependency")
	}
	return &Service{repo: repo, blobs: blobs, publisher: publisher, ids: ids, clock: clock}
}

func (s *Service) Inventory(ctx context.Context, workspace id.ID, limit int) (domain.Inventory, error) {
	if workspace.IsZero() {
		return domain.Inventory{}, fmt.Errorf("artifact cleanup: workspace is required")
	}
	limit = normalizeLimit(limit)
	out, err := s.inventoryCandidates(ctx, workspace, limit)
	if err != nil {
		return domain.Inventory{}, err
	}
	out.RecentSweeps, err = s.repo.RecentSweeps(ctx, workspace, 10)
	if err != nil {
		return domain.Inventory{}, err
	}
	if out.RecentSweeps == nil {
		out.RecentSweeps = []domain.SweepRun{}
	}
	return out, nil
}

func (s *Service) Review(ctx context.Context, workspace id.ID) (domain.Review, error) {
	if workspace.IsZero() {
		return domain.Review{}, fmt.Errorf("artifact cleanup: workspace is required")
	}
	review, err := s.repo.LatestReview(ctx, workspace)
	if err != nil {
		return domain.Review{}, err
	}
	if review.Items == nil {
		review.Items = []domain.Candidate{}
	}
	if review.SelectedRefs == nil {
		review.SelectedRefs = review.Refs()
	}
	return review, nil
}

func (s *Service) ReviewHistory(ctx context.Context, workspace id.ID, limit int) (domain.ReviewPage, error) {
	if workspace.IsZero() {
		return domain.ReviewPage{}, fmt.Errorf("artifact cleanup: workspace is required")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	items, err := s.repo.RecentReviews(ctx, workspace, limit)
	if err != nil {
		return domain.ReviewPage{}, err
	}
	if items == nil {
		items = []domain.ReviewSummary{}
	}
	return domain.ReviewPage{Items: items, Count: len(items)}, nil
}

func (s *Service) DiscardReview(ctx context.Context, workspace, actor, reviewID id.ID, reason string) error {
	if workspace.IsZero() {
		return fmt.Errorf("artifact cleanup: workspace is required")
	}
	if actor.IsZero() || reviewID.IsZero() {
		return fmt.Errorf("artifact cleanup: actor and review are required")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return pkgerrors.New(pkgerrors.Invalid, "discarding a cleanup review requires a reason")
	}
	if len([]byte(reason)) > 4000 {
		return pkgerrors.New(pkgerrors.Invalid, "cleanup review discard reason is too long")
	}
	review, err := s.repo.ReadReview(ctx, workspace, reviewID)
	if err != nil {
		return err
	}
	if review.Status != domain.ReviewOpen {
		return pkgerrors.New(pkgerrors.PreconditionRequired, "only an open cleanup review can be discarded")
	}
	now := s.clock.Now()
	review.Status = domain.ReviewDiscarded
	review.UpdatedBy = actor
	review.UpdatedAt = now
	review.DiscardedBy = &actor
	review.DiscardedAt = &now
	review.DiscardReason = reason
	return s.repo.DiscardReview(ctx, review)
}

func (s *Service) SaveReview(ctx context.Context, workspace, actor id.ID, selectedRefs []string) (domain.Review, error) {
	if workspace.IsZero() {
		return domain.Review{}, fmt.Errorf("artifact cleanup: workspace is required")
	}
	if actor.IsZero() {
		return domain.Review{}, fmt.Errorf("artifact cleanup: actor is required")
	}
	selected, err := normalizeSelectedRefs(selectedRefs)
	if err != nil {
		return domain.Review{}, err
	}
	if len(selected) == 0 {
		return domain.Review{}, pkgerrors.New(pkgerrors.PreconditionRequired, "select at least one artifact before saving a cleanup review")
	}
	inventory, err := s.inventoryCandidates(ctx, workspace, normalizeLimit(len(selected)))
	if err != nil {
		return domain.Review{}, err
	}
	filtered, ok := filterInventory(inventory, selected)
	if !ok {
		return domain.Review{}, pkgerrors.New(pkgerrors.PreconditionRequired, "one or more selected artifacts are no longer eligible for cleanup")
	}
	open, err := s.repo.OpenReview(ctx, workspace)
	if err != nil {
		return domain.Review{}, err
	}
	if open.Status != domain.ReviewOpen {
		open = domain.Review{}
	}
	now := s.clock.Now()
	review := domain.Review{
		ID:           open.ID,
		WorkspaceID:  workspace,
		CreatedBy:    open.CreatedBy,
		UpdatedBy:    actor,
		Status:       domain.ReviewOpen,
		Items:        filtered.Items,
		SelectedRefs: make([]string, 0, len(filtered.Items)),
		CreatedAt:    open.CreatedAt,
		UpdatedAt:    now,
	}
	if review.ID.IsZero() {
		review.ID = s.ids.NewID()
		review.CreatedBy = actor
		review.CreatedAt = now
	}
	for _, item := range review.Items {
		review.SelectedRefs = append(review.SelectedRefs, item.Ref)
	}
	if err := s.repo.SaveReview(ctx, review); err != nil {
		return domain.Review{}, err
	}
	return review, nil
}

func (s *Service) Status(ctx context.Context, workspace id.ID, before, exactRef, state string, limit int) (domain.StatusPage, error) {
	if workspace.IsZero() {
		return domain.StatusPage{}, fmt.Errorf("artifact cleanup: workspace is required")
	}
	limit = normalizeLimit(limit)
	if state != "" && !validState(state) {
		return domain.StatusPage{}, fmt.Errorf("artifact cleanup: unknown lifecycle state %q", state)
	}
	beforeHex, err := referenceFilter(before)
	if err != nil {
		return domain.StatusPage{}, err
	}
	exactHex, err := referenceFilter(exactRef)
	if err != nil {
		return domain.StatusPage{}, err
	}
	references, err := s.repo.References(ctx, workspace, beforeHex, exactHex, min(101, limit+1))
	if err != nil {
		return domain.StatusPage{}, err
	}
	last, err := s.repo.LastSweepItems(ctx, workspace, min(101, limit+1))
	if err != nil {
		return domain.StatusPage{}, err
	}
	page := domain.StatusPage{Items: make([]domain.ArtifactStatus, 0, len(references)), StateCounts: map[string]int{}}
	for _, reference := range references {
		status := domain.ArtifactStatus{
			Ref:                 reference.Ref,
			Kind:                reference.Kind,
			WorkspaceID:         reference.WorkspaceID,
			SourceID:            reference.SourceID,
			CaptureID:           reference.CaptureID,
			ExtractionID:        reference.ExtractionID,
			Bytes:               reference.Bytes,
			PurgedAt:            reference.PurgedAt,
			CreatedAt:           reference.CreatedAt,
			ReferenceCount:      reference.ReferenceCount,
			LiveSourceCount:     reference.LiveSourceCount,
			LiveExtractionCount: reference.LiveExtractionCount,
			RunReferenced:       reference.RunReferenced,
			ReportReferenced:    reference.ReportReferenced,
		}
		var item *domain.SweepItem
		if found, ok := last[reference.Ref]; ok {
			item = &found
			lastID := found.SweepID
			status.LastSweepID = &lastID
			status.LastSweepOutcome = found.Outcome
			recorded := found.RecordedAt
			status.LastSweepAt = &recorded
		}
		status.State, status.Reason = s.referenceState(ctx, reference, item)
		if state != "" && status.State != state {
			continue
		}
		page.Items = append(page.Items, status)
		page.StateCounts[status.State]++
	}
	if len(page.Items) > limit {
		cursor := page.Items[limit-1].Ref
		page.NextCursor = &cursor
		page.Items = page.Items[:limit]
	} else if state != "" && len(references) > limit {
		cursor := references[len(references)-1].Ref
		page.NextCursor = &cursor
	} else if state == "" && len(references) > limit {
		cursor := page.Items[len(page.Items)-1].Ref
		page.NextCursor = &cursor
	}
	page.Count = len(page.Items)
	return page, nil
}

func validState(state string) bool {
	switch state {
	case domain.StateProtected, domain.StateEligible, domain.StateMissing, domain.StateSwept, domain.StateAlreadyGone, domain.StateMismatched, domain.StateCorrupted, domain.StateUnavailable:
		return true
	default:
		return false
	}
}

func referenceFilter(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	ref, err := blob.ParseRef(value)
	if err != nil {
		return "", fmt.Errorf("artifact cleanup: invalid content reference")
	}
	return ref.Hex, nil
}

func (s *Service) referenceState(ctx context.Context, reference domain.Reference, last *domain.SweepItem) (string, string) {
	switch {
	case reference.LiveSourceCount > 0:
		return domain.StateProtected, "Referenced by a live source."
	case reference.LiveExtractionCount > 0:
		return domain.StateProtected, "Referenced by a live extraction."
	case reference.RunReferenced:
		return domain.StateProtected, "Referenced by a run artifact."
	case reference.ReportReferenced:
		return domain.StateProtected, "Referenced by a report revision."
	case reference.PurgedAt == nil:
		return domain.StateProtected, "The owning source has not been purged."
	}
	ref, err := blob.ParseRef(reference.Ref)
	if err != nil {
		return domain.StateUnavailable, "The durable reference is malformed."
	}
	info, err := s.blobs.Stat(ctx, ref)
	if errors.Is(err, blob.ErrNotFound) {
		if last != nil && last.Outcome == domain.OutcomeDeleted {
			return domain.StateSwept, "Removed by the recorded cleanup sweep."
		}
		if last != nil && last.Outcome == domain.OutcomeAlreadyGone {
			return domain.StateAlreadyGone, "The bytes were already absent when the sweep checked."
		}
		return domain.StateMissing, "No bytes are present in blob storage."
	}
	if err != nil {
		return domain.StateUnavailable, "Blob storage could not be checked: " + err.Error()
	}
	if info.Size != reference.Bytes {
		return domain.StateMismatched, "Stored byte count does not match the durable reference."
	}
	if err := s.blobs.Verify(ctx, ref); errors.Is(err, blob.ErrNotFound) {
		return domain.StateMissing, "The bytes disappeared while they were being checked."
	} else if errors.Is(err, blob.ErrCorrupted) {
		return domain.StateCorrupted, "Stored bytes do not match their content address."
	} else if err != nil {
		return domain.StateUnavailable, "Blob storage could not verify the bytes: " + err.Error()
	}
	return domain.StateEligible, "Verified bytes have no live durable reference."
}

func (s *Service) inventoryCandidates(ctx context.Context, workspace id.ID, limit int) (domain.Inventory, error) {
	limit = normalizeLimit(limit)
	out, err := s.repo.Inventory(ctx, workspace, limit)
	if err != nil {
		return domain.Inventory{}, err
	}
	// The database is the authority for references, while the filesystem is the
	// authority for whether there is anything left to remove. A crash after an
	// unlink must therefore disappear from the next inventory rather than
	// remaining a perpetually "eligible" row.
	eligible := make([]domain.Candidate, 0, len(out.Items))
	for _, candidate := range out.Items {
		ref, err := blob.ParseRef(candidate.Ref)
		if err != nil {
			continue
		}
		info, err := s.blobs.Stat(ctx, ref)
		if errors.Is(err, blob.ErrNotFound) {
			continue
		}
		if err != nil {
			return domain.Inventory{}, err
		}
		if info.Size != candidate.Bytes {
			continue
		}
		eligible = append(eligible, candidate)
	}
	out.Items = eligible
	out.CandidateCount = len(eligible)
	out.CandidateBytes = 0
	for _, candidate := range eligible {
		out.CandidateBytes += candidate.Bytes
	}
	return out, nil
}

// Sweep re-checks the inventory immediately before touching bytes. A candidate
// is removed only when its strict content address parses, the on-disk size
// matches the durable row, and the bytes still hash to that address. The
// repository query excludes every live source, extraction, run, and report
// reference; no source/citation row is changed here. When selectedRefs is
// non-empty, every selected reference must still be in the current eligible
// inventory or the sweep is rejected before any bytes are touched.
func (s *Service) Sweep(ctx context.Context, workspace, actor id.ID, limit int, selectedRefs []string, reviewID id.ID) (domain.SweepResult, error) {
	if workspace.IsZero() {
		return domain.SweepResult{}, fmt.Errorf("artifact cleanup: workspace is required")
	}
	if actor.IsZero() {
		return domain.SweepResult{}, fmt.Errorf("artifact cleanup: actor is required")
	}
	selected, err := normalizeSelectedRefs(selectedRefs)
	if err != nil {
		return domain.SweepResult{}, err
	}
	limit = normalizeLimit(limit)
	if len(selected) > limit {
		limit = len(selected)
	}
	if !reviewID.IsZero() {
		review, err := s.repo.ReadReview(ctx, workspace, reviewID)
		if err != nil {
			return domain.SweepResult{}, err
		}
		if review.Status != domain.ReviewOpen || !sameRefs(review.SelectedRefs, selected) {
			return domain.SweepResult{}, pkgerrors.New(pkgerrors.PreconditionRequired, "the cleanup review is no longer open or does not match the selected artifacts")
		}
	}
	started := s.clock.Now()
	run := domain.SweepRun{ID: s.ids.NewID(), WorkspaceID: workspace, RequestedBy: actor, Status: domain.SweepRunning, Limit: limit, StartedAt: started}
	if !reviewID.IsZero() {
		run.ReviewID = &reviewID
	}
	if err := s.repo.StartSweep(ctx, run); err != nil {
		return domain.SweepResult{}, err
	}
	fail := func(result domain.SweepResult, cause error) (domain.SweepResult, error) {
		finished := s.clock.Now()
		run.Status = domain.SweepFailed
		run.CandidateCount = result.Inventory.CandidateCount
		run.CandidateBytes = result.Inventory.CandidateBytes
		run.DeletedCount = len(result.Deleted)
		run.DeletedBytes = result.DeletedBytes
		run.AlreadyGoneCount = len(result.AlreadyGone)
		run.SkippedCount = len(result.Skipped)
		run.Error = cause.Error()
		run.FinishedAt = &finished
		result.Run = run
		failureCtx := context.WithoutCancel(ctx)
		if err := s.repo.FailSweep(failureCtx, run); err != nil {
			return result, fmt.Errorf("%v; recording cleanup failure: %w", cause, err)
		}
		if err := s.publishFailure(failureCtx, workspace, actor, run); err != nil {
			return result, fmt.Errorf("%v; recording cleanup failure audit: %w", cause, err)
		}
		return result, cause
	}
	inventory, err := s.inventoryCandidates(ctx, workspace, limit)
	if err != nil {
		return fail(domain.SweepResult{Run: run}, err)
	}
	out := domain.SweepResult{Inventory: inventory, Deleted: []domain.Candidate{}, AlreadyGone: []domain.Candidate{}, Skipped: []domain.Candidate{}}
	if len(selected) > 0 {
		filtered, ok := filterInventory(inventory, selected)
		if !ok {
			return fail(out, pkgerrors.New(pkgerrors.PreconditionRequired, "one or more selected artifacts are no longer eligible for cleanup"))
		}
		inventory = filtered
		out.Inventory = inventory
	}
	recordItem := func(candidate domain.Candidate, outcome, reason string) error {
		return s.repo.RecordSweepItem(ctx, domain.SweepItem{
			SweepID:      run.ID,
			WorkspaceID:  workspace,
			Ref:          candidate.Ref,
			Kind:         candidate.Kind,
			SourceID:     candidate.SourceID,
			CaptureID:    candidate.CaptureID,
			ExtractionID: candidate.ExtractionID,
			Bytes:        candidate.Bytes,
			Outcome:      outcome,
			Reason:       reason,
			RecordedAt:   s.clock.Now(),
		})
	}
	for _, candidate := range inventory.Items {
		ref, err := blob.ParseRef(candidate.Ref)
		if err != nil {
			out.Skipped = append(out.Skipped, candidate)
			if recordErr := recordItem(candidate, domain.OutcomeSkipped, "invalid blob reference"); recordErr != nil {
				return fail(out, recordErr)
			}
			continue
		}
		info, err := s.blobs.Stat(ctx, ref)
		if errors.Is(err, blob.ErrNotFound) {
			out.AlreadyGone = append(out.AlreadyGone, candidate)
			if recordErr := recordItem(candidate, domain.OutcomeAlreadyGone, "bytes were absent when the sweep checked"); recordErr != nil {
				return fail(out, recordErr)
			}
			continue
		}
		if err != nil {
			if recordErr := recordItem(candidate, domain.OutcomeFailed, err.Error()); recordErr != nil {
				return fail(out, recordErr)
			}
			return fail(out, err)
		}
		if info.Size != candidate.Bytes {
			out.Skipped = append(out.Skipped, candidate)
			if recordErr := recordItem(candidate, domain.OutcomeSkipped, "stored byte count did not match the durable reference"); recordErr != nil {
				return fail(out, recordErr)
			}
			continue
		}
		if err := s.blobs.Verify(ctx, ref); err != nil {
			if errors.Is(err, blob.ErrNotFound) {
				out.AlreadyGone = append(out.AlreadyGone, candidate)
				if recordErr := recordItem(candidate, domain.OutcomeAlreadyGone, "bytes disappeared during verification"); recordErr != nil {
					return fail(out, recordErr)
				}
				continue
			}
			if errors.Is(err, blob.ErrCorrupted) {
				out.Skipped = append(out.Skipped, candidate)
				if recordErr := recordItem(candidate, domain.OutcomeSkipped, "stored bytes did not match their content address"); recordErr != nil {
					return fail(out, recordErr)
				}
				continue
			}
			if recordErr := recordItem(candidate, domain.OutcomeFailed, err.Error()); recordErr != nil {
				return fail(out, recordErr)
			}
			return fail(out, err)
		}
		if err := s.blobs.Remove(ctx, ref); err != nil {
			if errors.Is(err, blob.ErrNotFound) {
				out.AlreadyGone = append(out.AlreadyGone, candidate)
				if recordErr := recordItem(candidate, domain.OutcomeAlreadyGone, "bytes disappeared before removal"); recordErr != nil {
					return fail(out, recordErr)
				}
				continue
			}
			if recordErr := recordItem(candidate, domain.OutcomeFailed, err.Error()); recordErr != nil {
				return fail(out, recordErr)
			}
			return fail(out, err)
		}
		out.Deleted = append(out.Deleted, candidate)
		out.DeletedBytes += candidate.Bytes
		if recordErr := recordItem(candidate, domain.OutcomeDeleted, "verified and removed"); recordErr != nil {
			return fail(out, recordErr)
		}
	}
	if len(out.Deleted) > 0 {
		if err := s.publish(ctx, workspace, actor, out); err != nil {
			return fail(out, err)
		}
	}
	finished := s.clock.Now()
	run.Status = domain.SweepCompleted
	run.CandidateCount = inventory.CandidateCount
	run.CandidateBytes = inventory.CandidateBytes
	run.DeletedCount = len(out.Deleted)
	run.DeletedBytes = out.DeletedBytes
	run.AlreadyGoneCount = len(out.AlreadyGone)
	run.SkippedCount = len(out.Skipped)
	run.FinishedAt = &finished
	out.Run = run
	if err := s.repo.FinishSweep(ctx, run); err != nil {
		return fail(out, err)
	}
	out.Inventory.RecentSweeps = []domain.SweepRun{run}
	return out, nil
}

func filterInventory(inventory domain.Inventory, selected map[string]struct{}) (domain.Inventory, bool) {
	filtered := make([]domain.Candidate, 0, len(selected))
	for _, candidate := range inventory.Items {
		if _, ok := selected[candidate.Ref]; ok {
			filtered = append(filtered, candidate)
		}
	}
	if len(filtered) != len(selected) {
		return domain.Inventory{}, false
	}
	inventory.Items = filtered
	inventory.CandidateCount = len(filtered)
	inventory.CandidateBytes = 0
	for _, candidate := range filtered {
		inventory.CandidateBytes += candidate.Bytes
	}
	return inventory, true
}

func sameRefs(refs []string, selected map[string]struct{}) bool {
	if len(refs) != len(selected) {
		return false
	}
	for _, ref := range refs {
		if _, ok := selected[ref]; !ok {
			return false
		}
	}
	return true
}

func normalizeSelectedRefs(refs []string) (map[string]struct{}, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	if len(refs) > 100 {
		return nil, pkgerrors.New(pkgerrors.Invalid, "at most 100 artifacts may be selected for cleanup")
	}
	selected := make(map[string]struct{}, len(refs))
	for _, raw := range refs {
		ref, err := blob.ParseRef(strings.TrimSpace(raw))
		if err != nil {
			return nil, pkgerrors.New(pkgerrors.Invalid, "selected artifacts must use valid sha256 content references")
		}
		canonical := ref.String()
		if _, exists := selected[canonical]; exists {
			return nil, pkgerrors.New(pkgerrors.Invalid, "selected artifacts must not contain duplicates")
		}
		selected[canonical] = struct{}{}
	}
	return selected, nil
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Service) publish(ctx context.Context, workspace, actor id.ID, result domain.SweepResult) error {
	event, err := s.sweepEvent(ctx, workspace, actor, domain.EventSwept, map[string]any{
		"workspace_id":  workspace.String(),
		"actor_id":      actor.String(),
		"deleted_count": len(result.Deleted),
		"deleted_bytes": result.DeletedBytes,
		"refs":          deletedRefs(result.Deleted),
	})
	if err != nil {
		return err
	}
	return s.publisher.Publish(ctx, event)
}

func (s *Service) publishFailure(ctx context.Context, workspace, actor id.ID, run domain.SweepRun) error {
	event, err := s.sweepEvent(ctx, workspace, actor, domain.EventSweepFailed, map[string]any{
		"workspace_id":       workspace.String(),
		"actor_id":           actor.String(),
		"sweep_id":           run.ID.String(),
		"status":             run.Status,
		"error":              run.Error,
		"candidate_count":    run.CandidateCount,
		"deleted_count":      run.DeletedCount,
		"deleted_bytes":      run.DeletedBytes,
		"already_gone_count": run.AlreadyGoneCount,
		"skipped_count":      run.SkippedCount,
	})
	if err != nil {
		return err
	}
	return s.publisher.Publish(ctx, event)
}

func (s *Service) sweepEvent(ctx context.Context, workspace, actor id.ID, name string, payload map[string]any) (events.Event, error) {
	p, ok := provenance.Current(ctx)
	if !ok {
		p = provenance.New(provenance.OriginRequest, s.ids)
	}
	p, err := p.WithTenant(workspace.String())
	if err != nil {
		return events.Event{}, err
	}
	return events.NewDecision(s.ids, s.clock, name, "workspace:"+workspace.String(), p, payload)
}

func deletedRefs(candidates []domain.Candidate) []string {
	refs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		refs = append(refs, candidate.Ref)
	}
	return refs
}
