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
from research.record r
left join research.record_observation ro
  on ro.record_id=r.id and ro.workspace_id=r.workspace_id`

type scanner interface{ Scan(...any) error }

func scanRecord(row scanner) (domain.Record, error) {
	var out domain.Record
	var recordID, workspace, author, updatedBy pgtype.UUID
	var kind string
	var raw []byte
	if err := row.Scan(&recordID, &workspace, &kind, &out.Name, &out.Description,
		&author, &updatedBy, &out.CreatedAt, &out.UpdatedAt, &raw); err != nil {
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

func (s *Store) Page(ctx context.Context, workspace, before id.ID, limit int) ([]domain.Record, error) {
	rows, err := s.db.DB(ctx).Query(ctx, recordSelect+`
where r.workspace_id=$1 and ($2::uuid is null or r.id < $2)
group by r.id,r.workspace_id,r.kind,r.name,r.description,r.author,r.updated_by,r.created_at,r.updated_at
order by r.id desc limit $3`, uuid(workspace), uuid(before), limit)
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
