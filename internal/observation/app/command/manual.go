package command

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"time"

	"github.com/0xsj/overwatch-backend/internal/observation/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type ManualRepository interface {
	CreateManual(context.Context, domain.Manual) error
}

type citationShareRepository interface {
	CreateCitationShare(context.Context, domain.CitationShare) error
	RevokeCitationShare(context.Context, id.ID, id.ID, id.ID, time.Time) (domain.CitationShare, error)
}
type ManualTransactor interface {
	InTx(context.Context, func(context.Context) error) error
}

// Captures is adapted by root from the source domain. Keeping the workspace
// and source in the answer lets this command reject an incorrectly wired port.
type Captures interface {
	Retained(ctx context.Context, workspace, source, capture, extraction id.ID) (RetainedCapture, error)
}
type RetainedCapture struct {
	WorkspaceID, SourceID, CaptureID id.ID
	ExtractionID                     id.ID
	MediaType                        string
	Content                          string
}
type ManualDraft struct {
	CaptureID    id.ID  `json:"capture_id"`
	ExtractionID id.ID  `json:"extraction_id,omitempty"`
	Statement    string `json:"statement"`
	Quote        string `json:"quote"`
	QuoteStart   *int   `json:"quote_start,omitempty"`
	Locator      string `json:"locator,omitempty"`
}
type ManualObservations struct {
	repo      ManualRepository
	captures  Captures
	tx        ManualTransactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewManualObservations(repo ManualRepository, captures Captures, tx ManualTransactor, publisher events.Publisher, ids Minter, clock Clock) *ManualObservations {
	if repo == nil || captures == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("observation: NewManualObservations with a nil dependency")
	}
	return &ManualObservations{repo, captures, tx, publisher, ids, clock}
}
func (m *ManualObservations) Record(ctx context.Context, workspace, source, author id.ID, in ManualDraft) (domain.Manual, error) {
	if workspace.IsZero() || source.IsZero() || author.IsZero() || in.CaptureID.IsZero() {
		return domain.Manual{}, domain.ErrIDRequired
	}
	retained, err := m.captures.Retained(ctx, workspace, source, in.CaptureID, in.ExtractionID)
	if err != nil {
		return domain.Manual{}, err
	}
	if retained.WorkspaceID != workspace || retained.SourceID != source || retained.CaptureID != in.CaptureID || retained.ExtractionID != in.ExtractionID {
		return domain.Manual{}, domain.ErrNotFound
	}
	if in.ExtractionID.IsZero() && retained.MediaType != "text/plain" && retained.MediaType != "text/html" && retained.MediaType != "application/json" {
		return domain.Manual{}, domain.ErrTextCaptureRequired
	}
	fresh, err := domain.NewManual(m.ids.NewID(), workspace, source, in.CaptureID, author, in.Statement, in.Quote, in.Locator, retained.Content, in.QuoteStart, m.clock.Now())
	if err != nil {
		return domain.Manual{}, err
	}
	if !in.ExtractionID.IsZero() {
		extraction := in.ExtractionID
		fresh.ExtractionID = &extraction
	}
	err = m.tx.InTx(ctx, func(ctx context.Context) error {
		if err := m.repo.CreateManual(ctx, fresh); err != nil {
			return err
		}
		prov, ok := provenance.Current(ctx)
		if !ok {
			prov = provenance.New(provenance.OriginRequest, m.ids)
		}
		prov, err = prov.WithTenant(workspace.String())
		if err != nil {
			return err
		}
		event, err := events.NewDecision(m.ids, m.clock, domain.EventManualRecorded, "workspace:"+workspace.String(), prov, map[string]string{"workspace_id": workspace.String(), "source_id": source.String(), "capture_id": in.CaptureID.String(), "observation_id": fresh.ID.String(), "author": author.String()})
		if err != nil {
			return err
		}
		return m.publisher.Publish(ctx, event)
	})
	if err != nil {
		return domain.Manual{}, err
	}
	return fresh, nil
}

// CreateCitationShare mints an opaque link for one exact observation. The
// caller receives the raw token once; only its digest enters the repository.
func (m *ManualObservations) CreateCitationShare(ctx context.Context, workspace, source, observation, createdBy id.ID) (domain.CitationShare, string, error) {
	if workspace.IsZero() || source.IsZero() || observation.IsZero() || createdBy.IsZero() {
		return domain.CitationShare{}, "", domain.ErrIDRequired
	}
	repo, ok := m.repo.(citationShareRepository)
	if !ok {
		return domain.CitationShare{}, "", domain.ErrNotFound
	}
	tokenID := m.ids.NewID()
	token := base64.RawURLEncoding.EncodeToString(tokenID[:])
	digest := sha256.Sum256([]byte(token))
	share, err := domain.NewCitationShare(m.ids.NewID(), workspace, source, observation, createdBy, hex.EncodeToString(digest[:]), m.clock.Now())
	if err != nil {
		return domain.CitationShare{}, "", err
	}
	if err := m.tx.InTx(ctx, func(ctx context.Context) error {
		if err := repo.CreateCitationShare(ctx, share); err != nil {
			return err
		}
		return m.publishCitationShareCreated(ctx, workspace, share)
	}); err != nil {
		return domain.CitationShare{}, "", err
	}
	return share, token, nil
}

func (m *ManualObservations) RevokeCitationShare(ctx context.Context, workspace, share, revokedBy id.ID) (domain.CitationShare, error) {
	if workspace.IsZero() || share.IsZero() || revokedBy.IsZero() {
		return domain.CitationShare{}, domain.ErrIDRequired
	}
	repo, ok := m.repo.(citationShareRepository)
	if !ok {
		return domain.CitationShare{}, domain.ErrNotFound
	}
	var revoked domain.CitationShare
	if err := m.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		revoked, err = repo.RevokeCitationShare(ctx, workspace, share, revokedBy, m.clock.Now())
		if err != nil {
			return err
		}
		return m.publishCitationShareRevoked(ctx, workspace, revoked)
	}); err != nil {
		return domain.CitationShare{}, err
	}
	return revoked, nil
}

func (m *ManualObservations) RecordCitationShareAccess(ctx context.Context, workspace, source, observation, share, accessedBy id.ID, accessMode string) error {
	if workspace.IsZero() || source.IsZero() || observation.IsZero() || share.IsZero() || accessedBy.IsZero() {
		return domain.ErrIDRequired
	}
	if accessMode != "shared" {
		return domain.ErrInvalidShareAccessMode
	}
	return m.publishCitationShareAccessed(ctx, workspace, source, observation, share, accessMode)
}

func (m *ManualObservations) publishCitationShareCreated(ctx context.Context, workspace id.ID, share domain.CitationShare) error {
	return m.publishCitationEvent(ctx, workspace, domain.EventCitationShareCreated, domain.CitationShareCreated{WorkspaceID: workspace.String(), SourceID: share.SourceID.String(), ObservationID: share.ObservationID.String(), ShareID: share.ID.String(), CreatedBy: share.CreatedBy.String()})
}

func (m *ManualObservations) publishCitationShareRevoked(ctx context.Context, workspace id.ID, share domain.CitationShare) error {
	revokedBy := ""
	if share.RevokedBy != nil {
		revokedBy = share.RevokedBy.String()
	}
	return m.publishCitationEvent(ctx, workspace, domain.EventCitationShareRevoked, domain.CitationShareRevoked{WorkspaceID: workspace.String(), SourceID: share.SourceID.String(), ObservationID: share.ObservationID.String(), ShareID: share.ID.String(), RevokedBy: revokedBy})
}

func (m *ManualObservations) publishCitationShareAccessed(ctx context.Context, workspace, source, observation, share id.ID, accessMode string) error {
	return m.publishCitationEvent(ctx, workspace, domain.EventCitationShareAccessed, domain.CitationShareAccessed{WorkspaceID: workspace.String(), SourceID: source.String(), ObservationID: observation.String(), ShareID: share.String(), AccessMode: accessMode})
}

func (m *ManualObservations) publishCitationEvent(ctx context.Context, workspace id.ID, kind string, payload any) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, m.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	event, err := events.NewDecision(m.ids, m.clock, kind, "workspace:"+workspace.String(), prov, payload)
	if err != nil {
		return err
	}
	return m.publisher.Publish(ctx, event)
}
