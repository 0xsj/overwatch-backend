package postgres

import (
	"context"
	"embed"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/review/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const Schema = "review"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("review: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("review: NewStore with a nil pool")
	}
	return &Store{db: db}
}

func uuid(i id.ID) pgtype.UUID { return pgtype.UUID{Bytes: i, Valid: !i.IsZero()} }

func translate(ctx context.Context, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return postgres.Translate(ctx, err, "review")
}

func (s *Store) Upsert(ctx context.Context, in domain.Relation) (domain.Relation, error) {
	row := s.db.DB(ctx).QueryRow(ctx, `
insert into review.relation
 (id, workspace_id, left_observation_id, right_observation_id, kind, rationale, author, created_at, updated_at)
select $1,$2,$3,$4,$5,$6,$7,$8,$9
where exists (select 1 from observation.manual where id=$3 and workspace_id=$2)
  and exists (select 1 from observation.manual where id=$4 and workspace_id=$2)
on conflict (workspace_id, left_observation_id, right_observation_id)
do update set kind=excluded.kind, rationale=excluded.rationale,
              author=excluded.author, updated_at=excluded.updated_at
	returning id,workspace_id,left_observation_id,right_observation_id,kind,rationale,author,created_at,updated_at`,
		uuid(in.ID), uuid(in.WorkspaceID), uuid(in.LeftObservationID), uuid(in.RightObservationID), in.Kind.String(), in.Rationale, uuid(in.Author), in.CreatedAt, in.UpdatedAt)
	out, err := scanRelation(row)
	if err != nil {
		return domain.Relation{}, translate(ctx, err)
	}
	return out, nil
}

type scanner interface{ Scan(...any) error }

func scanRelation(row scanner) (domain.Relation, error) {
	var out domain.Relation
	var relation, workspace, left, right, author pgtype.UUID
	if err := row.Scan(&relation, &workspace, &left, &right, &out.Kind, &out.Rationale, &author, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return domain.Relation{}, err
	}
	out.ID, out.WorkspaceID = id.ID(relation.Bytes), id.ID(workspace.Bytes)
	out.LeftObservationID, out.RightObservationID, out.Author = id.ID(left.Bytes), id.ID(right.Bytes), id.ID(author.Bytes)
	return out, nil
}

func (s *Store) PageRelations(ctx context.Context, workspace, before id.ID, limit int) ([]domain.Relation, error) {
	rows, err := s.db.DB(ctx).Query(ctx, `
select id,workspace_id,left_observation_id,right_observation_id,kind,rationale,author,created_at,updated_at
from review.relation
where workspace_id=$1 and ($2::uuid is null or id < $2)
order by id desc limit $3`, uuid(workspace), uuid(before), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Relation, 0)
	for rows.Next() {
		one, err := scanRelation(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}

func (s *Store) PageEvidence(ctx context.Context, workspace, before id.ID, limit int) ([]domain.Evidence, error) {
	rows, err := s.db.DB(ctx).Query(ctx, `
select m.id,m.workspace_id,m.source_id,s.title,m.capture_id,m.statement,m.quote,
       m.extraction_id,m.quote_start,m.quote_end,m.locator,m.author,m.recorded_at
from observation.manual m
join source.source s on s.workspace_id=m.workspace_id and s.id=m.source_id
where m.workspace_id=$1 and ($2::uuid is null or m.id < $2)
order by m.id desc limit $3`, uuid(workspace), uuid(before), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Evidence, 0)
	for rows.Next() {
		one, err := scanEvidence(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}

func (s *Store) EvidenceByID(ctx context.Context, workspace, want id.ID) (domain.Evidence, error) {
	row := s.db.DB(ctx).QueryRow(ctx, `
select m.id,m.workspace_id,m.source_id,s.title,m.capture_id,m.statement,m.quote,
       m.extraction_id,m.quote_start,m.quote_end,m.locator,m.author,m.recorded_at
from observation.manual m
join source.source s on s.workspace_id=m.workspace_id and s.id=m.source_id
where m.workspace_id=$1 and m.id=$2`, uuid(workspace), uuid(want))
	one, err := scanEvidence(row)
	if err != nil {
		return domain.Evidence{}, translate(ctx, err)
	}
	return one, nil
}

func scanEvidence(row scanner) (domain.Evidence, error) {
	var one domain.Evidence
	var observation, space, source, capture, extraction, author pgtype.UUID
	var quote []byte
	if err := row.Scan(&observation, &space, &source, &one.SourceTitle, &capture, &one.Statement, &quote, &extraction, &one.QuoteStart, &one.QuoteEnd, &one.Locator, &author, &one.RecordedAt); err != nil {
		return domain.Evidence{}, err
	}
	one.ID, one.WorkspaceID, one.SourceID, one.CaptureID, one.Author = id.ID(observation.Bytes), id.ID(space.Bytes), id.ID(source.Bytes), id.ID(capture.Bytes), id.ID(author.Bytes)
	if extraction.Valid {
		value := id.ID(extraction.Bytes)
		one.ExtractionID = &value
	}
	one.Quote = string(quote)
	return one, nil
}
