package command

import (
	"bytes"
	"context"
	"io"
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
	SetRetention(context.Context, id.ID, id.ID, id.ID, *time.Time, time.Time) error
	// NextVersion must lock the source row until the enclosing transaction ends.
	NextVersion(ctx context.Context, workspace, source id.ID) (int, error)
}

// LifecycleRepository is optional so the original capture command port stays
// small for adapters and tests. The production PostgreSQL store implements it
// to make privacy changes and purge checks part of the same source boundary.
type LifecycleRepository interface {
	ByID(context.Context, id.ID, id.ID) (domain.Summary, error)
	LockForPurge(context.Context, id.ID, id.ID) (domain.Summary, domain.PurgeDependencies, error)
	SetPrivacy(context.Context, id.ID, id.ID, id.ID, string, bool, string, time.Time) error
	MarkPurged(context.Context, id.ID, id.ID, id.ID, string, time.Time) error
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
