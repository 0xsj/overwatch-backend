package command

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/observation/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type ManualRepository interface {
	CreateManual(context.Context, domain.Manual) error
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
