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

func translate(ctx context.Context, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return postgres.Translate(ctx, err, "timeline")
}

const eventSelect = `
select e.id,e.workspace_id,e.title,e.description,e.reported_time,e.time_precision,e.sort_date,e.location,
       e.author,e.updated_by,e.created_at,e.updated_at,
       coalesce(json_agg(eo.observation_id order by eo.observation_id)
         filter (where eo.observation_id is not null), '[]'::json)::text
from timeline.event e
left join timeline.event_observation eo
  on eo.event_id=e.id and eo.workspace_id=e.workspace_id`

func scanEvent(row interface{ Scan(...any) error }) (domain.Event, error) {
	var out domain.Event
	var eventID, workspace, author, updatedBy pgtype.UUID
	var precision string
	var raw []byte
	if err := row.Scan(&eventID, &workspace, &out.Title, &out.Description, &out.ReportedTime, &precision, &out.SortDate, &out.Location, &author, &updatedBy, &out.CreatedAt, &out.UpdatedAt, &raw); err != nil {
		return domain.Event{}, err
	}
	parsed, err := domain.ParseTimePrecision(precision)
	if err != nil {
		return domain.Event{}, err
	}
	var observationIDs []string
	if err := json.Unmarshal(raw, &observationIDs); err != nil {
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
	return out, nil
}

func (s *Store) Create(ctx context.Context, in domain.Event) error {
	_, err := s.db.DB(ctx).Exec(ctx, `insert into timeline.event(id,workspace_id,title,description,reported_time,time_precision,sort_date,location,author,updated_by,created_at,updated_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, uuid(in.ID), uuid(in.WorkspaceID), in.Title, in.Description, in.ReportedTime, in.TimePrecision.String(), in.SortDate, in.Location, uuid(in.Author), uuid(in.UpdatedBy), in.CreatedAt, in.UpdatedAt)
	return translate(ctx, err)
}

func (s *Store) ByID(ctx context.Context, workspace, want id.ID) (domain.Event, error) {
	return scanEvent(s.db.DB(ctx).QueryRow(ctx, eventSelect+` where e.workspace_id=$1 and e.id=$2 group by e.id,e.workspace_id,e.title,e.description,e.reported_time,e.time_precision,e.sort_date,e.location,e.author,e.updated_by,e.created_at,e.updated_at`, uuid(workspace), uuid(want)))
}

func (s *Store) Save(ctx context.Context, in domain.Event) error {
	tag, err := s.db.DB(ctx).Exec(ctx, `update timeline.event set title=$3,description=$4,reported_time=$5,time_precision=$6,sort_date=$7,location=$8,updated_by=$9,updated_at=$10 where workspace_id=$1 and id=$2`, uuid(in.WorkspaceID), uuid(in.ID), in.Title, in.Description, in.ReportedTime, in.TimePrecision.String(), in.SortDate, in.Location, uuid(in.UpdatedBy), in.UpdatedAt)
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

func (s *Store) Page(ctx context.Context, workspace, before id.ID, limit int) ([]domain.Event, error) {
	rows, err := s.db.DB(ctx).Query(ctx, eventSelect+` where e.workspace_id=$1 and ($2::uuid is null or e.id < $2) group by e.id,e.workspace_id,e.title,e.description,e.reported_time,e.time_precision,e.sort_date,e.location,e.author,e.updated_by,e.created_at,e.updated_at order by e.id desc limit $3`, uuid(workspace), uuid(before), limit)
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
