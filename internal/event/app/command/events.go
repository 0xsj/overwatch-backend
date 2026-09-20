package command

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/event/domain"
	recorddomain "github.com/0xsj/overwatch-backend/internal/researchentity/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type Repository interface {
	Create(context.Context, domain.Event) error
	ByID(context.Context, id.ID, id.ID) (domain.Event, error)
	Save(context.Context, domain.Event) error
	ReplaceObservations(context.Context, id.ID, id.ID, []id.ID) error
	ReplaceParticipants(context.Context, id.ID, id.ID, []id.ID) error
	ReplaceParticipantLinks(context.Context, id.ID, id.ID, []domain.ParticipantLink) error
	CreateRevision(context.Context, domain.Revision) error
}
type Records interface {
	ByID(context.Context, id.ID, id.ID) (recorddomain.Record, error)
}
type Transactor interface {
	InTx(context.Context, func(context.Context) error) error
}
type Minter interface{ NewID() id.ID }
type Clock interface{ Now() time.Time }

type Events struct {
	repo      Repository
	records   Records
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewEvents(repo Repository, records Records, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Events {
	if repo == nil || records == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("event: NewEvents with a nil dependency")
	}
	return &Events{repo: repo, records: records, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (e *Events) Create(ctx context.Context, workspace, author id.ID, title, description, reportedTime, precision, sortDate, location string, observations []id.ID) (domain.Event, error) {
	return e.CreateWithLinks(ctx, workspace, author, title, description, reportedTime, precision, sortDate, location, observations, nil, nil)
}

func (e *Events) CreateWithLinks(ctx context.Context, workspace, author id.ID, title, description, reportedTime, precision, sortDate, location string, observations, participants []id.ID, locationRecord *id.ID) (domain.Event, error) {
	return e.CreateWithParticipantLinks(ctx, workspace, author, title, description, reportedTime, precision, sortDate, location, observations, participantLinksFromIDs(participants), locationRecord)
}

func (e *Events) CreateWithParticipantLinks(ctx context.Context, workspace, author id.ID, title, description, reportedTime, precision, sortDate, location string, observations []id.ID, participants []domain.ParticipantLink, locationRecord *id.ID) (domain.Event, error) {
	fresh, err := domain.NewWithParticipantLinks(e.ids.NewID(), workspace, author, title, description, reportedTime, precision, sortDate, location, observations, participants, locationRecord, e.clock.Now())
	if err != nil {
		return domain.Event{}, err
	}
	if err := e.validateRecords(ctx, workspace, fresh.ParticipantLinks, fresh.LocationRecordID); err != nil {
		return domain.Event{}, err
	}
	if err := e.tx.InTx(ctx, func(ctx context.Context) error {
		if err := e.repo.Create(ctx, fresh); err != nil {
			return err
		}
		if err := e.repo.ReplaceObservations(ctx, workspace, fresh.ID, fresh.ObservationIDs); err != nil {
			return err
		}
		if err := e.repo.ReplaceParticipantLinks(ctx, workspace, fresh.ID, fresh.ParticipantLinks); err != nil {
			return err
		}
		revision, err := e.revision(ctx, fresh)
		if err != nil {
			return err
		}
		if err := e.repo.CreateRevision(ctx, revision); err != nil {
			return err
		}
		return e.publish(ctx, workspace, fresh, false)
	}); err != nil {
		return domain.Event{}, err
	}
	return fresh, nil
}

func (e *Events) Edit(ctx context.Context, workspace, want, editor id.ID, title, description, reportedTime, precision, sortDate, location string, observations []id.ID) (domain.Event, error) {
	held, err := e.repo.ByID(ctx, workspace, want)
	if err != nil {
		return domain.Event{}, err
	}
	return e.EditWithLinks(ctx, workspace, want, editor, title, description, reportedTime, precision, sortDate, location, observations, held.ParticipantRecordIDs, held.LocationRecordID)
}

func (e *Events) EditWithLinks(ctx context.Context, workspace, want, editor id.ID, title, description, reportedTime, precision, sortDate, location string, observations, participants []id.ID, locationRecord *id.ID) (domain.Event, error) {
	return e.EditWithParticipantLinks(ctx, workspace, want, editor, title, description, reportedTime, precision, sortDate, location, observations, participantLinksFromIDs(participants), locationRecord)
}

func (e *Events) EditWithParticipantLinks(ctx context.Context, workspace, want, editor id.ID, title, description, reportedTime, precision, sortDate, location string, observations []id.ID, participants []domain.ParticipantLink, locationRecord *id.ID) (domain.Event, error) {
	held, err := e.repo.ByID(ctx, workspace, want)
	if err != nil {
		return domain.Event{}, err
	}
	next, err := held.EditWithParticipantLinks(editor, title, description, reportedTime, precision, sortDate, location, observations, participants, locationRecord, e.clock.Now())
	if err != nil {
		return domain.Event{}, err
	}
	if err := e.validateRecords(ctx, workspace, next.ParticipantLinks, next.LocationRecordID); err != nil {
		return domain.Event{}, err
	}
	if err := e.tx.InTx(ctx, func(ctx context.Context) error {
		if err := e.repo.Save(ctx, next); err != nil {
			return err
		}
		if err := e.repo.ReplaceObservations(ctx, workspace, next.ID, next.ObservationIDs); err != nil {
			return err
		}
		if err := e.repo.ReplaceParticipantLinks(ctx, workspace, next.ID, next.ParticipantLinks); err != nil {
			return err
		}
		revision, err := e.revision(ctx, next)
		if err != nil {
			return err
		}
		if err := e.repo.CreateRevision(ctx, revision); err != nil {
			return err
		}
		return e.publish(ctx, workspace, next, true)
	}); err != nil {
		return domain.Event{}, err
	}
	return next, nil
}

func (e *Events) revision(ctx context.Context, event domain.Event) (domain.Revision, error) {
	out := domain.Revision{
		ID:                   e.ids.NewID(),
		WorkspaceID:          event.WorkspaceID,
		EventID:              event.ID,
		Title:                event.Title,
		Description:          event.Description,
		ReportedTime:         event.ReportedTime,
		TimePrecision:        event.TimePrecision,
		SortDate:             event.SortDate,
		Location:             event.Location,
		ObservationIDs:       append([]id.ID(nil), event.ObservationIDs...),
		ParticipantRecordIDs: append([]id.ID(nil), event.ParticipantRecordIDs...),
		ParticipantLinks:     append([]domain.ParticipantLink(nil), event.ParticipantLinks...),
		LocationRecordID:     copyID(event.LocationRecordID),
		ChangedBy:            event.UpdatedBy,
		ChangedAt:            event.UpdatedAt,
	}
	for _, participant := range event.ParticipantLinks {
		record, err := e.records.ByID(ctx, event.WorkspaceID, participant.RecordID)
		if err != nil {
			return domain.Revision{}, err
		}
		out.ParticipantRecords = append(out.ParticipantRecords, domain.RecordSnapshot{
			ID: record.ID, Kind: record.Kind.String(), Name: record.Name, Description: record.Description,
			ObservationIDs: append([]id.ID(nil), record.ObservationIDs...), PlaceGeometry: placeGeometrySnapshot(record.PlaceGeometry), Role: participant.Role,
		})
	}
	if event.LocationRecordID != nil {
		record, err := e.records.ByID(ctx, event.WorkspaceID, *event.LocationRecordID)
		if err != nil {
			return domain.Revision{}, err
		}
		out.LocationRecord = &domain.RecordSnapshot{
			ID: record.ID, Kind: record.Kind.String(), Name: record.Name, Description: record.Description,
			ObservationIDs: append([]id.ID(nil), record.ObservationIDs...), PlaceGeometry: placeGeometrySnapshot(record.PlaceGeometry),
		}
	}
	return out, nil
}

func placeGeometrySnapshot(input *recorddomain.PlaceGeometry) *domain.PlaceGeometrySnapshot {
	if input == nil {
		return nil
	}
	return &domain.PlaceGeometrySnapshot{Latitude: input.Latitude, Longitude: input.Longitude, Precision: input.Precision.String(), ObservationIDs: append([]id.ID(nil), input.ObservationIDs...)}
}

func copyID(value *id.ID) *id.ID {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

func (e *Events) validateRecords(ctx context.Context, workspace id.ID, participants []domain.ParticipantLink, location *id.ID) error {
	for _, participant := range participants {
		if _, err := e.records.ByID(ctx, workspace, participant.RecordID); err != nil {
			return err
		}
	}
	if location != nil {
		record, err := e.records.ByID(ctx, workspace, *location)
		if err != nil {
			return err
		}
		if record.Kind != recorddomain.Place {
			return domain.ErrLocationRecordKind
		}
	}
	return nil
}

func participantLinksFromIDs(input []id.ID) []domain.ParticipantLink {
	links := make([]domain.ParticipantLink, 0, len(input))
	for _, recordID := range input {
		links = append(links, domain.ParticipantLink{RecordID: recordID, Role: domain.ParticipantAssociated})
	}
	return links
}

func (e *Events) publish(ctx context.Context, workspace id.ID, event domain.Event, edit bool) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, e.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	out, err := events.NewDecision(e.ids, e.clock, domain.EventChanged, "workspace:"+workspace.String(), prov, map[string]any{
		"workspace_id": workspace.String(), "event_id": event.ID.String(), "updated_by": event.UpdatedBy.String(), "edit": edit,
	})
	if err != nil {
		return err
	}
	return e.publisher.Publish(ctx, out)
}
