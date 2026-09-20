package postgres

import (
	"context"
	"embed"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/event/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const Schema = "timeline"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("event: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("event: NewStore with a nil pool")
	}
	return &Store{db: db}
}

func uuid(i id.ID) pgtype.UUID { return pgtype.UUID{Bytes: i, Valid: !i.IsZero()} }

func optionalUUID(i *id.ID) pgtype.UUID {
	if i == nil {
		return pgtype.UUID{}
	}
	return uuid(*i)
}

func translate(ctx context.Context, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return postgres.Translate(ctx, err, "timeline")
}

const eventSelect = `
select e.id,e.workspace_id,e.title,e.description,e.reported_time,e.time_precision,e.sort_date,e.location,e.location_record_id,
       e.author,e.updated_by,e.created_at,e.updated_at,
       coalesce((select json_agg(eo.observation_id order by eo.observation_id)
         from timeline.event_observation eo
         where eo.event_id=e.id and eo.workspace_id=e.workspace_id), '[]'::json)::text,
       coalesce((select json_agg(json_build_object('record_id', ep.record_id, 'role', ep.role) order by ep.record_id)
         from timeline.event_participant ep
         where ep.event_id=e.id and ep.workspace_id=e.workspace_id), '[]'::json)::text
from timeline.event e`

func scanEvent(row interface{ Scan(...any) error }) (domain.Event, error) {
	var out domain.Event
	var eventID, workspace, author, updatedBy, locationRecord pgtype.UUID
	var precision string
	var observationRaw, participantRaw []byte
	if err := row.Scan(&eventID, &workspace, &out.Title, &out.Description, &out.ReportedTime, &precision, &out.SortDate, &out.Location, &locationRecord, &author, &updatedBy, &out.CreatedAt, &out.UpdatedAt, &observationRaw, &participantRaw); err != nil {
		return domain.Event{}, err
	}
	parsed, err := domain.ParseTimePrecision(precision)
	if err != nil {
		return domain.Event{}, err
	}
	var observationIDs []string
	if err := json.Unmarshal(observationRaw, &observationIDs); err != nil {
		return domain.Event{}, err
	}
	out.ID, out.WorkspaceID, out.Author, out.UpdatedBy = id.ID(eventID.Bytes), id.ID(workspace.Bytes), id.ID(author.Bytes), id.ID(updatedBy.Bytes)
	out.TimePrecision = parsed
	out.ObservationIDs = make([]id.ID, 0, len(observationIDs))
	for _, rawID := range observationIDs {
		one, err := id.Parse(rawID)
		if err != nil {
			return domain.Event{}, err
		}
		out.ObservationIDs = append(out.ObservationIDs, one)
	}
	var storedParticipants []storedParticipantLink
	if err := json.Unmarshal(participantRaw, &storedParticipants); err != nil {
		return domain.Event{}, err
	}
	out.ParticipantLinks = make([]domain.ParticipantLink, 0, len(storedParticipants))
	for _, stored := range storedParticipants {
		one, err := id.Parse(stored.RecordID)
		if err != nil {
			return domain.Event{}, err
		}
		out.ParticipantLinks = append(out.ParticipantLinks, domain.ParticipantLink{RecordID: one, Role: domain.ParticipantRole(stored.Role)})
	}
	if len(out.ParticipantLinks) == 0 {
		out.ParticipantLinks = []domain.ParticipantLink{}
	}
	out.ParticipantRecordIDs = participantIDs(out.ParticipantLinks)
	if locationRecord.Valid {
		value := id.ID(locationRecord.Bytes)
		out.LocationRecordID = &value
	}
	return out, nil
}

type storedRecordSnapshot struct {
	ID             string               `json:"record_id"`
	Kind           string               `json:"kind"`
	Name           string               `json:"name"`
	Description    string               `json:"description,omitempty"`
	ObservationIDs []string             `json:"observation_ids"`
	PlaceGeometry  *storedPlaceGeometry `json:"place_geometry,omitempty"`
	Role           string               `json:"role,omitempty"`
}

type storedPlaceGeometry struct {
	Latitude       float64  `json:"latitude"`
	Longitude      float64  `json:"longitude"`
	Precision      string   `json:"precision"`
	ObservationIDs []string `json:"observation_ids"`
}

type storedParticipantLink struct {
	RecordID string `json:"record_id"`
	Role     string `json:"role"`
}

type storedRevision struct {
	ObservationIDs       []string                `json:"observation_ids"`
	ParticipantRecordIDs []string                `json:"participant_record_ids"`
	ParticipantLinks     []storedParticipantLink `json:"participant_links"`
	ParticipantRecords   []storedRecordSnapshot  `json:"participant_records"`
	LocationRecord       *storedRecordSnapshot   `json:"location_record"`
}

func scanRevision(row interface{ Scan(...any) error }) (domain.Revision, error) {
	var out domain.Revision
	var revisionID, workspace, eventID, locationRecord, changedBy pgtype.UUID
	var precision string
	var observationRaw, participantIDRaw, participantLinkRaw, participantRaw, locationRaw []byte
	if err := row.Scan(&revisionID, &workspace, &eventID, &out.Revision, &out.Title, &out.Description, &out.ReportedTime, &precision, &out.SortDate, &out.Location, &observationRaw, &participantIDRaw, &participantLinkRaw, &participantRaw, &locationRecord, &locationRaw, &changedBy, &out.ChangedAt); err != nil {
		return domain.Revision{}, err
	}
	parsed, err := domain.ParseTimePrecision(precision)
	if err != nil {
		return domain.Revision{}, err
	}
	var observationIDs, participantIDs []string
	if err := json.Unmarshal(observationRaw, &observationIDs); err != nil {
		return domain.Revision{}, err
	}
	if err := json.Unmarshal(participantIDRaw, &participantIDs); err != nil {
		return domain.Revision{}, err
	}
	var stored storedRevision
	if err := json.Unmarshal(participantLinkRaw, &stored.ParticipantLinks); err != nil {
		return domain.Revision{}, err
	}
	if len(stored.ParticipantLinks) == 0 {
		for _, participantID := range participantIDs {
			stored.ParticipantLinks = append(stored.ParticipantLinks, storedParticipantLink{RecordID: participantID, Role: string(domain.ParticipantAssociated)})
		}
	}
	if err := json.Unmarshal(participantRaw, &stored.ParticipantRecords); err != nil {
		return domain.Revision{}, err
	}
	if len(locationRaw) > 0 && string(locationRaw) != "null" {
		if err := json.Unmarshal(locationRaw, &stored.LocationRecord); err != nil {
			return domain.Revision{}, err
		}
	}
	out.ID, out.WorkspaceID, out.EventID = id.ID(revisionID.Bytes), id.ID(workspace.Bytes), id.ID(eventID.Bytes)
	out.TimePrecision = parsed
	out.ObservationIDs, err = parseIDs(observationIDs)
	if err != nil {
		return domain.Revision{}, err
	}
	out.ParticipantRecordIDs, err = parseIDs(participantIDs)
	if err != nil {
		return domain.Revision{}, err
	}
	out.ParticipantLinks = make([]domain.ParticipantLink, 0, len(stored.ParticipantLinks))
	for _, participant := range stored.ParticipantLinks {
		recordID, err := id.Parse(participant.RecordID)
		if err != nil {
			return domain.Revision{}, err
		}
		out.ParticipantLinks = append(out.ParticipantLinks, domain.ParticipantLink{RecordID: recordID, Role: domain.ParticipantRole(participant.Role)})
	}
	out.ParticipantRecords, err = parseRecordSnapshots(stored.ParticipantRecords)
	if err != nil {
		return domain.Revision{}, err
	}
	if stored.LocationRecord != nil {
		parsedLocation, err := parseRecordSnapshot(*stored.LocationRecord)
		if err != nil {
			return domain.Revision{}, err
		}
		out.LocationRecord = &parsedLocation
	}
	if locationRecord.Valid {
		value := id.ID(locationRecord.Bytes)
		out.LocationRecordID = &value
	}
	out.ChangedBy = id.ID(changedBy.Bytes)
	return out, nil
}

func parseRecordSnapshots(input []storedRecordSnapshot) ([]domain.RecordSnapshot, error) {
	out := make([]domain.RecordSnapshot, 0, len(input))
	for _, one := range input {
		parsed, err := parseRecordSnapshot(one)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	return out, nil
}

func parseRecordSnapshot(input storedRecordSnapshot) (domain.RecordSnapshot, error) {
	recordID, err := id.Parse(input.ID)
	if err != nil {
		return domain.RecordSnapshot{}, err
	}
	observations, err := parseIDs(input.ObservationIDs)
	if err != nil {
		return domain.RecordSnapshot{}, err
	}
	var geometry *domain.PlaceGeometrySnapshot
	if input.PlaceGeometry != nil {
		geometry = &domain.PlaceGeometrySnapshot{Latitude: input.PlaceGeometry.Latitude, Longitude: input.PlaceGeometry.Longitude, Precision: input.PlaceGeometry.Precision}
		geometry.ObservationIDs, err = parseIDs(input.PlaceGeometry.ObservationIDs)
		if err != nil {
			return domain.RecordSnapshot{}, err
		}
	}
	return domain.RecordSnapshot{ID: recordID, Kind: input.Kind, Name: input.Name, Description: input.Description, ObservationIDs: observations, PlaceGeometry: geometry, Role: domain.ParticipantRole(input.Role)}, nil
}

func parseIDs(raw []string) ([]id.ID, error) {
	out := make([]id.ID, 0, len(raw))
	for _, one := range raw {
		parsed, err := id.Parse(one)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	return out, nil
}

func (s *Store) Create(ctx context.Context, in domain.Event) error {
	_, err := s.db.DB(ctx).Exec(ctx, `insert into timeline.event(id,workspace_id,title,description,reported_time,time_precision,sort_date,location,location_record_id,author,updated_by,created_at,updated_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, uuid(in.ID), uuid(in.WorkspaceID), in.Title, in.Description, in.ReportedTime, in.TimePrecision.String(), in.SortDate, in.Location, optionalUUID(in.LocationRecordID), uuid(in.Author), uuid(in.UpdatedBy), in.CreatedAt, in.UpdatedAt)
	return translate(ctx, err)
}

func (s *Store) ByID(ctx context.Context, workspace, want id.ID) (domain.Event, error) {
	return scanEvent(s.db.DB(ctx).QueryRow(ctx, eventSelect+` where e.workspace_id=$1 and e.id=$2 group by e.id,e.workspace_id,e.title,e.description,e.reported_time,e.time_precision,e.sort_date,e.location,e.location_record_id,e.author,e.updated_by,e.created_at,e.updated_at`, uuid(workspace), uuid(want)))
}

func (s *Store) Save(ctx context.Context, in domain.Event) error {
	tag, err := s.db.DB(ctx).Exec(ctx, `update timeline.event set title=$3,description=$4,reported_time=$5,time_precision=$6,sort_date=$7,location=$8,location_record_id=$9,updated_by=$10,updated_at=$11 where workspace_id=$1 and id=$2`, uuid(in.WorkspaceID), uuid(in.ID), in.Title, in.Description, in.ReportedTime, in.TimePrecision.String(), in.SortDate, in.Location, optionalUUID(in.LocationRecordID), uuid(in.UpdatedBy), in.UpdatedAt)
	if err != nil {
		return translate(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) ReplaceObservations(ctx context.Context, workspace, event id.ID, observations []id.ID) error {
	if _, err := s.db.DB(ctx).Exec(ctx, `delete from timeline.event_observation where workspace_id=$1 and event_id=$2`, uuid(workspace), uuid(event)); err != nil {
		return translate(ctx, err)
	}
	for _, observation := range observations {
		tag, err := s.db.DB(ctx).Exec(ctx, `insert into timeline.event_observation(workspace_id,event_id,observation_id) select $1,$2,$3 where exists (select 1 from timeline.event where id=$2 and workspace_id=$1) and exists (select 1 from observation.manual where id=$3 and workspace_id=$1)`, uuid(workspace), uuid(event), uuid(observation))
		if err != nil {
			return translate(ctx, err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (s *Store) ReplaceParticipants(ctx context.Context, workspace, event id.ID, records []id.ID) error {
	links := make([]domain.ParticipantLink, 0, len(records))
	for _, record := range records {
		links = append(links, domain.ParticipantLink{RecordID: record, Role: domain.ParticipantAssociated})
	}
	return s.ReplaceParticipantLinks(ctx, workspace, event, links)
}

func (s *Store) ReplaceParticipantLinks(ctx context.Context, workspace, event id.ID, records []domain.ParticipantLink) error {
	if _, err := s.db.DB(ctx).Exec(ctx, `delete from timeline.event_participant where workspace_id=$1 and event_id=$2`, uuid(workspace), uuid(event)); err != nil {
		return translate(ctx, err)
	}
	for _, record := range records {
		tag, err := s.db.DB(ctx).Exec(ctx, `insert into timeline.event_participant(workspace_id,event_id,record_id,role) select $1,$2,$3,$4 where exists (select 1 from timeline.event where id=$2 and workspace_id=$1) and exists (select 1 from research.record where id=$3 and workspace_id=$1)`, uuid(workspace), uuid(event), uuid(record.RecordID), record.Role.String())
		if err != nil {
			return translate(ctx, err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (s *Store) CreateRevision(ctx context.Context, in domain.Revision) error {
	observations, err := json.Marshal(stringIDs(in.ObservationIDs))
	if err != nil {
		return err
	}
	participantIDs, err := json.Marshal(stringIDs(in.ParticipantRecordIDs))
	if err != nil {
		return err
	}
	participantLinks, err := json.Marshal(participantLinks(in.ParticipantLinks))
	if err != nil {
		return err
	}
	participantRecords, err := json.Marshal(recordSnapshots(in.ParticipantRecords))
	if err != nil {
		return err
	}
	var locationRecord any
	if in.LocationRecord != nil {
		locationRecord, err = json.Marshal(recordSnapshot(*in.LocationRecord))
		if err != nil {
			return err
		}
	}
	_, err = s.db.DB(ctx).Exec(ctx, `
		insert into timeline.event_revision(id,workspace_id,event_id,revision,title,description,reported_time,time_precision,sort_date,location,observation_ids,participant_record_ids,participant_links,participant_records,location_record_id,location_record,changed_by,changed_at)
		values($1,$2,$3,coalesce((select max(revision)+1 from timeline.event_revision where event_id=$3),1),$4,$5,$6,$7,$8,$9,$10::jsonb,$11::jsonb,$12::jsonb,$13::jsonb,$14,$15::jsonb,$16,$17)`,
		uuid(in.ID), uuid(in.WorkspaceID), uuid(in.EventID), in.Title, in.Description, in.ReportedTime, in.TimePrecision.String(), in.SortDate, in.Location,
		string(observations), string(participantIDs), string(participantLinks), string(participantRecords), optionalUUID(in.LocationRecordID), locationRecord, uuid(in.ChangedBy), in.ChangedAt)
	return translate(ctx, err)
}

func recordSnapshots(input []domain.RecordSnapshot) []storedRecordSnapshot {
	out := make([]storedRecordSnapshot, 0, len(input))
	for _, one := range input {
		out = append(out, recordSnapshot(one))
	}
	return out
}

func recordSnapshot(input domain.RecordSnapshot) storedRecordSnapshot {
	var geometry *storedPlaceGeometry
	if input.PlaceGeometry != nil {
		geometry = &storedPlaceGeometry{Latitude: input.PlaceGeometry.Latitude, Longitude: input.PlaceGeometry.Longitude, Precision: input.PlaceGeometry.Precision, ObservationIDs: stringIDs(input.PlaceGeometry.ObservationIDs)}
	}
	return storedRecordSnapshot{ID: input.ID.String(), Kind: input.Kind, Name: input.Name, Description: input.Description, ObservationIDs: stringIDs(input.ObservationIDs), PlaceGeometry: geometry, Role: input.Role.String()}
}

func participantLinks(input []domain.ParticipantLink) []storedParticipantLink {
	out := make([]storedParticipantLink, 0, len(input))
	for _, one := range input {
		out = append(out, storedParticipantLink{RecordID: one.RecordID.String(), Role: one.Role.String()})
	}
	return out
}

func participantIDs(input []domain.ParticipantLink) []id.ID {
	out := make([]id.ID, 0, len(input))
	for _, one := range input {
		out = append(out, one.RecordID)
	}
	return out
}

func stringIDs(values []id.ID) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.String())
	}
	return out
}

func (s *Store) Revisions(ctx context.Context, workspace, event id.ID) ([]domain.Revision, error) {
	rows, err := s.db.DB(ctx).Query(ctx, eventRevisionSelect+` where workspace_id=$1 and event_id=$2 order by revision asc`, uuid(workspace), uuid(event))
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Revision, 0)
	for rows.Next() {
		one, err := scanRevision(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}

func (s *Store) RevisionByID(ctx context.Context, workspace, event, revision id.ID) (domain.Revision, error) {
	row := s.db.DB(ctx).QueryRow(ctx, eventRevisionSelect+` where workspace_id=$1 and event_id=$2 and id=$3`, uuid(workspace), uuid(event), uuid(revision))
	out, err := scanRevision(row)
	if err != nil {
		return domain.Revision{}, translate(ctx, err)
	}
	return out, nil
}

const eventRevisionSelect = `select id,workspace_id,event_id,revision,title,description,reported_time,time_precision,sort_date,location,observation_ids::text,participant_record_ids::text,participant_links::text,participant_records::text,location_record_id,coalesce(location_record,'null'::jsonb)::text,changed_by,changed_at from timeline.event_revision`

func (s *Store) Page(ctx context.Context, workspace, before id.ID, limit int) ([]domain.Event, error) {
	rows, err := s.db.DB(ctx).Query(ctx, eventSelect+` where e.workspace_id=$1 and ($2::uuid is null or e.id < $2) group by e.id,e.workspace_id,e.title,e.description,e.reported_time,e.time_precision,e.sort_date,e.location,e.location_record_id,e.author,e.updated_by,e.created_at,e.updated_at order by e.id desc limit $3`, uuid(workspace), uuid(before), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Event, 0)
	for rows.Next() {
		one, err := scanEvent(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}

const accountSelect = `
select a.id,a.workspace_id,a.event_id,a.title,a.description,a.reported_time,a.time_precision,a.sort_date,a.location,a.author,a.created_at,a.updated_at,
       coalesce((select json_agg(ao.observation_id order by ao.observation_id) from timeline.event_account_observation ao where ao.account_id=a.id and ao.workspace_id=a.workspace_id),'[]'::json)::text,
       coalesce((select json_agg(ap.record_id order by ap.record_id) from timeline.event_account_participant ap where ap.account_id=a.id and ap.workspace_id=a.workspace_id),'[]'::json)::text,
       (select eal.record_id from timeline.event_account_location eal where eal.account_id=a.id and eal.workspace_id=a.workspace_id)
from timeline.event_account a`

func scanAccount(row interface{ Scan(...any) error }) (domain.Account, error) {
	var out domain.Account
	var accountID, workspace, eventID, author, locationRecord pgtype.UUID
	var precision string
	var observationRaw, participantRaw []byte
	if err := row.Scan(&accountID, &workspace, &eventID, &out.Title, &out.Description, &out.ReportedTime, &precision, &out.SortDate, &out.Location, &author, &out.CreatedAt, &out.UpdatedAt, &observationRaw, &participantRaw, &locationRecord); err != nil {
		return domain.Account{}, err
	}
	parsed, err := domain.ParseTimePrecision(precision)
	if err != nil {
		return domain.Account{}, err
	}
	var observationIDs, participantIDs []string
	if err := json.Unmarshal(observationRaw, &observationIDs); err != nil {
		return domain.Account{}, err
	}
	if err := json.Unmarshal(participantRaw, &participantIDs); err != nil {
		return domain.Account{}, err
	}
	out.ID, out.WorkspaceID, out.EventID, out.Author = id.ID(accountID.Bytes), id.ID(workspace.Bytes), id.ID(eventID.Bytes), id.ID(author.Bytes)
	out.TimePrecision = parsed
	out.ObservationIDs, err = parseIDs(observationIDs)
	if err != nil {
		return domain.Account{}, err
	}
	out.ParticipantRecordIDs, err = parseIDs(participantIDs)
	if err != nil {
		return domain.Account{}, err
	}
	if locationRecord.Valid {
		value := id.ID(locationRecord.Bytes)
		out.LocationRecordID = &value
	}
	return out, nil
}

func (s *Store) CreateAccount(ctx context.Context, in domain.Account) error {
	_, err := s.db.DB(ctx).Exec(ctx, `insert into timeline.event_account(id,workspace_id,event_id,title,description,reported_time,time_precision,sort_date,location,author,created_at,updated_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, uuid(in.ID), uuid(in.WorkspaceID), uuid(in.EventID), in.Title, in.Description, in.ReportedTime, in.TimePrecision.String(), in.SortDate, in.Location, uuid(in.Author), in.CreatedAt, in.UpdatedAt)
	if err != nil {
		return translate(ctx, err)
	}
	for _, observation := range in.ObservationIDs {
		tag, err := s.db.DB(ctx).Exec(ctx, `insert into timeline.event_account_observation(workspace_id,account_id,observation_id) select $1,$2,$3 where exists (select 1 from timeline.event_account where id=$2 and workspace_id=$1) and exists (select 1 from observation.manual where id=$3 and workspace_id=$1)`, uuid(in.WorkspaceID), uuid(in.ID), uuid(observation))
		if err != nil {
			return translate(ctx, err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
	}
	for _, record := range in.ParticipantRecordIDs {
		tag, err := s.db.DB(ctx).Exec(ctx, `insert into timeline.event_account_participant(workspace_id,account_id,record_id) select $1,$2,$3 where exists (select 1 from timeline.event_account where id=$2 and workspace_id=$1) and exists (select 1 from research.record where id=$3 and workspace_id=$1)`, uuid(in.WorkspaceID), uuid(in.ID), uuid(record))
		if err != nil {
			return translate(ctx, err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
	}
	if in.LocationRecordID != nil {
		tag, err := s.db.DB(ctx).Exec(ctx, `insert into timeline.event_account_location(workspace_id,account_id,record_id) select $1,$2,$3 where exists (select 1 from timeline.event_account where id=$2 and workspace_id=$1) and exists (select 1 from research.record where id=$3 and workspace_id=$1)`, uuid(in.WorkspaceID), uuid(in.ID), uuid(*in.LocationRecordID))
		if err != nil {
			return translate(ctx, err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (s *Store) AccountByID(ctx context.Context, workspace, event, account id.ID) (domain.Account, error) {
	row := s.db.DB(ctx).QueryRow(ctx, accountSelect+` where a.workspace_id=$1 and a.event_id=$2 and a.id=$3`, uuid(workspace), uuid(event), uuid(account))
	out, err := scanAccount(row)
	if err != nil {
		return domain.Account{}, translate(ctx, err)
	}
	return out, nil
}

func (s *Store) Accounts(ctx context.Context, workspace, event id.ID) ([]domain.Account, error) {
	rows, err := s.db.DB(ctx).Query(ctx, accountSelect+` where a.workspace_id=$1 and a.event_id=$2 order by a.created_at asc,a.id asc`, uuid(workspace), uuid(event))
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Account, 0)
	for rows.Next() {
		one, err := scanAccount(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}

func (s *Store) CreateReconciliation(ctx context.Context, in domain.Reconciliation) error {
	_, err := s.db.DB(ctx).Exec(ctx, `insert into timeline.event_reconciliation(id,workspace_id,event_id,decision,selected_account_id,rationale,reviewed_by,reviewed_at) values($1,$2,$3,$4,$5,$6,$7,$8) on conflict (workspace_id,event_id) do update set id=excluded.id,decision=excluded.decision,selected_account_id=excluded.selected_account_id,rationale=excluded.rationale,reviewed_by=excluded.reviewed_by,reviewed_at=excluded.reviewed_at`, uuid(in.ID), uuid(in.WorkspaceID), uuid(in.EventID), string(in.Decision), optionalUUID(in.SelectedAccountID), in.Rationale, uuid(in.ReviewedBy), in.ReviewedAt)
	return translate(ctx, err)
}

func (s *Store) Reconciliation(ctx context.Context, workspace, event id.ID) (domain.Reconciliation, error) {
	var out domain.Reconciliation
	var reconciliationID, workspaceID, eventID, selected, reviewer pgtype.UUID
	var decision string
	if err := s.db.DB(ctx).QueryRow(ctx, `select id,workspace_id,event_id,decision,selected_account_id,rationale,reviewed_by,reviewed_at from timeline.event_reconciliation where workspace_id=$1 and event_id=$2`, uuid(workspace), uuid(event)).Scan(&reconciliationID, &workspaceID, &eventID, &decision, &selected, &out.Rationale, &reviewer, &out.ReviewedAt); err != nil {
		return domain.Reconciliation{}, translate(ctx, err)
	}
	out.ID, out.WorkspaceID, out.EventID, out.ReviewedBy = id.ID(reconciliationID.Bytes), id.ID(workspaceID.Bytes), id.ID(eventID.Bytes), id.ID(reviewer.Bytes)
	out.Decision = domain.ReconciliationDecision(decision)
	if selected.Valid {
		value := id.ID(selected.Bytes)
		out.SelectedAccountID = &value
	}
	return out, nil
}

const clusterSelect = `
select c.id,c.workspace_id,c.title,c.description,c.state,c.review_note,c.author,c.updated_by,c.reviewed_by,c.reviewed_at,c.created_at,c.updated_at,
       coalesce((select json_agg(ce.event_id order by ce.ordinal) from timeline.event_cluster_event ce where ce.cluster_id=c.id and ce.workspace_id=c.workspace_id),'[]'::json)::text
from timeline.event_cluster c`

func scanCluster(row interface{ Scan(...any) error }) (domain.Cluster, error) {
	var out domain.Cluster
	var clusterID, workspace, author, updatedBy, reviewedBy pgtype.UUID
	var state string
	var eventRaw []byte
	if err := row.Scan(&clusterID, &workspace, &out.Title, &out.Description, &state, &out.ReviewNote, &author, &updatedBy, &reviewedBy, &out.ReviewedAt, &out.CreatedAt, &out.UpdatedAt, &eventRaw); err != nil {
		return domain.Cluster{}, err
	}
	var eventIDs []string
	if err := json.Unmarshal(eventRaw, &eventIDs); err != nil {
		return domain.Cluster{}, err
	}
	parsed, err := parseIDs(eventIDs)
	if err != nil {
		return domain.Cluster{}, err
	}
	out.ID, out.WorkspaceID, out.Author, out.UpdatedBy = id.ID(clusterID.Bytes), id.ID(workspace.Bytes), id.ID(author.Bytes), id.ID(updatedBy.Bytes)
	out.State, out.EventIDs = domain.ClusterState(state), parsed
	if reviewedBy.Valid {
		value := id.ID(reviewedBy.Bytes)
		out.ReviewedBy = &value
	}
	return out, nil
}

func (s *Store) CreateCluster(ctx context.Context, in domain.Cluster) error {
	_, err := s.db.DB(ctx).Exec(ctx, `insert into timeline.event_cluster(id,workspace_id,title,description,state,review_note,author,updated_by,reviewed_by,reviewed_at,created_at,updated_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, uuid(in.ID), uuid(in.WorkspaceID), in.Title, in.Description, string(in.State), in.ReviewNote, uuid(in.Author), uuid(in.UpdatedBy), optionalUUID(in.ReviewedBy), in.ReviewedAt, in.CreatedAt, in.UpdatedAt)
	if err != nil {
		return translate(ctx, err)
	}
	return s.replaceClusterEvents(ctx, in)
}

func (s *Store) ClusterByID(ctx context.Context, workspace, cluster id.ID) (domain.Cluster, error) {
	row := s.db.DB(ctx).QueryRow(ctx, clusterSelect+` where c.workspace_id=$1 and c.id=$2`, uuid(workspace), uuid(cluster))
	out, err := scanCluster(row)
	if err != nil {
		return domain.Cluster{}, translate(ctx, err)
	}
	return out, nil
}

func (s *Store) SaveCluster(ctx context.Context, in domain.Cluster) error {
	tag, err := s.db.DB(ctx).Exec(ctx, `update timeline.event_cluster set title=$3,description=$4,state=$5,review_note=$6,updated_by=$7,reviewed_by=$8,reviewed_at=$9,updated_at=$10 where workspace_id=$1 and id=$2`, uuid(in.WorkspaceID), uuid(in.ID), in.Title, in.Description, string(in.State), in.ReviewNote, uuid(in.UpdatedBy), optionalUUID(in.ReviewedBy), in.ReviewedAt, in.UpdatedAt)
	if err != nil {
		return translate(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return s.replaceClusterEvents(ctx, in)
}

func (s *Store) replaceClusterEvents(ctx context.Context, in domain.Cluster) error {
	if _, err := s.db.DB(ctx).Exec(ctx, `delete from timeline.event_cluster_event where workspace_id=$1 and cluster_id=$2`, uuid(in.WorkspaceID), uuid(in.ID)); err != nil {
		return translate(ctx, err)
	}
	for ordinal, eventID := range in.EventIDs {
		tag, err := s.db.DB(ctx).Exec(ctx, `insert into timeline.event_cluster_event(workspace_id,cluster_id,event_id,ordinal) select $1,$2,$3,$4 where exists (select 1 from timeline.event_cluster where id=$2 and workspace_id=$1) and exists (select 1 from timeline.event where id=$3 and workspace_id=$1)`, uuid(in.WorkspaceID), uuid(in.ID), uuid(eventID), ordinal)
		if err != nil {
			return translate(ctx, err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (s *Store) PageClusters(ctx context.Context, workspace, before id.ID, limit int) ([]domain.Cluster, error) {
	rows, err := s.db.DB(ctx).Query(ctx, clusterSelect+` where c.workspace_id=$1 and ($2::uuid is null or c.id < $2) order by c.id desc limit $3`, uuid(workspace), uuid(before), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Cluster, 0)
	for rows.Next() {
		one, err := scanCluster(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}

const relationshipSelect = `
select r.id,r.workspace_id,r.from_event_id,r.to_event_id,r.kind,r.rationale,r.state,r.review_note,
       coalesce((select json_agg(s.supporting_observation_id order by s.supporting_observation_id)
         from timeline.event_relationship_supporting s
         where s.relationship_id=r.id and s.workspace_id=r.workspace_id), '[]'::json)::text,
       coalesce((select json_agg(o.opposing_observation_id order by o.opposing_observation_id)
         from timeline.event_relationship_opposing o
         where o.relationship_id=r.id and o.workspace_id=r.workspace_id), '[]'::json)::text,
       r.author,r.updated_by,r.reviewed_by,r.reviewed_at,r.created_at,r.updated_at
from timeline.event_relationship r`

func scanRelationship(row interface{ Scan(...any) error }) (domain.Relationship, error) {
	var out domain.Relationship
	var relationshipID, workspace, fromEvent, toEvent, author, updatedBy, reviewedBy pgtype.UUID
	var kind, state string
	var supportingRaw, opposingRaw []byte
	if err := row.Scan(&relationshipID, &workspace, &fromEvent, &toEvent, &kind, &out.Rationale, &state, &out.ReviewNote, &supportingRaw, &opposingRaw, &author, &updatedBy, &reviewedBy, &out.ReviewedAt, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return domain.Relationship{}, err
	}
	var supporting, opposing []string
	if err := json.Unmarshal(supportingRaw, &supporting); err != nil {
		return domain.Relationship{}, err
	}
	if err := json.Unmarshal(opposingRaw, &opposing); err != nil {
		return domain.Relationship{}, err
	}
	parsedSupporting, err := parseIDs(supporting)
	if err != nil {
		return domain.Relationship{}, err
	}
	parsedOpposing, err := parseIDs(opposing)
	if err != nil {
		return domain.Relationship{}, err
	}
	out.ID, out.WorkspaceID, out.FromEventID, out.ToEventID = id.ID(relationshipID.Bytes), id.ID(workspace.Bytes), id.ID(fromEvent.Bytes), id.ID(toEvent.Bytes)
	out.Kind, out.State = domain.RelationshipKind(kind), domain.RelationshipState(state)
	out.SupportingObservationIDs, out.OpposingObservationIDs = parsedSupporting, parsedOpposing
	out.Author, out.UpdatedBy = id.ID(author.Bytes), id.ID(updatedBy.Bytes)
	if reviewedBy.Valid {
		value := id.ID(reviewedBy.Bytes)
		out.ReviewedBy = &value
	}
	return out, nil
}

func (s *Store) CreateRelationship(ctx context.Context, in domain.Relationship) error {
	if _, err := s.db.DB(ctx).Exec(ctx, `insert into timeline.event_relationship(id,workspace_id,from_event_id,to_event_id,kind,rationale,state,review_note,author,updated_by,reviewed_by,reviewed_at,created_at,updated_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, uuid(in.ID), uuid(in.WorkspaceID), uuid(in.FromEventID), uuid(in.ToEventID), string(in.Kind), in.Rationale, string(in.State), in.ReviewNote, uuid(in.Author), uuid(in.UpdatedBy), optionalUUID(in.ReviewedBy), in.ReviewedAt, in.CreatedAt, in.UpdatedAt); err != nil {
		return translate(ctx, err)
	}
	return s.replaceRelationshipEvidence(ctx, in)
}

func (s *Store) replaceRelationshipEvidence(ctx context.Context, in domain.Relationship) error {
	db := s.db.DB(ctx)
	if _, err := db.Exec(ctx, `delete from timeline.event_relationship_supporting where relationship_id=$1 and workspace_id=$2`, uuid(in.ID), uuid(in.WorkspaceID)); err != nil {
		return translate(ctx, err)
	}
	for _, observation := range in.SupportingObservationIDs {
		if _, err := db.Exec(ctx, `insert into timeline.event_relationship_supporting(workspace_id,relationship_id,supporting_observation_id) values($1,$2,$3)`, uuid(in.WorkspaceID), uuid(in.ID), uuid(observation)); err != nil {
			return translate(ctx, err)
		}
	}
	if _, err := db.Exec(ctx, `delete from timeline.event_relationship_opposing where relationship_id=$1 and workspace_id=$2`, uuid(in.ID), uuid(in.WorkspaceID)); err != nil {
		return translate(ctx, err)
	}
	for _, observation := range in.OpposingObservationIDs {
		if _, err := db.Exec(ctx, `insert into timeline.event_relationship_opposing(workspace_id,relationship_id,opposing_observation_id) values($1,$2,$3)`, uuid(in.WorkspaceID), uuid(in.ID), uuid(observation)); err != nil {
			return translate(ctx, err)
		}
	}
	return nil
}

func (s *Store) RelationshipByID(ctx context.Context, workspace, relationship id.ID) (domain.Relationship, error) {
	out, err := scanRelationship(s.db.DB(ctx).QueryRow(ctx, relationshipSelect+` where r.workspace_id=$1 and r.id=$2`, uuid(workspace), uuid(relationship)))
	if err != nil {
		return domain.Relationship{}, translate(ctx, err)
	}
	return out, nil
}

func (s *Store) SaveRelationship(ctx context.Context, in domain.Relationship) error {
	tag, err := s.db.DB(ctx).Exec(ctx, `update timeline.event_relationship set state=$3,review_note=$4,updated_by=$5,reviewed_by=$6,reviewed_at=$7,updated_at=$8 where workspace_id=$1 and id=$2`, uuid(in.WorkspaceID), uuid(in.ID), string(in.State), in.ReviewNote, uuid(in.UpdatedBy), optionalUUID(in.ReviewedBy), in.ReviewedAt, in.UpdatedAt)
	if err != nil {
		return translate(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) PageRelationships(ctx context.Context, workspace, before id.ID, limit int) ([]domain.Relationship, error) {
	rows, err := s.db.DB(ctx).Query(ctx, relationshipSelect+` where r.workspace_id=$1 and ($2::uuid is null or r.id < $2) order by r.id desc limit $3`, uuid(workspace), uuid(before), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Relationship, 0)
	for rows.Next() {
		one, err := scanRelationship(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}
