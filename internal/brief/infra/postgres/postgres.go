package postgres

import (
	"context"
	"embed"
	"encoding/json"
	stderrors "errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/brief/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const Schema = "brief"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("brief: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("brief: NewStore with a nil pool")
	}
	return &Store{db: db}
}
func uuid(i id.ID) pgtype.UUID { return pgtype.UUID{Bytes: i, Valid: !i.IsZero()} }

func optionalUUID(i *id.ID) pgtype.UUID {
	if i == nil || i.IsZero() {
		return pgtype.UUID{}
	}
	return uuid(*i)
}

func translate(ctx context.Context, err error) error {
	if stderrors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return postgres.Translate(ctx, err, "brief")
}

const briefSelect = `
select b.id,b.workspace_id,b.title,b.question,b.current_account,b.alternatives,b.limitations,b.next_steps,
       b.author,b.updated_by,b.created_at,b.updated_at,
       coalesce((select json_agg(bo.observation_id order by bo.observation_id) from brief.working_observation bo where bo.workspace_id=b.workspace_id and bo.brief_id=b.id), '[]'::json)::text,
       coalesce((select json_agg(bc.cluster_id order by bc.cluster_id) from brief.working_cluster bc where bc.workspace_id=b.workspace_id and bc.brief_id=b.id), '[]'::json)::text,
       coalesce((select json_agg(bq.question_id order by bq.question_id) from brief.working_question bq where bq.workspace_id=b.workspace_id and bq.brief_id=b.id), '[]'::json)::text,
       coalesce((select json_agg(bc.connection_id order by bc.connection_id) from brief.working_connection bc where bc.workspace_id=b.workspace_id and bc.brief_id=b.id), '[]'::json)::text,
       coalesce((select json_agg(be.event_id order by be.event_id) from brief.working_event be where be.workspace_id=b.workspace_id and be.brief_id=b.id), '[]'::json)::text
from brief.working b`

const snapshotSelect = `
select s.id,s.workspace_id,s.brief_id,s.title,s.question,s.current_account,s.alternatives,s.limitations,s.next_steps,
       s.author,s.updated_by,s.frozen_by,s.source_updated_at,s.frozen_at,
       coalesce((select json_agg(so.observation_id order by so.observation_id) from brief.snapshot_observation so where so.workspace_id=s.workspace_id and so.snapshot_id=s.id), '[]'::json)::text,
       coalesce((select json_agg(json_build_object('cluster_id',sc.cluster_id,'kind',sc.kind,'title',sc.title,'description',sc.description,'observation_ids',sc.observation_ids) order by sc.cluster_id) from brief.snapshot_cluster sc where sc.workspace_id=s.workspace_id and sc.snapshot_id=s.id), '[]'::json)::text,
       coalesce((select json_agg(json_build_object('question_id',sq.question_id,'question',sq.question,'state',sq.state,'resolution',sq.resolution,'observation_ids',sq.observation_ids) order by sq.question_id) from brief.snapshot_question sq where sq.workspace_id=s.workspace_id and sq.snapshot_id=s.id), '[]'::json)::text,
       coalesce((select json_agg(json_build_object('connection_id',sc.connection_id,'from_record_id',sc.from_record_id,'from_record_kind',sc.from_record_kind,'from_record_name',sc.from_record_name,'from_record_description',sc.from_record_description,'from_record_observation_ids',sc.from_record_observation_ids,'to_record_id',sc.to_record_id,'to_record_kind',sc.to_record_kind,'to_record_name',sc.to_record_name,'to_record_description',sc.to_record_description,'to_record_observation_ids',sc.to_record_observation_ids,'kind',sc.kind,'state',sc.state,'rationale',sc.rationale,'supporting_observation_ids',sc.supporting_observation_ids,'opposing_observation_ids',sc.opposing_observation_ids) order by sc.connection_id) from brief.snapshot_connection sc where sc.workspace_id=s.workspace_id and sc.snapshot_id=s.id), '[]'::json)::text,
       coalesce((select json_agg(json_build_object('event_id',se.event_id,'event_revision_id',se.event_revision_id,'event_revision',se.event_revision,'title',se.title,'description',se.description,'reported_time',se.reported_time,'time_precision',se.time_precision,'sort_date',se.sort_date,'location',se.location,'observation_ids',se.observation_ids,'participant_records',se.participant_records,'location_record',se.location_record) order by se.event_id) from brief.snapshot_event se where se.workspace_id=s.workspace_id and se.snapshot_id=s.id), '[]'::json)::text,
       coalesce((select json_agg(json_build_object('relationship_id',ser.relationship_id,'from_event_id',ser.from_event_id,'to_event_id',ser.to_event_id,'kind',ser.kind,'rationale',ser.rationale,'state',ser.state,'review_note',ser.review_note,'supporting_observation_ids',ser.supporting_observation_ids,'opposing_observation_ids',ser.opposing_observation_ids) order by ser.relationship_id) from brief.snapshot_event_relationship ser where ser.workspace_id=s.workspace_id and ser.snapshot_id=s.id), '[]'::json)::text
from brief.snapshot s`

const workingVisibility = `
  and ($2='restricted' or not exists (
    select 1
    from (
      select bo.observation_id
      from brief.working_observation bo
      where bo.workspace_id=b.workspace_id and bo.brief_id=b.id
      union all
      select qo.observation_id
      from brief.working_question bwq
      join lead.question_observation qo on qo.workspace_id=bwq.workspace_id and qo.question_id=bwq.question_id
      where bwq.workspace_id=b.workspace_id and bwq.brief_id=b.id
      union all
      select co.observation_id
      from brief.working_cluster bwc
      join review.cluster_observation co on co.cluster_id=bwc.cluster_id
      join review.cluster c on c.id=co.cluster_id and c.workspace_id=bwc.workspace_id
      where bwc.workspace_id=b.workspace_id and bwc.brief_id=b.id
      union all
      select ce.observation_id
      from brief.working_connection bwc
      join research.connection_evidence ce on ce.connection_id=bwc.connection_id and ce.workspace_id=bwc.workspace_id
      where bwc.workspace_id=b.workspace_id and bwc.brief_id=b.id
      union all
      select ro.observation_id
      from brief.working_connection bwc
      join research.connection c on c.id=bwc.connection_id and c.workspace_id=bwc.workspace_id
      join research.record_observation ro on ro.workspace_id=c.workspace_id and ro.record_id in (c.from_record_id,c.to_record_id)
      where bwc.workspace_id=b.workspace_id and bwc.brief_id=b.id
      union all
      select eo.observation_id
      from brief.working_event bwe
      join timeline.event_observation eo on eo.workspace_id=bwe.workspace_id and eo.event_id=bwe.event_id
      where bwe.workspace_id=b.workspace_id and bwe.brief_id=b.id
      union all
      select ro.observation_id
      from brief.working_event bwe
      join timeline.event e on e.workspace_id=bwe.workspace_id and e.id=bwe.event_id
      join timeline.event_participant ep on ep.workspace_id=e.workspace_id and ep.event_id=e.id
      join research.record_observation ro on ro.workspace_id=ep.workspace_id and ro.record_id=ep.record_id
      where bwe.workspace_id=b.workspace_id and bwe.brief_id=b.id
      union all
      select ro.observation_id
      from brief.working_event bwe
      join timeline.event e on e.workspace_id=bwe.workspace_id and e.id=bwe.event_id
      join research.record_observation ro on ro.workspace_id=e.workspace_id and ro.record_id=e.location_record_id
      where bwe.workspace_id=b.workspace_id and bwe.brief_id=b.id and e.location_record_id is not null
    ) linked
    join observation.manual m on m.id=linked.observation_id and m.workspace_id=b.workspace_id
    join source.source src on src.id=m.source_id and src.workspace_id=b.workspace_id
    where src.sensitivity='restricted'))`

const snapshotVisibility = `
  and ($3='restricted' or not exists (
    select 1
    from (
      select so.observation_id
      from brief.snapshot_observation so
      where so.workspace_id=s.workspace_id and so.snapshot_id=s.id
      union all
      select cited.observation_id::uuid
      from brief.snapshot_question sq
      cross join lateral jsonb_array_elements_text(sq.observation_ids) as cited(observation_id)
      where sq.workspace_id=s.workspace_id and sq.snapshot_id=s.id
      union all
      select cited.observation_id::uuid
      from brief.snapshot_cluster sc
      cross join lateral jsonb_array_elements_text(sc.observation_ids) as cited(observation_id)
      where sc.workspace_id=s.workspace_id and sc.snapshot_id=s.id
      union all
      select cited.observation_id::uuid
      from brief.snapshot_connection sc
      cross join lateral jsonb_array_elements_text(sc.supporting_observation_ids) as cited(observation_id)
      where sc.workspace_id=s.workspace_id and sc.snapshot_id=s.id
      union all
      select cited.observation_id::uuid
      from brief.snapshot_connection sc
      cross join lateral jsonb_array_elements_text(sc.opposing_observation_ids) as cited(observation_id)
      where sc.workspace_id=s.workspace_id and sc.snapshot_id=s.id
      union all
      select cited.observation_id::uuid
      from brief.snapshot_connection sc
      cross join lateral jsonb_array_elements_text(sc.from_record_observation_ids) as cited(observation_id)
      where sc.workspace_id=s.workspace_id and sc.snapshot_id=s.id
      union all
      select cited.observation_id::uuid
      from brief.snapshot_connection sc
      cross join lateral jsonb_array_elements_text(sc.to_record_observation_ids) as cited(observation_id)
      where sc.workspace_id=s.workspace_id and sc.snapshot_id=s.id
      union all
      select cited.observation_id::uuid
      from brief.snapshot_event se
      cross join lateral jsonb_array_elements_text(se.observation_ids) as cited(observation_id)
      where se.workspace_id=s.workspace_id and se.snapshot_id=s.id
      union all
      select cited.observation_id::uuid
      from brief.snapshot_event se
      cross join lateral jsonb_array_elements(se.participant_records) as record_entry
      cross join lateral jsonb_array_elements_text(record_entry->'observation_ids') as cited(observation_id)
      where se.workspace_id=s.workspace_id and se.snapshot_id=s.id
      union all
      select cited.observation_id::uuid
      from brief.snapshot_event se
      cross join lateral jsonb_array_elements_text(coalesce(se.location_record->'observation_ids','[]'::jsonb)) as cited(observation_id)
      where se.workspace_id=s.workspace_id and se.snapshot_id=s.id
      union all
      select cited.observation_id::uuid
      from brief.snapshot_event_relationship ser
      cross join lateral jsonb_array_elements_text(ser.supporting_observation_ids) as cited(observation_id)
      where ser.workspace_id=s.workspace_id and ser.snapshot_id=s.id
      union all
      select cited.observation_id::uuid
      from brief.snapshot_event_relationship ser
      cross join lateral jsonb_array_elements_text(ser.opposing_observation_ids) as cited(observation_id)
      where ser.workspace_id=s.workspace_id and ser.snapshot_id=s.id
    ) linked
    join observation.manual m on m.id=linked.observation_id and m.workspace_id=s.workspace_id
    join source.source src on src.id=m.source_id and src.workspace_id=s.workspace_id
    where src.sensitivity='restricted'))`

func scanBrief(row interface{ Scan(...any) error }) (domain.Brief, error) {
	var out domain.Brief
	var briefID, workspace, author, updatedBy pgtype.UUID
	var observations, clusters, questions, connections, events []byte
	if err := row.Scan(&briefID, &workspace, &out.Title, &out.Question, &out.CurrentAccount, &out.Alternatives, &out.Limitations, &out.NextSteps, &author, &updatedBy, &out.CreatedAt, &out.UpdatedAt, &observations, &clusters, &questions, &connections, &events); err != nil {
		return domain.Brief{}, err
	}
	var observationIDs, questionIDs []string
	if err := json.Unmarshal(observations, &observationIDs); err != nil {
		return domain.Brief{}, err
	}
	if err := json.Unmarshal(questions, &questionIDs); err != nil {
		return domain.Brief{}, err
	}
	var clusterIDs []string
	if err := json.Unmarshal(clusters, &clusterIDs); err != nil {
		return domain.Brief{}, err
	}
	var connectionIDs []string
	if err := json.Unmarshal(connections, &connectionIDs); err != nil {
		return domain.Brief{}, err
	}
	var eventIDs []string
	if err := json.Unmarshal(events, &eventIDs); err != nil {
		return domain.Brief{}, err
	}
	parsedObservations, err := parseIDs(observationIDs)
	if err != nil {
		return domain.Brief{}, err
	}
	parsedQuestions, err := parseIDs(questionIDs)
	if err != nil {
		return domain.Brief{}, err
	}
	parsedClusters, err := parseIDs(clusterIDs)
	if err != nil {
		return domain.Brief{}, err
	}
	parsedConnections, err := parseIDs(connectionIDs)
	if err != nil {
		return domain.Brief{}, err
	}
	parsedEvents, err := parseIDs(eventIDs)
	if err != nil {
		return domain.Brief{}, err
	}
	out.ID, out.WorkspaceID = id.ID(briefID.Bytes), id.ID(workspace.Bytes)
	out.Author, out.UpdatedBy = id.ID(author.Bytes), id.ID(updatedBy.Bytes)
	out.ObservationIDs, out.ClusterIDs, out.QuestionIDs, out.ConnectionIDs, out.EventIDs = parsedObservations, parsedClusters, parsedQuestions, parsedConnections, parsedEvents
	return out, nil
}

func parseIDs(raw []string) ([]id.ID, error) {
	out := make([]id.ID, 0, len(raw))
	for _, value := range raw {
		one, err := id.Parse(value)
		if err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	return out, nil
}

type rawEventRecordSnapshot struct {
	ID             string                 `json:"record_id"`
	Kind           string                 `json:"kind"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	ObservationIDs []string               `json:"observation_ids"`
	PlaceGeometry  *rawEventPlaceGeometry `json:"place_geometry"`
}

type rawEventPlaceGeometry struct {
	Latitude       float64  `json:"latitude"`
	Longitude      float64  `json:"longitude"`
	Precision      string   `json:"precision"`
	ObservationIDs []string `json:"observation_ids"`
}

type rawEventSnapshot struct {
	ID                 string                   `json:"event_id"`
	RevisionID         *string                  `json:"event_revision_id"`
	Revision           *int                     `json:"event_revision"`
	Title              string                   `json:"title"`
	Description        string                   `json:"description"`
	ReportedTime       string                   `json:"reported_time"`
	TimePrecision      string                   `json:"time_precision"`
	SortDate           string                   `json:"sort_date"`
	Location           string                   `json:"location"`
	ObservationIDs     []string                 `json:"observation_ids"`
	ParticipantRecords []rawEventRecordSnapshot `json:"participant_records"`
	LocationRecord     *rawEventRecordSnapshot  `json:"location_record"`
}

type rawEventRelationshipSnapshot struct {
	ID                       string   `json:"relationship_id"`
	FromEventID              string   `json:"from_event_id"`
	ToEventID                string   `json:"to_event_id"`
	Kind                     string   `json:"kind"`
	Rationale                string   `json:"rationale"`
	State                    string   `json:"state"`
	ReviewNote               string   `json:"review_note"`
	SupportingObservationIDs []string `json:"supporting_observation_ids"`
	OpposingObservationIDs   []string `json:"opposing_observation_ids"`
}

type storedEventRecordSnapshot struct {
	ID             string                    `json:"record_id"`
	Kind           string                    `json:"kind"`
	Name           string                    `json:"name"`
	Description    string                    `json:"description,omitempty"`
	ObservationIDs []string                  `json:"observation_ids"`
	PlaceGeometry  *storedEventPlaceGeometry `json:"place_geometry,omitempty"`
}

type storedEventPlaceGeometry struct {
	Latitude       float64  `json:"latitude"`
	Longitude      float64  `json:"longitude"`
	Precision      string   `json:"precision"`
	ObservationIDs []string `json:"observation_ids"`
}

func storedEventRecord(record domain.EventRecordSnapshot) storedEventRecordSnapshot {
	var geometry *storedEventPlaceGeometry
	if record.PlaceGeometry != nil {
		geometry = &storedEventPlaceGeometry{Latitude: record.PlaceGeometry.Latitude, Longitude: record.PlaceGeometry.Longitude, Precision: record.PlaceGeometry.Precision, ObservationIDs: stringIDs(record.PlaceGeometry.ObservationIDs)}
	}
	return storedEventRecordSnapshot{ID: record.ID.String(), Kind: record.Kind, Name: record.Name, Description: record.Description, ObservationIDs: stringIDs(record.ObservationIDs), PlaceGeometry: geometry}
}

func parseEventRecordSnapshot(raw rawEventRecordSnapshot) (domain.EventRecordSnapshot, error) {
	recordID, err := id.Parse(raw.ID)
	if err != nil {
		return domain.EventRecordSnapshot{}, err
	}
	observations, err := parseIDs(raw.ObservationIDs)
	if err != nil {
		return domain.EventRecordSnapshot{}, err
	}
	var geometry *domain.EventPlaceGeometrySnapshot
	if raw.PlaceGeometry != nil {
		geometryObservations, err := parseIDs(raw.PlaceGeometry.ObservationIDs)
		if err != nil {
			return domain.EventRecordSnapshot{}, err
		}
		geometry = &domain.EventPlaceGeometrySnapshot{Latitude: raw.PlaceGeometry.Latitude, Longitude: raw.PlaceGeometry.Longitude, Precision: raw.PlaceGeometry.Precision, ObservationIDs: geometryObservations}
	}
	return domain.EventRecordSnapshot{ID: recordID, Kind: raw.Kind, Name: raw.Name, Description: raw.Description, ObservationIDs: observations, PlaceGeometry: geometry}, nil
}

func scanSnapshot(row interface{ Scan(...any) error }) (domain.Snapshot, error) {
	var out domain.Snapshot
	var snapshotID, workspace, briefID, author, updatedBy, frozenBy pgtype.UUID
	var observations, clusters, questions, connections, events, eventRelationships []byte
	if err := row.Scan(&snapshotID, &workspace, &briefID, &out.Title, &out.Question, &out.CurrentAccount, &out.Alternatives, &out.Limitations, &out.NextSteps, &author, &updatedBy, &frozenBy, &out.SourceUpdatedAt, &out.FrozenAt, &observations, &clusters, &questions, &connections, &events, &eventRelationships); err != nil {
		return domain.Snapshot{}, err
	}
	var observationIDs []string
	if err := json.Unmarshal(observations, &observationIDs); err != nil {
		return domain.Snapshot{}, err
	}
	parsedObservations, err := parseIDs(observationIDs)
	if err != nil {
		return domain.Snapshot{}, err
	}
	var rawClusters []struct {
		ID             string   `json:"cluster_id"`
		Kind           string   `json:"kind"`
		Title          string   `json:"title"`
		Description    string   `json:"description"`
		ObservationIDs []string `json:"observation_ids"`
	}
	if err := json.Unmarshal(clusters, &rawClusters); err != nil {
		return domain.Snapshot{}, err
	}
	parsedClusters := make([]domain.ClusterSnapshot, 0, len(rawClusters))
	for _, raw := range rawClusters {
		clusterID, err := id.Parse(raw.ID)
		if err != nil {
			return domain.Snapshot{}, err
		}
		observationIDs, err := parseIDs(raw.ObservationIDs)
		if err != nil {
			return domain.Snapshot{}, err
		}
		parsedClusters = append(parsedClusters, domain.ClusterSnapshot{ID: clusterID, Kind: raw.Kind, Title: raw.Title, Description: raw.Description, ObservationIDs: observationIDs})
	}
	var rawQuestions []struct {
		ID             string   `json:"question_id"`
		Prompt         string   `json:"question"`
		State          string   `json:"state"`
		Resolution     string   `json:"resolution"`
		ObservationIDs []string `json:"observation_ids"`
	}
	if err := json.Unmarshal(questions, &rawQuestions); err != nil {
		return domain.Snapshot{}, err
	}
	parsedQuestions := make([]domain.QuestionSnapshot, 0, len(rawQuestions))
	for _, raw := range rawQuestions {
		questionID, err := id.Parse(raw.ID)
		if err != nil {
			return domain.Snapshot{}, err
		}
		observationIDs, err := parseIDs(raw.ObservationIDs)
		if err != nil {
			return domain.Snapshot{}, err
		}
		parsedQuestions = append(parsedQuestions, domain.QuestionSnapshot{ID: questionID, Prompt: raw.Prompt, State: raw.State, Resolution: raw.Resolution, ObservationIDs: observationIDs})
	}
	var rawConnections []struct {
		ID                       string   `json:"connection_id"`
		FromRecordID             string   `json:"from_record_id"`
		FromRecordKind           string   `json:"from_record_kind"`
		FromRecordName           string   `json:"from_record_name"`
		FromRecordDescription    string   `json:"from_record_description"`
		FromRecordObservationIDs []string `json:"from_record_observation_ids"`
		ToRecordID               string   `json:"to_record_id"`
		ToRecordKind             string   `json:"to_record_kind"`
		ToRecordName             string   `json:"to_record_name"`
		ToRecordDescription      string   `json:"to_record_description"`
		ToRecordObservationIDs   []string `json:"to_record_observation_ids"`
		Kind                     string   `json:"kind"`
		State                    string   `json:"state"`
		Rationale                string   `json:"rationale"`
		SupportingObservationIDs []string `json:"supporting_observation_ids"`
		OpposingObservationIDs   []string `json:"opposing_observation_ids"`
	}
	if err := json.Unmarshal(connections, &rawConnections); err != nil {
		return domain.Snapshot{}, err
	}
	parsedConnections := make([]domain.ConnectionSnapshot, 0, len(rawConnections))
	for _, raw := range rawConnections {
		connectionID, err := id.Parse(raw.ID)
		if err != nil {
			return domain.Snapshot{}, err
		}
		fromRecordID, err := id.Parse(raw.FromRecordID)
		if err != nil {
			return domain.Snapshot{}, err
		}
		toRecordID, err := id.Parse(raw.ToRecordID)
		if err != nil {
			return domain.Snapshot{}, err
		}
		supporting, err := parseIDs(raw.SupportingObservationIDs)
		if err != nil {
			return domain.Snapshot{}, err
		}
		opposing, err := parseIDs(raw.OpposingObservationIDs)
		if err != nil {
			return domain.Snapshot{}, err
		}
		fromRecordObservations, err := parseIDs(raw.FromRecordObservationIDs)
		if err != nil {
			return domain.Snapshot{}, err
		}
		toRecordObservations, err := parseIDs(raw.ToRecordObservationIDs)
		if err != nil {
			return domain.Snapshot{}, err
		}
		parsedConnections = append(parsedConnections, domain.ConnectionSnapshot{
			ID: connectionID, FromRecordID: fromRecordID, FromRecordKind: raw.FromRecordKind, FromRecordName: raw.FromRecordName,
			FromRecordDescription: raw.FromRecordDescription, FromRecordObservationIDs: fromRecordObservations,
			ToRecordID: toRecordID, ToRecordKind: raw.ToRecordKind, ToRecordName: raw.ToRecordName,
			ToRecordDescription: raw.ToRecordDescription, ToRecordObservationIDs: toRecordObservations,
			Kind: raw.Kind, State: raw.State, Rationale: raw.Rationale,
			SupportingObservationIDs: supporting, OpposingObservationIDs: opposing,
		})
	}
	var rawEvents []rawEventSnapshot
	if err := json.Unmarshal(events, &rawEvents); err != nil {
		return domain.Snapshot{}, err
	}
	parsedEvents := make([]domain.EventSnapshot, 0, len(rawEvents))
	for _, raw := range rawEvents {
		eventID, err := id.Parse(raw.ID)
		if err != nil {
			return domain.Snapshot{}, err
		}
		observationIDs, err := parseIDs(raw.ObservationIDs)
		if err != nil {
			return domain.Snapshot{}, err
		}
		participants := make([]domain.EventRecordSnapshot, 0, len(raw.ParticipantRecords))
		for _, participant := range raw.ParticipantRecords {
			parsed, err := parseEventRecordSnapshot(participant)
			if err != nil {
				return domain.Snapshot{}, err
			}
			participants = append(participants, parsed)
		}
		var location *domain.EventRecordSnapshot
		if raw.LocationRecord != nil {
			parsed, err := parseEventRecordSnapshot(*raw.LocationRecord)
			if err != nil {
				return domain.Snapshot{}, err
			}
			location = &parsed
		}
		snapshot := domain.EventSnapshot{ID: eventID, Title: raw.Title, Description: raw.Description, ReportedTime: raw.ReportedTime, TimePrecision: raw.TimePrecision, SortDate: raw.SortDate, Location: raw.Location, ObservationIDs: observationIDs, ParticipantRecords: participants, LocationRecord: location}
		if raw.RevisionID != nil {
			snapshot.RevisionID, err = id.Parse(*raw.RevisionID)
			if err != nil {
				return domain.Snapshot{}, err
			}
		}
		if raw.Revision != nil {
			snapshot.Revision = *raw.Revision
		}
		parsedEvents = append(parsedEvents, snapshot)
	}
	var rawEventRelationships []rawEventRelationshipSnapshot
	if err := json.Unmarshal(eventRelationships, &rawEventRelationships); err != nil {
		return domain.Snapshot{}, err
	}
	parsedEventRelationships := make([]domain.EventRelationshipSnapshot, 0, len(rawEventRelationships))
	for _, raw := range rawEventRelationships {
		relationshipID, err := id.Parse(raw.ID)
		if err != nil {
			return domain.Snapshot{}, err
		}
		fromEventID, err := id.Parse(raw.FromEventID)
		if err != nil {
			return domain.Snapshot{}, err
		}
		toEventID, err := id.Parse(raw.ToEventID)
		if err != nil {
			return domain.Snapshot{}, err
		}
		supporting, err := parseIDs(raw.SupportingObservationIDs)
		if err != nil {
			return domain.Snapshot{}, err
		}
		opposing, err := parseIDs(raw.OpposingObservationIDs)
		if err != nil {
			return domain.Snapshot{}, err
		}
		parsedEventRelationships = append(parsedEventRelationships, domain.EventRelationshipSnapshot{ID: relationshipID, FromEventID: fromEventID, ToEventID: toEventID, Kind: raw.Kind, Rationale: raw.Rationale, State: raw.State, ReviewNote: raw.ReviewNote, SupportingObservationIDs: supporting, OpposingObservationIDs: opposing})
	}
	out.ID, out.WorkspaceID, out.BriefID = id.ID(snapshotID.Bytes), id.ID(workspace.Bytes), id.ID(briefID.Bytes)
	out.Author, out.UpdatedBy, out.FrozenBy = id.ID(author.Bytes), id.ID(updatedBy.Bytes), id.ID(frozenBy.Bytes)
	out.ObservationIDs, out.Clusters, out.Questions, out.Connections, out.Events, out.EventRelationships = parsedObservations, parsedClusters, parsedQuestions, parsedConnections, parsedEvents, parsedEventRelationships
	return out, nil
}

func (s *Store) ByWorkspace(ctx context.Context, workspace id.ID) (domain.Brief, error) {
	out, err := scanBrief(s.db.DB(ctx).QueryRow(ctx, briefSelect+` where b.workspace_id=$1`, uuid(workspace)))
	if err != nil {
		return domain.Brief{}, translate(ctx, err)
	}
	return out, nil
}

func (s *Store) ByWorkspaceVisible(ctx context.Context, workspace id.ID, maxSensitivity string) (domain.Brief, error) {
	out, err := scanBrief(s.db.DB(ctx).QueryRow(ctx, briefSelect+` where b.workspace_id=$1`+workingVisibility, uuid(workspace), maxSensitivity))
	if err != nil {
		return domain.Brief{}, translate(ctx, err)
	}
	return out, nil
}

func (s *Store) Create(ctx context.Context, in domain.Brief) error {
	_, err := s.db.DB(ctx).Exec(ctx, `insert into brief.working(id,workspace_id,title,question,current_account,alternatives,limitations,next_steps,author,updated_by,created_at,updated_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, uuid(in.ID), uuid(in.WorkspaceID), in.Title, in.Question, in.CurrentAccount, in.Alternatives, in.Limitations, in.NextSteps, uuid(in.Author), uuid(in.UpdatedBy), in.CreatedAt, in.UpdatedAt)
	return translate(ctx, err)
}

func (s *Store) Save(ctx context.Context, in domain.Brief) error {
	tag, err := s.db.DB(ctx).Exec(ctx, `update brief.working set title=$3,question=$4,current_account=$5,alternatives=$6,limitations=$7,next_steps=$8,updated_by=$9,updated_at=$10 where workspace_id=$1 and id=$2`, uuid(in.WorkspaceID), uuid(in.ID), in.Title, in.Question, in.CurrentAccount, in.Alternatives, in.Limitations, in.NextSteps, uuid(in.UpdatedBy), in.UpdatedAt)
	if err != nil {
		return translate(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) ReplaceObservations(ctx context.Context, workspace, brief id.ID, observations []id.ID) error {
	if _, err := s.db.DB(ctx).Exec(ctx, `delete from brief.working_observation where workspace_id=$1 and brief_id=$2`, uuid(workspace), uuid(brief)); err != nil {
		return translate(ctx, err)
	}
	for _, observation := range observations {
		tag, err := s.db.DB(ctx).Exec(ctx, `insert into brief.working_observation(workspace_id,brief_id,observation_id) select $1,$2,$3 where exists (select 1 from brief.working where id=$2 and workspace_id=$1) and exists (select 1 from observation.manual where id=$3 and workspace_id=$1)`, uuid(workspace), uuid(brief), uuid(observation))
		if err != nil {
			return translate(ctx, err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (s *Store) ReplaceClusters(ctx context.Context, workspace, brief id.ID, clusters []id.ID) error {
	if _, err := s.db.DB(ctx).Exec(ctx, `delete from brief.working_cluster where workspace_id=$1 and brief_id=$2`, uuid(workspace), uuid(brief)); err != nil {
		return translate(ctx, err)
	}
	for _, cluster := range clusters {
		tag, err := s.db.DB(ctx).Exec(ctx, `insert into brief.working_cluster(workspace_id,brief_id,cluster_id) select $1,$2,$3 where exists (select 1 from brief.working where id=$2 and workspace_id=$1) and exists (select 1 from review.cluster where id=$3 and workspace_id=$1)`, uuid(workspace), uuid(brief), uuid(cluster))
		if err != nil {
			return translate(ctx, err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (s *Store) ReplaceQuestions(ctx context.Context, workspace, brief id.ID, questions []id.ID) error {
	if _, err := s.db.DB(ctx).Exec(ctx, `delete from brief.working_question where workspace_id=$1 and brief_id=$2`, uuid(workspace), uuid(brief)); err != nil {
		return translate(ctx, err)
	}
	for _, question := range questions {
		tag, err := s.db.DB(ctx).Exec(ctx, `insert into brief.working_question(workspace_id,brief_id,question_id) select $1,$2,$3 where exists (select 1 from brief.working where id=$2 and workspace_id=$1) and exists (select 1 from lead.question where id=$3 and workspace_id=$1)`, uuid(workspace), uuid(brief), uuid(question))
		if err != nil {
			return translate(ctx, err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (s *Store) ReplaceConnections(ctx context.Context, workspace, brief id.ID, connections []id.ID) error {
	if _, err := s.db.DB(ctx).Exec(ctx, `delete from brief.working_connection where workspace_id=$1 and brief_id=$2`, uuid(workspace), uuid(brief)); err != nil {
		return translate(ctx, err)
	}
	for _, connection := range connections {
		tag, err := s.db.DB(ctx).Exec(ctx, `insert into brief.working_connection(workspace_id,brief_id,connection_id) select $1,$2,$3 where exists (select 1 from brief.working where id=$2 and workspace_id=$1) and exists (select 1 from research.connection where id=$3 and workspace_id=$1)`, uuid(workspace), uuid(brief), uuid(connection))
		if err != nil {
			return translate(ctx, err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (s *Store) ReplaceEvents(ctx context.Context, workspace, brief id.ID, events []id.ID) error {
	if _, err := s.db.DB(ctx).Exec(ctx, `delete from brief.working_event where workspace_id=$1 and brief_id=$2`, uuid(workspace), uuid(brief)); err != nil {
		return translate(ctx, err)
	}
	for _, event := range events {
		tag, err := s.db.DB(ctx).Exec(ctx, `insert into brief.working_event(workspace_id,brief_id,event_id) select $1,$2,$3 where exists (select 1 from brief.working where id=$2 and workspace_id=$1) and exists (select 1 from timeline.event where id=$3 and workspace_id=$1)`, uuid(workspace), uuid(brief), uuid(event))
		if err != nil {
			return translate(ctx, err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (s *Store) QuestionSnapshots(ctx context.Context, workspace id.ID, questions []id.ID) ([]domain.QuestionSnapshot, error) {
	out := make([]domain.QuestionSnapshot, 0, len(questions))
	for _, question := range questions {
		var rawID pgtype.UUID
		var snapshot domain.QuestionSnapshot
		var state string
		var observationRaw []byte
		err := s.db.DB(ctx).QueryRow(ctx, `
			select q.id,q.question,q.state,q.resolution,
			coalesce((select json_agg(qo.observation_id order by qo.observation_id) from lead.question_observation qo where qo.workspace_id=q.workspace_id and qo.question_id=q.id), '[]'::json)::text
			from lead.question q
			where q.workspace_id=$1 and q.id=$2`, uuid(workspace), uuid(question)).Scan(&rawID, &snapshot.Prompt, &state, &snapshot.Resolution, &observationRaw)
		if err != nil {
			if stderrors.Is(err, pgx.ErrNoRows) {
				return nil, domain.ErrQuestionSnapshotMissing
			}
			return nil, translate(ctx, err)
		}
		var observationIDs []string
		if err := json.Unmarshal(observationRaw, &observationIDs); err != nil {
			return nil, err
		}
		parsedObservations, err := parseIDs(observationIDs)
		if err != nil {
			return nil, err
		}
		snapshot.ID, snapshot.State, snapshot.ObservationIDs = id.ID(rawID.Bytes), state, parsedObservations
		out = append(out, snapshot)
	}
	return out, nil
}

func (s *Store) ClusterSnapshots(ctx context.Context, workspace id.ID, clusters []id.ID) ([]domain.ClusterSnapshot, error) {
	out := make([]domain.ClusterSnapshot, 0, len(clusters))
	for _, cluster := range clusters {
		var snapshot domain.ClusterSnapshot
		var rawID pgtype.UUID
		var observations []byte
		err := s.db.DB(ctx).QueryRow(ctx, `
			select c.id,c.kind,c.title,c.description,
          coalesce((select json_agg(co.observation_id order by co.ordinal) from review.cluster_observation co where co.cluster_id=c.id), '[]'::json)::text
			from review.cluster c where c.workspace_id=$1 and c.id=$2`, uuid(workspace), uuid(cluster)).Scan(&rawID, &snapshot.Kind, &snapshot.Title, &snapshot.Description, &observations)
		if err != nil {
			if stderrors.Is(err, pgx.ErrNoRows) {
				return nil, domain.ErrClusterSnapshotMissing
			}
			return nil, translate(ctx, err)
		}
		var observationIDs []string
		if err := json.Unmarshal(observations, &observationIDs); err != nil {
			return nil, err
		}
		parsed, err := parseIDs(observationIDs)
		if err != nil {
			return nil, err
		}
		snapshot.ID = id.ID(rawID.Bytes)
		snapshot.ObservationIDs = parsed
		out = append(out, snapshot)
	}
	return out, nil
}

func (s *Store) ConnectionSnapshots(ctx context.Context, workspace id.ID, connections []id.ID) ([]domain.ConnectionSnapshot, error) {
	out := make([]domain.ConnectionSnapshot, 0, len(connections))
	for _, connection := range connections {
		var snapshot domain.ConnectionSnapshot
		var connectionID, fromRecordID, toRecordID pgtype.UUID
		var fromRecordObservationRaw, toRecordObservationRaw, supportingRaw, opposingRaw []byte
		err := s.db.DB(ctx).QueryRow(ctx, `
			select c.id,c.from_record_id,fr.kind,fr.name,fr.description,
			coalesce((select json_agg(ro.observation_id order by ro.observation_id) from research.record_observation ro where ro.workspace_id=fr.workspace_id and ro.record_id=fr.id), '[]'::json)::text,
			c.to_record_id,tr.kind,tr.name,tr.description,
			coalesce((select json_agg(ro.observation_id order by ro.observation_id) from research.record_observation ro where ro.workspace_id=tr.workspace_id and ro.record_id=tr.id), '[]'::json)::text,
			c.kind,c.state,c.rationale,
			coalesce(json_agg(ce.observation_id order by ce.observation_id) filter (where ce.polarity='supporting'), '[]'::json)::text,
			coalesce(json_agg(ce.observation_id order by ce.observation_id) filter (where ce.polarity='opposing'), '[]'::json)::text
			from research.connection c
			join research.record fr on fr.id=c.from_record_id and fr.workspace_id=c.workspace_id
			join research.record tr on tr.id=c.to_record_id and tr.workspace_id=c.workspace_id
			left join research.connection_evidence ce on ce.connection_id=c.id
			where c.workspace_id=$1 and c.id=$2
			group by c.id,c.workspace_id,c.from_record_id,fr.id,fr.workspace_id,fr.kind,fr.name,fr.description,c.to_record_id,tr.id,tr.workspace_id,tr.kind,tr.name,tr.description,c.kind,c.state,c.rationale`, uuid(workspace), uuid(connection)).Scan(
			&connectionID, &fromRecordID, &snapshot.FromRecordKind, &snapshot.FromRecordName, &snapshot.FromRecordDescription, &fromRecordObservationRaw,
			&toRecordID, &snapshot.ToRecordKind, &snapshot.ToRecordName, &snapshot.ToRecordDescription, &toRecordObservationRaw,
			&snapshot.Kind, &snapshot.State, &snapshot.Rationale, &supportingRaw, &opposingRaw)
		if err != nil {
			if stderrors.Is(err, pgx.ErrNoRows) {
				return nil, domain.ErrConnectionSnapshotMissing
			}
			return nil, translate(ctx, err)
		}
		var fromRecordObservationIDs, toRecordObservationIDs, supporting, opposing []string
		if err := json.Unmarshal(fromRecordObservationRaw, &fromRecordObservationIDs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(toRecordObservationRaw, &toRecordObservationIDs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(supportingRaw, &supporting); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(opposingRaw, &opposing); err != nil {
			return nil, err
		}
		parsedSupporting, err := parseIDs(supporting)
		if err != nil {
			return nil, err
		}
		parsedOpposing, err := parseIDs(opposing)
		if err != nil {
			return nil, err
		}
		parsedFromRecordObservations, err := parseIDs(fromRecordObservationIDs)
		if err != nil {
			return nil, err
		}
		parsedToRecordObservations, err := parseIDs(toRecordObservationIDs)
		if err != nil {
			return nil, err
		}
		snapshot.ID, snapshot.FromRecordID, snapshot.ToRecordID = id.ID(connectionID.Bytes), id.ID(fromRecordID.Bytes), id.ID(toRecordID.Bytes)
		snapshot.FromRecordObservationIDs, snapshot.ToRecordObservationIDs = parsedFromRecordObservations, parsedToRecordObservations
		snapshot.SupportingObservationIDs, snapshot.OpposingObservationIDs = parsedSupporting, parsedOpposing
		out = append(out, snapshot)
	}
	return out, nil
}

func (s *Store) EventSnapshots(ctx context.Context, workspace id.ID, events []id.ID) ([]domain.EventSnapshot, error) {
	out := make([]domain.EventSnapshot, 0, len(events))
	for _, event := range events {
		var snapshot domain.EventSnapshot
		var eventID, revisionID pgtype.UUID
		var revision pgtype.Int4
		var observations, participants, location []byte
		err := s.db.DB(ctx).QueryRow(ctx, `
			select e.id,er.id,er.revision,e.title,e.description,e.reported_time,e.time_precision,e.sort_date,e.location,
			coalesce((select json_agg(eo.observation_id order by eo.observation_id) from timeline.event_observation eo where eo.workspace_id=e.workspace_id and eo.event_id=e.id), '[]'::json)::text,
			coalesce((select json_agg(json_build_object('record_id',r.id,'kind',r.kind,'name',r.name,'description',r.description,'observation_ids',coalesce((select json_agg(ro.observation_id order by ro.observation_id) from research.record_observation ro where ro.workspace_id=r.workspace_id and ro.record_id=r.id), '[]'::json),'place_geometry',(select json_build_object('latitude',g.latitude,'longitude',g.longitude,'precision',g.precision,'observation_ids',g.observation_ids) from research.record_place_geometry g where g.workspace_id=r.workspace_id and g.record_id=r.id)) order by r.id) from timeline.event_participant ep join research.record r on r.id=ep.record_id and r.workspace_id=ep.workspace_id where ep.workspace_id=e.workspace_id and ep.event_id=e.id), '[]'::json)::text,
			coalesce((select json_build_object('record_id',r.id,'kind',r.kind,'name',r.name,'description',r.description,'observation_ids',coalesce((select json_agg(ro.observation_id order by ro.observation_id) from research.record_observation ro where ro.workspace_id=r.workspace_id and ro.record_id=r.id), '[]'::json),'place_geometry',(select json_build_object('latitude',g.latitude,'longitude',g.longitude,'precision',g.precision,'observation_ids',g.observation_ids) from research.record_place_geometry g where g.workspace_id=r.workspace_id and g.record_id=r.id)) from research.record r where r.workspace_id=e.workspace_id and r.id=e.location_record_id), 'null'::json)::text
			from timeline.event e
			left join lateral (select id,revision from timeline.event_revision where workspace_id=e.workspace_id and event_id=e.id order by revision desc limit 1) er on true
			where e.workspace_id=$1 and e.id=$2`, uuid(workspace), uuid(event)).Scan(
			&eventID, &revisionID, &revision, &snapshot.Title, &snapshot.Description, &snapshot.ReportedTime, &snapshot.TimePrecision, &snapshot.SortDate, &snapshot.Location, &observations, &participants, &location)
		if err != nil {
			if stderrors.Is(err, pgx.ErrNoRows) {
				return nil, domain.ErrEventSnapshotMissing
			}
			return nil, translate(ctx, err)
		}
		observationIDs := []string{}
		if err := json.Unmarshal(observations, &observationIDs); err != nil {
			return nil, err
		}
		snapshot.ID = id.ID(eventID.Bytes)
		if revisionID.Valid {
			snapshot.RevisionID = id.ID(revisionID.Bytes)
		}
		if revision.Valid {
			snapshot.Revision = int(revision.Int32)
		}
		snapshot.ObservationIDs, err = parseIDs(observationIDs)
		if err != nil {
			return nil, err
		}
		var rawParticipants []rawEventRecordSnapshot
		if err := json.Unmarshal(participants, &rawParticipants); err != nil {
			return nil, err
		}
		snapshot.ParticipantRecords = make([]domain.EventRecordSnapshot, 0, len(rawParticipants))
		for _, raw := range rawParticipants {
			parsed, err := parseEventRecordSnapshot(raw)
			if err != nil {
				return nil, err
			}
			snapshot.ParticipantRecords = append(snapshot.ParticipantRecords, parsed)
		}
		var rawLocation *rawEventRecordSnapshot
		if string(location) != "null" {
			if err := json.Unmarshal(location, &rawLocation); err != nil {
				return nil, err
			}
		}
		if rawLocation != nil {
			parsed, err := parseEventRecordSnapshot(*rawLocation)
			if err != nil {
				return nil, err
			}
			snapshot.LocationRecord = &parsed
		}
		out = append(out, snapshot)
	}
	return out, nil
}

func (s *Store) EventRelationshipSnapshots(ctx context.Context, workspace id.ID, events []id.ID) ([]domain.EventRelationshipSnapshot, error) {
	out := make([]domain.EventRelationshipSnapshot, 0)
	if len(events) == 0 {
		return out, nil
	}
	eventIDs := make([]pgtype.UUID, 0, len(events))
	for _, event := range events {
		eventIDs = append(eventIDs, uuid(event))
	}
	rows, err := s.db.DB(ctx).Query(ctx, `
		select r.id,r.from_event_id,r.to_event_id,r.kind,r.rationale,r.state,r.review_note,
		coalesce((select json_agg(s.supporting_observation_id order by s.supporting_observation_id) from timeline.event_relationship_supporting s where s.workspace_id=r.workspace_id and s.relationship_id=r.id), '[]'::json)::text,
		coalesce((select json_agg(o.opposing_observation_id order by o.opposing_observation_id) from timeline.event_relationship_opposing o where o.workspace_id=r.workspace_id and o.relationship_id=r.id), '[]'::json)::text
		from timeline.event_relationship r
		where r.workspace_id=$1 and r.from_event_id=any($2::uuid[]) and r.to_event_id=any($2::uuid[])
		order by r.id`, uuid(workspace), eventIDs)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	for rows.Next() {
		var relationshipID, fromEventID, toEventID pgtype.UUID
		var kind, rationale, state, reviewNote string
		var supportingRaw, opposingRaw []byte
		if err := rows.Scan(&relationshipID, &fromEventID, &toEventID, &kind, &rationale, &state, &reviewNote, &supportingRaw, &opposingRaw); err != nil {
			return nil, translate(ctx, err)
		}
		var supporting, opposing []string
		if err := json.Unmarshal(supportingRaw, &supporting); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(opposingRaw, &opposing); err != nil {
			return nil, err
		}
		parsedSupporting, err := parseIDs(supporting)
		if err != nil {
			return nil, err
		}
		parsedOpposing, err := parseIDs(opposing)
		if err != nil {
			return nil, err
		}
		out = append(out, domain.EventRelationshipSnapshot{ID: id.ID(relationshipID.Bytes), FromEventID: id.ID(fromEventID.Bytes), ToEventID: id.ID(toEventID.Bytes), Kind: kind, Rationale: rationale, State: state, ReviewNote: reviewNote, SupportingObservationIDs: parsedSupporting, OpposingObservationIDs: parsedOpposing})
	}
	return out, translate(ctx, rows.Err())
}

func (s *Store) CreateSnapshot(ctx context.Context, in domain.Snapshot) error {
	_, err := s.db.DB(ctx).Exec(ctx, `insert into brief.snapshot(id,workspace_id,brief_id,title,question,current_account,alternatives,limitations,next_steps,author,updated_by,frozen_by,source_updated_at,frozen_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, uuid(in.ID), uuid(in.WorkspaceID), uuid(in.BriefID), in.Title, in.Question, in.CurrentAccount, in.Alternatives, in.Limitations, in.NextSteps, uuid(in.Author), uuid(in.UpdatedBy), uuid(in.FrozenBy), in.SourceUpdatedAt, in.FrozenAt)
	return translate(ctx, err)
}

func (s *Store) ReplaceSnapshotObservations(ctx context.Context, workspace, snapshot id.ID, observations []id.ID) error {
	if _, err := s.db.DB(ctx).Exec(ctx, `delete from brief.snapshot_observation where workspace_id=$1 and snapshot_id=$2`, uuid(workspace), uuid(snapshot)); err != nil {
		return translate(ctx, err)
	}
	for _, observation := range observations {
		tag, err := s.db.DB(ctx).Exec(ctx, `insert into brief.snapshot_observation(workspace_id,snapshot_id,observation_id) select $1,$2,$3 where exists (select 1 from brief.snapshot where id=$2 and workspace_id=$1) and exists (select 1 from observation.manual where id=$3 and workspace_id=$1)`, uuid(workspace), uuid(snapshot), uuid(observation))
		if err != nil {
			return translate(ctx, err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (s *Store) ReplaceSnapshotClusters(ctx context.Context, workspace, snapshot id.ID, clusters []domain.ClusterSnapshot) error {
	if _, err := s.db.DB(ctx).Exec(ctx, `delete from brief.snapshot_cluster where workspace_id=$1 and snapshot_id=$2`, uuid(workspace), uuid(snapshot)); err != nil {
		return translate(ctx, err)
	}
	for _, cluster := range clusters {
		observations, err := json.Marshal(stringIDs(cluster.ObservationIDs))
		if err != nil {
			return err
		}
		tag, err := s.db.DB(ctx).Exec(ctx, `insert into brief.snapshot_cluster(workspace_id,snapshot_id,cluster_id,kind,title,description,observation_ids) values($1,$2,$3,$4,$5,$6,$7::jsonb)`, uuid(workspace), uuid(snapshot), uuid(cluster.ID), cluster.Kind, cluster.Title, cluster.Description, string(observations))
		if err != nil {
			return translate(ctx, err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (s *Store) ReplaceSnapshotQuestions(ctx context.Context, workspace, snapshot id.ID, questions []domain.QuestionSnapshot) error {
	if _, err := s.db.DB(ctx).Exec(ctx, `delete from brief.snapshot_question where workspace_id=$1 and snapshot_id=$2`, uuid(workspace), uuid(snapshot)); err != nil {
		return translate(ctx, err)
	}
	for _, question := range questions {
		observations, err := json.Marshal(stringIDs(question.ObservationIDs))
		if err != nil {
			return err
		}
		tag, err := s.db.DB(ctx).Exec(ctx, `insert into brief.snapshot_question(workspace_id,snapshot_id,question_id,question,state,resolution,observation_ids) values($1,$2,$3,$4,$5,$6,$7::jsonb)`, uuid(workspace), uuid(snapshot), uuid(question.ID), question.Prompt, question.State, question.Resolution, string(observations))
		if err != nil {
			return translate(ctx, err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (s *Store) ReplaceSnapshotConnections(ctx context.Context, workspace, snapshot id.ID, connections []domain.ConnectionSnapshot) error {
	if _, err := s.db.DB(ctx).Exec(ctx, `delete from brief.snapshot_connection where workspace_id=$1 and snapshot_id=$2`, uuid(workspace), uuid(snapshot)); err != nil {
		return translate(ctx, err)
	}
	for _, connection := range connections {
		supporting, err := json.Marshal(stringIDs(connection.SupportingObservationIDs))
		if err != nil {
			return err
		}
		opposing, err := json.Marshal(stringIDs(connection.OpposingObservationIDs))
		if err != nil {
			return err
		}
		fromRecordObservations, err := json.Marshal(stringIDs(connection.FromRecordObservationIDs))
		if err != nil {
			return err
		}
		toRecordObservations, err := json.Marshal(stringIDs(connection.ToRecordObservationIDs))
		if err != nil {
			return err
		}
		tag, err := s.db.DB(ctx).Exec(ctx, `insert into brief.snapshot_connection(workspace_id,snapshot_id,connection_id,from_record_id,from_record_kind,from_record_name,from_record_description,from_record_observation_ids,to_record_id,to_record_kind,to_record_name,to_record_description,to_record_observation_ids,kind,state,rationale,supporting_observation_ids,opposing_observation_ids) values($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12,$13::jsonb,$14,$15,$16,$17::jsonb,$18::jsonb)`, uuid(workspace), uuid(snapshot), uuid(connection.ID), uuid(connection.FromRecordID), connection.FromRecordKind, connection.FromRecordName, connection.FromRecordDescription, string(fromRecordObservations), uuid(connection.ToRecordID), connection.ToRecordKind, connection.ToRecordName, connection.ToRecordDescription, string(toRecordObservations), connection.Kind, connection.State, connection.Rationale, string(supporting), string(opposing))
		if err != nil {
			return translate(ctx, err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (s *Store) ReplaceSnapshotEvents(ctx context.Context, workspace, snapshot id.ID, events []domain.EventSnapshot) error {
	if _, err := s.db.DB(ctx).Exec(ctx, `delete from brief.snapshot_event where workspace_id=$1 and snapshot_id=$2`, uuid(workspace), uuid(snapshot)); err != nil {
		return translate(ctx, err)
	}
	for _, event := range events {
		observations, err := json.Marshal(stringIDs(event.ObservationIDs))
		if err != nil {
			return err
		}
		participantRecords := make([]storedEventRecordSnapshot, 0, len(event.ParticipantRecords))
		for _, participant := range event.ParticipantRecords {
			participantRecords = append(participantRecords, storedEventRecord(participant))
		}
		participants, err := json.Marshal(participantRecords)
		if err != nil {
			return err
		}
		var locationRecord *storedEventRecordSnapshot
		if event.LocationRecord != nil {
			stored := storedEventRecord(*event.LocationRecord)
			locationRecord = &stored
		}
		location, err := json.Marshal(locationRecord)
		if err != nil {
			return err
		}
		var revisionID *id.ID
		if !event.RevisionID.IsZero() {
			revisionID = &event.RevisionID
		}
		var revision any
		if event.Revision > 0 {
			revision = event.Revision
		}
		tag, err := s.db.DB(ctx).Exec(ctx, `insert into brief.snapshot_event(workspace_id,snapshot_id,event_id,event_revision_id,event_revision,title,description,reported_time,time_precision,sort_date,location,observation_ids,participant_records,location_record) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13::jsonb,$14::jsonb)`, uuid(workspace), uuid(snapshot), uuid(event.ID), optionalUUID(revisionID), revision, event.Title, event.Description, event.ReportedTime, event.TimePrecision, event.SortDate, event.Location, string(observations), string(participants), string(location))
		if err != nil {
			return translate(ctx, err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (s *Store) ReplaceSnapshotEventRelationships(ctx context.Context, workspace, snapshot id.ID, relationships []domain.EventRelationshipSnapshot) error {
	if _, err := s.db.DB(ctx).Exec(ctx, `delete from brief.snapshot_event_relationship where workspace_id=$1 and snapshot_id=$2`, uuid(workspace), uuid(snapshot)); err != nil {
		return translate(ctx, err)
	}
	for _, relationship := range relationships {
		supporting, err := json.Marshal(stringIDs(relationship.SupportingObservationIDs))
		if err != nil {
			return err
		}
		opposing, err := json.Marshal(stringIDs(relationship.OpposingObservationIDs))
		if err != nil {
			return err
		}
		_, err = s.db.DB(ctx).Exec(ctx, `insert into brief.snapshot_event_relationship(workspace_id,snapshot_id,relationship_id,from_event_id,to_event_id,kind,rationale,state,review_note,supporting_observation_ids,opposing_observation_ids) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11::jsonb)`, uuid(workspace), uuid(snapshot), uuid(relationship.ID), uuid(relationship.FromEventID), uuid(relationship.ToEventID), relationship.Kind, relationship.Rationale, relationship.State, relationship.ReviewNote, string(supporting), string(opposing))
		if err != nil {
			return translate(ctx, err)
		}
	}
	return nil
}

func stringIDs(values []id.ID) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.String())
	}
	return out
}

func (s *Store) SnapshotByID(ctx context.Context, workspace, snapshot id.ID) (domain.Snapshot, error) {
	out, err := scanSnapshot(s.db.DB(ctx).QueryRow(ctx, snapshotSelect+` where s.workspace_id=$1 and s.id=$2`, uuid(workspace), uuid(snapshot)))
	if err != nil {
		return domain.Snapshot{}, translate(ctx, err)
	}
	return out, nil
}

func (s *Store) SnapshotByIDVisible(ctx context.Context, workspace, snapshot id.ID, maxSensitivity string) (domain.Snapshot, error) {
	out, err := scanSnapshot(s.db.DB(ctx).QueryRow(ctx, snapshotSelect+` where s.workspace_id=$1 and s.id=$2`+snapshotVisibility, uuid(workspace), uuid(snapshot), maxSensitivity))
	if err != nil {
		return domain.Snapshot{}, translate(ctx, err)
	}
	return out, nil
}

func (s *Store) SnapshotPage(ctx context.Context, workspace, before id.ID, limit int) ([]domain.Snapshot, error) {
	rows, err := s.db.DB(ctx).Query(ctx, snapshotSelect+` where s.workspace_id=$1 and ($2::uuid is null or s.id < $2) order by s.id desc limit $3`, uuid(workspace), uuid(before), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Snapshot, 0)
	for rows.Next() {
		one, err := scanSnapshot(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}

func (s *Store) SnapshotPageVisible(ctx context.Context, workspace, before id.ID, limit int, maxSensitivity string) ([]domain.Snapshot, error) {
	rows, err := s.db.DB(ctx).Query(ctx, snapshotSelect+` where s.workspace_id=$1 and ($2::uuid is null or s.id < $2)`+snapshotVisibility+` order by s.id desc limit $4`, uuid(workspace), uuid(before), maxSensitivity, limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Snapshot, 0)
	for rows.Next() {
		one, err := scanSnapshot(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}

func (s *Store) SnapshotReview(ctx context.Context, workspace, snapshot id.ID) (domain.SnapshotReview, error) {
	var state string
	var updatedAt pgtype.Timestamptz
	var assignee, assignedBy pgtype.UUID
	var assignedAt pgtype.Timestamptz
	var decisions []byte
	row := s.db.DB(ctx).QueryRow(ctx, `
		select coalesce(sr.state, 'pending'), coalesce(sr.updated_at, s.frozen_at),
		       sr.assignee_id, sr.assigned_by, sr.assigned_at,
		       coalesce((select json_agg(x.item order by x.created_at desc)
		                  from (select json_build_object('decision_id', d.id, 'workspace_id', d.workspace_id, 'snapshot_id', d.snapshot_id, 'reviewer_id', d.reviewer_id, 'state', d.state, 'note', d.note, 'created_at', d.created_at) as item, d.created_at
		                        from brief.snapshot_review_decision d
		                        where d.workspace_id=$1 and d.snapshot_id=$2
		                        order by d.created_at desc limit 50) x), '[]'::json)::text
		from brief.snapshot s
		left join brief.snapshot_review sr on sr.workspace_id=s.workspace_id and sr.snapshot_id=s.id
		where s.workspace_id=$1 and s.id=$2`, uuid(workspace), uuid(snapshot))
	if err := row.Scan(&state, &updatedAt, &assignee, &assignedBy, &assignedAt, &decisions); err != nil {
		return domain.SnapshotReview{}, translate(ctx, err)
	}
	parsedState, err := domain.ParseReviewState(state)
	if err != nil {
		return domain.SnapshotReview{}, err
	}
	out := domain.SnapshotReview{WorkspaceID: workspace, SnapshotID: snapshot, State: parsedState, UpdatedAt: updatedAt.Time, Decisions: []domain.ReviewDecision{}}
	if assignee.Valid {
		value := id.ID(assignee.Bytes)
		out.AssigneeID = &value
	}
	if assignedBy.Valid {
		value := id.ID(assignedBy.Bytes)
		out.AssignedBy = &value
	}
	if assignedAt.Valid {
		value := assignedAt.Time
		out.AssignedAt = &value
	}
	var raw []struct {
		ID          string    `json:"decision_id"`
		WorkspaceID string    `json:"workspace_id"`
		SnapshotID  string    `json:"snapshot_id"`
		ReviewerID  string    `json:"reviewer_id"`
		State       string    `json:"state"`
		Note        string    `json:"note"`
		CreatedAt   time.Time `json:"created_at"`
	}
	if err := json.Unmarshal(decisions, &raw); err != nil {
		return domain.SnapshotReview{}, err
	}
	for _, one := range raw {
		decisionID, err := id.Parse(one.ID)
		if err != nil {
			return domain.SnapshotReview{}, err
		}
		decisionWorkspace, err := id.Parse(one.WorkspaceID)
		if err != nil {
			return domain.SnapshotReview{}, err
		}
		decisionSnapshot, err := id.Parse(one.SnapshotID)
		if err != nil {
			return domain.SnapshotReview{}, err
		}
		reviewer, err := id.Parse(one.ReviewerID)
		if err != nil {
			return domain.SnapshotReview{}, err
		}
		decisionState, err := domain.ParseReviewState(one.State)
		if err != nil || decisionState == domain.ReviewPending {
			return domain.SnapshotReview{}, domain.ErrReviewStateInvalid
		}
		out.Decisions = append(out.Decisions, domain.ReviewDecision{ID: decisionID, WorkspaceID: decisionWorkspace, SnapshotID: decisionSnapshot, ReviewerID: reviewer, State: decisionState, Note: one.Note, CreatedAt: one.CreatedAt})
	}
	return out, nil
}

func (s *Store) EnsureSnapshotReview(ctx context.Context, workspace, snapshot id.ID, at time.Time) error {
	var exists bool
	if err := s.db.DB(ctx).QueryRow(ctx, `select exists(select 1 from brief.snapshot where workspace_id=$1 and id=$2)`, uuid(workspace), uuid(snapshot)).Scan(&exists); err != nil {
		return translate(ctx, err)
	}
	if !exists {
		return domain.ErrNotFound
	}
	_, err := s.db.DB(ctx).Exec(ctx, `insert into brief.snapshot_review(workspace_id,snapshot_id,state,updated_at) values($1,$2,'pending',$3) on conflict (snapshot_id) do nothing`, uuid(workspace), uuid(snapshot), at)
	return translate(ctx, err)
}

func (s *Store) AssignSnapshotReviewer(ctx context.Context, workspace, snapshot id.ID, assignee *id.ID, assignedBy id.ID, at time.Time) error {
	tag, err := s.db.DB(ctx).Exec(ctx, `update brief.snapshot_review set assignee_id=$3, assigned_by=$4, assigned_at=$5, updated_at=$6 where workspace_id=$1 and snapshot_id=$2`, uuid(workspace), uuid(snapshot), optionalUUID(assignee), uuid(assignedBy), at, at)
	if err != nil {
		return translate(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) AddReviewDecision(ctx context.Context, decision domain.ReviewDecision) error {
	_, err := s.db.DB(ctx).Exec(ctx, `insert into brief.snapshot_review_decision(id,workspace_id,snapshot_id,reviewer_id,state,note,created_at) values($1,$2,$3,$4,$5,$6,$7)`, uuid(decision.ID), uuid(decision.WorkspaceID), uuid(decision.SnapshotID), uuid(decision.ReviewerID), decision.State, decision.Note, decision.CreatedAt)
	if err != nil {
		return translate(ctx, err)
	}
	_, err = s.db.DB(ctx).Exec(ctx, `update brief.snapshot_review set state=$3, updated_at=$4 where workspace_id=$1 and snapshot_id=$2`, uuid(decision.WorkspaceID), uuid(decision.SnapshotID), decision.State, decision.CreatedAt)
	return translate(ctx, err)
}

func (s *Store) SnapshotComments(ctx context.Context, workspace, snapshot id.ID) ([]domain.SnapshotComment, error) {
	var exists bool
	if err := s.db.DB(ctx).QueryRow(ctx, `select exists(select 1 from brief.snapshot where workspace_id=$1 and id=$2)`, uuid(workspace), uuid(snapshot)).Scan(&exists); err != nil {
		return nil, translate(ctx, err)
	}
	if !exists {
		return nil, domain.ErrNotFound
	}
	rows, err := s.db.DB(ctx).Query(ctx, `select id,workspace_id,snapshot_id,author_id,body,created_at from brief.snapshot_comment where workspace_id=$1 and snapshot_id=$2 order by created_at desc, id desc limit 100`, uuid(workspace), uuid(snapshot))
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.SnapshotComment, 0)
	for rows.Next() {
		var commentID, commentWorkspace, commentSnapshot, author pgtype.UUID
		var body string
		var createdAt pgtype.Timestamptz
		if err := rows.Scan(&commentID, &commentWorkspace, &commentSnapshot, &author, &body, &createdAt); err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, domain.SnapshotComment{ID: id.ID(commentID.Bytes), WorkspaceID: id.ID(commentWorkspace.Bytes), SnapshotID: id.ID(commentSnapshot.Bytes), AuthorID: id.ID(author.Bytes), Body: body, CreatedAt: createdAt.Time})
	}
	return out, translate(ctx, rows.Err())
}

func (s *Store) CreateSnapshotComment(ctx context.Context, comment domain.SnapshotComment) error {
	tag, err := s.db.DB(ctx).Exec(ctx, `insert into brief.snapshot_comment(id,workspace_id,snapshot_id,author_id,body,created_at) select $1,$2,s.id,$4,$5,$6 from brief.snapshot s where s.workspace_id=$2 and s.id=$3`, uuid(comment.ID), uuid(comment.WorkspaceID), uuid(comment.SnapshotID), uuid(comment.AuthorID), comment.Body, comment.CreatedAt)
	if err != nil {
		return translate(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func scanHandoffShare(row interface{ Scan(...any) error }) (domain.HandoffShare, error) {
	var out domain.HandoffShare
	var shareID, workspaceID, snapshotID, createdBy, revokedBy pgtype.UUID
	var createdAt, revokedAt pgtype.Timestamptz
	if err := row.Scan(&shareID, &workspaceID, &snapshotID, &createdBy, &out.TokenDigest, &createdAt, &revokedAt, &revokedBy); err != nil {
		return domain.HandoffShare{}, err
	}
	out.ID = id.ID(shareID.Bytes)
	out.WorkspaceID = id.ID(workspaceID.Bytes)
	out.SnapshotID = id.ID(snapshotID.Bytes)
	out.CreatedBy = id.ID(createdBy.Bytes)
	out.CreatedAt = createdAt.Time
	if revokedAt.Valid {
		at := revokedAt.Time
		out.RevokedAt = &at
	}
	if revokedBy.Valid {
		value := id.ID(revokedBy.Bytes)
		out.RevokedBy = &value
	}
	return out, nil
}

const handoffShareSelect = `select id,workspace_id,snapshot_id,created_by,token_digest,created_at,revoked_at,revoked_by from brief.snapshot_handoff_share`

func (s *Store) CreateSnapshotShare(ctx context.Context, share domain.HandoffShare) error {
	tag, err := s.db.DB(ctx).Exec(ctx, `insert into brief.snapshot_handoff_share(id,workspace_id,snapshot_id,created_by,token_digest,created_at) select $1,$2,s.id,$4,$5,$6 from brief.snapshot s where s.workspace_id=$2 and s.id=$3`, uuid(share.ID), uuid(share.WorkspaceID), uuid(share.SnapshotID), uuid(share.CreatedBy), share.TokenDigest, share.CreatedAt)
	if err != nil {
		return translate(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) RevokeSnapshotShare(ctx context.Context, workspace, share, revokedBy id.ID, at time.Time) (domain.HandoffShare, error) {
	row := s.db.DB(ctx).QueryRow(ctx, `update brief.snapshot_handoff_share set revoked_at=$3, revoked_by=$4 where workspace_id=$1 and id=$2 and revoked_at is null returning id,workspace_id,snapshot_id,created_by,token_digest,created_at,revoked_at,revoked_by`, uuid(workspace), uuid(share), at, uuid(revokedBy))
	out, err := scanHandoffShare(row)
	if err != nil {
		return domain.HandoffShare{}, translate(ctx, err)
	}
	return out, nil
}

func (s *Store) SnapshotHandoffShares(ctx context.Context, workspace, snapshot id.ID) ([]domain.HandoffShare, error) {
	var exists bool
	if err := s.db.DB(ctx).QueryRow(ctx, `select exists(select 1 from brief.snapshot where workspace_id=$1 and id=$2)`, uuid(workspace), uuid(snapshot)).Scan(&exists); err != nil {
		return nil, translate(ctx, err)
	}
	if !exists {
		return nil, domain.ErrNotFound
	}
	rows, err := s.db.DB(ctx).Query(ctx, handoffShareSelect+` where workspace_id=$1 and snapshot_id=$2 order by created_at desc, id desc limit 100`, uuid(workspace), uuid(snapshot))
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.HandoffShare, 0)
	for rows.Next() {
		one, err := scanHandoffShare(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}

func (s *Store) SnapshotByHandoffShare(ctx context.Context, workspace id.ID, tokenDigest string) (domain.Snapshot, error) {
	out, err := scanSnapshot(s.db.DB(ctx).QueryRow(ctx, snapshotSelect+` join brief.snapshot_handoff_share sh on sh.snapshot_id=s.id and sh.workspace_id=s.workspace_id where s.workspace_id=$1 and sh.token_digest=$2 and sh.revoked_at is null`, uuid(workspace), tokenDigest))
	if err != nil {
		return domain.Snapshot{}, translate(ctx, err)
	}
	return out, nil
}

func (s *Store) SnapshotByHandoffShareVisible(ctx context.Context, workspace id.ID, tokenDigest, maxSensitivity string) (domain.Snapshot, error) {
	out, err := scanSnapshot(s.db.DB(ctx).QueryRow(ctx, snapshotSelect+` join brief.snapshot_handoff_share sh on sh.snapshot_id=s.id and sh.workspace_id=s.workspace_id where s.workspace_id=$1 and sh.token_digest=$2 and sh.revoked_at is null`+snapshotVisibility, uuid(workspace), tokenDigest, maxSensitivity))
	if err != nil {
		return domain.Snapshot{}, translate(ctx, err)
	}
	return out, nil
}
