package postgres

import (
	"context"
	"embed"
	"encoding/json"
	stderrors "errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/lead/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const Schema = "lead"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("lead: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("lead: NewStore with a nil pool")
	}
	return &Store{db: db}
}

func uuid(i id.ID) pgtype.UUID { return pgtype.UUID{Bytes: i, Valid: !i.IsZero()} }

func translate(ctx context.Context, err error) error {
	if stderrors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return postgres.Translate(ctx, err, "lead")
}

const questionSelect = `
select q.id,q.workspace_id,q.question,q.question_context,q.state,q.resolution,
       q.author,q.updated_by,q.created_at,q.updated_at,
       coalesce(json_agg(qo.observation_id order by qo.observation_id)
         filter (where qo.observation_id is not null), '[]'::json)::text
from lead.question q
left join lead.question_observation qo
  on qo.question_id=q.id and qo.workspace_id=q.workspace_id`

type scanner interface{ Scan(...any) error }

func scanQuestion(row scanner) (domain.Question, error) {
	var out domain.Question
	var question, workspace, author, updatedBy pgtype.UUID
	var state string
	var raw []byte
	if err := row.Scan(&question, &workspace, &out.Prompt, &out.Context, &state,
		&out.Resolution, &author, &updatedBy, &out.CreatedAt, &out.UpdatedAt, &raw); err != nil {
		return domain.Question{}, err
	}
	parsed, err := domain.ParseState(state)
	if err != nil {
		return domain.Question{}, err
	}
	var observationIDs []string
	if err := json.Unmarshal(raw, &observationIDs); err != nil {
		return domain.Question{}, err
	}
	out.ID, out.WorkspaceID = id.ID(question.Bytes), id.ID(workspace.Bytes)
	out.Author, out.UpdatedBy = id.ID(author.Bytes), id.ID(updatedBy.Bytes)
	out.State, out.ObservationIDs = parsed, make([]id.ID, 0, len(observationIDs))
	for _, rawID := range observationIDs {
		one, err := id.Parse(rawID)
		if err != nil {
			return domain.Question{}, err
		}
		out.ObservationIDs = append(out.ObservationIDs, one)
	}
	return out, nil
}

func (s *Store) Create(ctx context.Context, in domain.Question) error {
	_, err := s.db.DB(ctx).Exec(ctx, `
insert into lead.question
 (id,workspace_id,question,question_context,state,resolution,author,updated_by,created_at,updated_at)
values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		uuid(in.ID), uuid(in.WorkspaceID), in.Prompt, in.Context, in.State.String(),
		in.Resolution, uuid(in.Author), uuid(in.UpdatedBy), in.CreatedAt, in.UpdatedAt)
	return translate(ctx, err)
}

func (s *Store) ByID(ctx context.Context, workspace, want id.ID) (domain.Question, error) {
	row := s.db.DB(ctx).QueryRow(ctx, questionSelect+`
where q.workspace_id=$1 and q.id=$2
group by q.id,q.workspace_id,q.question,q.question_context,q.state,q.resolution,
         q.author,q.updated_by,q.created_at,q.updated_at`,
		uuid(workspace), uuid(want))
	out, err := scanQuestion(row)
	if err != nil {
		return domain.Question{}, translate(ctx, err)
	}
	return out, nil
}

func (s *Store) Save(ctx context.Context, in domain.Question) error {
	tag, err := s.db.DB(ctx).Exec(ctx, `
update lead.question
set question=$3,question_context=$4,state=$5,resolution=$6,
    updated_by=$7,updated_at=$8
where workspace_id=$1 and id=$2`,
		uuid(in.WorkspaceID), uuid(in.ID), in.Prompt, in.Context, in.State.String(),
		in.Resolution, uuid(in.UpdatedBy), in.UpdatedAt)
	if err != nil {
		return translate(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) ReplaceObservations(ctx context.Context, workspace, question id.ID, observations []id.ID) error {
	if _, err := s.db.DB(ctx).Exec(ctx,
		`delete from lead.question_observation where workspace_id=$1 and question_id=$2`,
		uuid(workspace), uuid(question)); err != nil {
		return translate(ctx, err)
	}
	for _, observation := range observations {
		tag, err := s.db.DB(ctx).Exec(ctx, `
insert into lead.question_observation (workspace_id,question_id,observation_id)
select $1,$2,$3
where exists (select 1 from lead.question where id=$2 and workspace_id=$1)
  and exists (select 1 from observation.manual where id=$3 and workspace_id=$1)`,
			uuid(workspace), uuid(question), uuid(observation))
		if err != nil {
			return translate(ctx, err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (s *Store) Page(ctx context.Context, workspace, before id.ID, limit int) ([]domain.Question, error) {
	rows, err := s.db.DB(ctx).Query(ctx, questionSelect+`
where q.workspace_id=$1 and ($2::uuid is null or q.id < $2)
group by q.id,q.workspace_id,q.question,q.question_context,q.state,q.resolution,
         q.author,q.updated_by,q.created_at,q.updated_at
order by q.id desc limit $3`, uuid(workspace), uuid(before), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Question, 0)
	for rows.Next() {
		one, err := scanQuestion(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}

func (s *Store) PageByState(ctx context.Context, workspace, before id.ID, state domain.State, limit int) ([]domain.Question, error) {
	rows, err := s.db.DB(ctx).Query(ctx, questionSelect+`
where q.workspace_id=$1 and ($2::uuid is null or q.id < $2) and q.state=$3
group by q.id,q.workspace_id,q.question,q.question_context,q.state,q.resolution,
         q.author,q.updated_by,q.created_at,q.updated_at
order by q.id desc limit $4`, uuid(workspace), uuid(before), state.String(), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Question, 0)
	for rows.Next() {
		one, err := scanQuestion(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}
