package postgres

import (
	"context"
	"embed"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/researchentity/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const Schema = "research"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("researchentity: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("researchentity: NewStore with a nil pool")
	}
	return &Store{db: db}
}

func uuid(i id.ID) pgtype.UUID { return pgtype.UUID{Bytes: i, Valid: !i.IsZero()} }

func stringIDs(values []id.ID) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.String())
	}
	return out
}

func translate(ctx context.Context, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return postgres.Translate(ctx, err, "research")
}

const recordSelect = `
select r.id,r.workspace_id,r.kind,r.name,r.description,
       r.author,r.updated_by,r.created_at,r.updated_at,
       coalesce(json_agg(ro.observation_id order by ro.observation_id)
         filter (where ro.observation_id is not null), '[]'::json)::text
       ,coalesce((select json_build_object('latitude',g.latitude,'longitude',g.longitude,'precision',g.precision,'observation_ids',g.observation_ids)::text
          from research.record_place_geometry g where g.record_id=r.id and g.workspace_id=r.workspace_id), 'null')
from research.record r
left join research.record_observation ro
  on ro.record_id=r.id and ro.workspace_id=r.workspace_id`

type scanner interface{ Scan(...any) error }

func scanRecord(row scanner) (domain.Record, error) {
	var out domain.Record
	var recordID, workspace, author, updatedBy pgtype.UUID
	var kind string
	var raw, geometryRaw []byte
	if err := row.Scan(&recordID, &workspace, &kind, &out.Name, &out.Description,
		&author, &updatedBy, &out.CreatedAt, &out.UpdatedAt, &raw, &geometryRaw); err != nil {
		return domain.Record{}, err
	}
	parsed, err := domain.ParseKind(kind)
	if err != nil {
		return domain.Record{}, err
	}
	var observationIDs []string
	if err := json.Unmarshal(raw, &observationIDs); err != nil {
		return domain.Record{}, err
	}
	out.ID, out.WorkspaceID = id.ID(recordID.Bytes), id.ID(workspace.Bytes)
	out.Kind, out.Author, out.UpdatedBy = parsed, id.ID(author.Bytes), id.ID(updatedBy.Bytes)
	out.ObservationIDs = make([]id.ID, 0, len(observationIDs))
	for _, rawID := range observationIDs {
		one, err := id.Parse(rawID)
		if err != nil {
			return domain.Record{}, err
		}
		out.ObservationIDs = append(out.ObservationIDs, one)
	}
	if len(geometryRaw) > 0 && string(geometryRaw) != "null" {
		var stored struct {
			Latitude       float64  `json:"latitude"`
			Longitude      float64  `json:"longitude"`
			Precision      string   `json:"precision"`
			ObservationIDs []string `json:"observation_ids"`
		}
		if err := json.Unmarshal(geometryRaw, &stored); err != nil {
			return domain.Record{}, err
		}
		precision, err := domain.ParsePlacePrecision(stored.Precision)
		if err != nil {
			return domain.Record{}, err
		}
		place := &domain.PlaceGeometry{Latitude: stored.Latitude, Longitude: stored.Longitude, Precision: precision, ObservationIDs: make([]id.ID, 0, len(stored.ObservationIDs))}
		for _, rawID := range stored.ObservationIDs {
			one, err := id.Parse(rawID)
			if err != nil {
				return domain.Record{}, err
			}
			place.ObservationIDs = append(place.ObservationIDs, one)
		}
		out.PlaceGeometry = place
	}
	return out, nil
}

func (s *Store) Create(ctx context.Context, in domain.Record) error {
	_, err := s.db.DB(ctx).Exec(ctx, `
insert into research.record
 (id,workspace_id,kind,name,description,author,updated_by,created_at,updated_at)
values ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, uuid(in.ID), uuid(in.WorkspaceID), in.Kind.String(), in.Name, in.Description,
		uuid(in.Author), uuid(in.UpdatedBy), in.CreatedAt, in.UpdatedAt)
	return translate(ctx, err)
}

func (s *Store) ByID(ctx context.Context, workspace, want id.ID) (domain.Record, error) {
	row := s.db.DB(ctx).QueryRow(ctx, recordSelect+`
where r.workspace_id=$1 and r.id=$2
group by r.id,r.workspace_id,r.kind,r.name,r.description,r.author,r.updated_by,r.created_at,r.updated_at`, uuid(workspace), uuid(want))
	out, err := scanRecord(row)
	if err != nil {
		return domain.Record{}, translate(ctx, err)
	}
	return out, nil
}

func (s *Store) Save(ctx context.Context, in domain.Record) error {
	tag, err := s.db.DB(ctx).Exec(ctx, `
update research.record
set kind=$3,name=$4,description=$5,updated_by=$6,updated_at=$7
where workspace_id=$1 and id=$2`, uuid(in.WorkspaceID), uuid(in.ID), in.Kind.String(), in.Name, in.Description,
		uuid(in.UpdatedBy), in.UpdatedAt)
	if err != nil {
		return translate(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) ReplaceObservations(ctx context.Context, workspace, record id.ID, observations []id.ID) error {
	if _, err := s.db.DB(ctx).Exec(ctx, `delete from research.record_observation where workspace_id=$1 and record_id=$2`, uuid(workspace), uuid(record)); err != nil {
		return translate(ctx, err)
	}
	for _, observation := range observations {
		tag, err := s.db.DB(ctx).Exec(ctx, `
insert into research.record_observation (workspace_id,record_id,observation_id)
select $1,$2,$3
where exists (select 1 from research.record where id=$2 and workspace_id=$1)
  and exists (select 1 from observation.manual where id=$3 and workspace_id=$1)`, uuid(workspace), uuid(record), uuid(observation))
		if err != nil {
			return translate(ctx, err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (s *Store) ReplacePlaceGeometry(ctx context.Context, record domain.Record) error {
	if _, err := s.db.DB(ctx).Exec(ctx, `delete from research.record_place_geometry where workspace_id=$1 and record_id=$2`, uuid(record.WorkspaceID), uuid(record.ID)); err != nil {
		return translate(ctx, err)
	}
	if record.PlaceGeometry == nil {
		return nil
	}
	observationIDs, err := json.Marshal(stringIDs(record.PlaceGeometry.ObservationIDs))
	if err != nil {
		return err
	}
	_, err = s.db.DB(ctx).Exec(ctx, `
insert into research.record_place_geometry
 (workspace_id,record_id,latitude,longitude,precision,observation_ids,updated_by,updated_at)
values ($1,$2,$3,$4,$5,$6::jsonb,$7,$8)`, uuid(record.WorkspaceID), uuid(record.ID), record.PlaceGeometry.Latitude, record.PlaceGeometry.Longitude, record.PlaceGeometry.Precision.String(), string(observationIDs), uuid(record.UpdatedBy), record.UpdatedAt)
	return translate(ctx, err)
}

func (s *Store) Page(ctx context.Context, workspace, before id.ID, search string, kind domain.Kind, citation domain.CitationFilter, resolution domain.ResolutionFilter, limit int) ([]domain.Record, error) {
	rows, err := s.db.DB(ctx).Query(ctx, recordSelect+`
where r.workspace_id=$1 and ($2::uuid is null or r.id < $2)
  and ($3 = '' or position(lower($3) in lower(r.id::text)) > 0
    or position(lower($3) in lower(r.name)) > 0
    or position(lower($3) in lower(coalesce(r.description, ''))) > 0
    or exists (select 1
       from research.record_observation searched_ro
       join observation.manual searched_o
         on searched_o.id=searched_ro.observation_id
        and searched_o.workspace_id=searched_ro.workspace_id
       where searched_ro.record_id=r.id
         and searched_ro.workspace_id=r.workspace_id
         and (position(lower($3) in lower(searched_o.statement)) > 0
           or position(lower($3) in lower(convert_from(searched_o.quote, 'UTF8'))) > 0
           or position(lower($3) in lower(coalesce(searched_o.locator, ''))) > 0))
  and ($4 = '' or r.kind = $4)
  and ($5 = '' or ($5 = 'cited' and exists (
       select 1 from research.record_observation citation_ro
       where citation_ro.record_id=r.id and citation_ro.workspace_id=r.workspace_id))
    or ($5 = 'uncited' and not exists (
       select 1 from research.record_observation citation_ro
       where citation_ro.record_id=r.id and citation_ro.workspace_id=r.workspace_id)))
  and ($6 = '' or ($6 = 'open' and exists (
       select 1 from research.record_resolution open_rr
       where open_rr.workspace_id=r.workspace_id
         and (open_rr.alias_record_id=r.id or open_rr.canonical_record_id=r.id)
         and open_rr.state='proposed'))
    or ($6 = 'accepted' and exists (
       select 1 from research.record_resolution accepted_rr
       where accepted_rr.workspace_id=r.workspace_id
         and (accepted_rr.alias_record_id=r.id or accepted_rr.canonical_record_id=r.id)
         and accepted_rr.state='accepted'))
    or ($6 = 'none' and not exists (
       select 1 from research.record_resolution active_rr
       where active_rr.workspace_id=r.workspace_id
         and (active_rr.alias_record_id=r.id or active_rr.canonical_record_id=r.id)
         and active_rr.state in ('proposed','accepted'))))
group by r.id,r.workspace_id,r.kind,r.name,r.description,r.author,r.updated_by,r.created_at,r.updated_at
order by r.id desc limit $7`, uuid(workspace), uuid(before), search, kind.String(), citation.String(), resolution.String(), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Record, 0)
	for rows.Next() {
		one, err := scanRecord(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}

func (s *Store) Summary(ctx context.Context, workspace id.ID) (domain.BrowseSummary, error) {
	var out domain.BrowseSummary
	var total, people, accounts, organisations, places, cited, uncited, citations, openResolutions, acceptedResolutions int64
	err := s.db.DB(ctx).QueryRow(ctx, `
select count(*)::bigint,
       count(*) filter (where r.kind='person')::bigint,
       count(*) filter (where r.kind='account')::bigint,
       count(*) filter (where r.kind='organisation')::bigint,
       count(*) filter (where r.kind='place')::bigint,
       count(distinct r.id) filter (where ro.observation_id is not null)::bigint,
       count(distinct r.id) filter (where ro.observation_id is null)::bigint,
       count(ro.observation_id)::bigint,
       count(distinct r.id) filter (where exists (
         select 1 from research.record_resolution rr
         where rr.workspace_id=r.workspace_id
           and (rr.alias_record_id=r.id or rr.canonical_record_id=r.id)
           and rr.state='proposed'))::bigint,
       count(distinct r.id) filter (where exists (
         select 1 from research.record_resolution rr
         where rr.workspace_id=r.workspace_id
           and (rr.alias_record_id=r.id or rr.canonical_record_id=r.id)
           and rr.state='accepted'))::bigint
from research.record r
left join research.record_observation ro
  on ro.record_id=r.id and ro.workspace_id=r.workspace_id
where r.workspace_id=$1`, uuid(workspace)).Scan(&total, &people, &accounts, &organisations, &places, &cited, &uncited, &citations, &openResolutions, &acceptedResolutions)
	if err != nil {
		return domain.BrowseSummary{}, translate(ctx, err)
	}
	out.RecordCount = int(total)
	out.KindCounts = map[domain.Kind]int{
		domain.Person: int(people), domain.Account: int(accounts),
		domain.Organisation: int(organisations), domain.Place: int(places),
	}
	out.CitedRecordCount, out.UncitedRecordCount, out.CitationCount = int(cited), int(uncited), int(citations)
	out.OpenResolutionRecordCount, out.AcceptedResolutionRecordCount = int(openResolutions), int(acceptedResolutions)
	return out, nil
}
