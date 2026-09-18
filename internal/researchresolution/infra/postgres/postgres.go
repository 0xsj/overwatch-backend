package postgres

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/researchresolution/domain"
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
		panic("researchresolution: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("researchresolution: NewStore with a nil pool")
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

const resolutionSelect = `
select id,workspace_id,alias_record_id,canonical_record_id,state,rationale,proposed_by,proposed_at,
       reviewed_by,reviewed_at,reversed_by,reversed_at,canonical_observation_ids_before,added_observation_ids
from research.record_resolution`

type scanner interface{ Scan(...any) error }

func scanResolution(row scanner) (domain.Resolution, error) {
	var out domain.Resolution
	var resolutionID, workspace, alias, canonical, proposedBy, reviewedBy, reversedBy pgtype.UUID
	var state string
	var reviewedAt, reversedAt *time.Time
	var beforeRaw, addedRaw []byte
	if err := row.Scan(&resolutionID, &workspace, &alias, &canonical, &state, &out.Rationale, &proposedBy, &out.ProposedAt, &reviewedBy, &reviewedAt, &reversedBy, &reversedAt, &beforeRaw, &addedRaw); err != nil {
		return domain.Resolution{}, err
	}
	parsed, err := domain.ParseState(state)
	if err != nil {
		return domain.Resolution{}, err
	}
	before, err := parseIDs(beforeRaw)
	if err != nil {
		return domain.Resolution{}, err
	}
	added, err := parseIDs(addedRaw)
	if err != nil {
		return domain.Resolution{}, err
	}
	out.ID, out.WorkspaceID = id.ID(resolutionID.Bytes), id.ID(workspace.Bytes)
	out.AliasRecordID, out.CanonicalRecordID = id.ID(alias.Bytes), id.ID(canonical.Bytes)
	out.State, out.ProposedBy, out.ReviewedBy, out.ReviewedAt = parsed, id.ID(proposedBy.Bytes), id.ID(reviewedBy.Bytes), reviewedAt
	out.ReversedBy, out.ReversedAt = id.ID(reversedBy.Bytes), reversedAt
	out.CanonicalObservationIDsBefore, out.AddedObservationIDs = before, added
	return out, nil
}

func parseIDs(raw []byte) ([]id.ID, error) {
	if len(raw) == 0 {
		return []id.ID{}, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	out := make([]id.ID, 0, len(values))
	for _, value := range values {
		parsed, err := id.Parse(value)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	return out, nil
}

func stringIDs(values []id.ID) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.String())
	}
	return out
}

func (s *Store) Create(ctx context.Context, in domain.Resolution) error {
	before, err := json.Marshal(stringIDs(in.CanonicalObservationIDsBefore))
	if err != nil {
		return err
	}
	added, err := json.Marshal(stringIDs(in.AddedObservationIDs))
	if err != nil {
		return err
	}
	_, err = s.db.DB(ctx).Exec(ctx, `insert into research.record_resolution
	(id,workspace_id,alias_record_id,canonical_record_id,state,rationale,proposed_by,proposed_at,reviewed_by,reviewed_at,reversed_by,reversed_at,canonical_observation_ids_before,added_observation_ids)
	values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb,$14::jsonb)`, uuid(in.ID), uuid(in.WorkspaceID), uuid(in.AliasRecordID), uuid(in.CanonicalRecordID), in.State.String(), in.Rationale, uuid(in.ProposedBy), in.ProposedAt, uuid(in.ReviewedBy), in.ReviewedAt, uuid(in.ReversedBy), in.ReversedAt, before, added)
	return translate(ctx, err)
}

func (s *Store) ByID(ctx context.Context, workspace, want id.ID) (domain.Resolution, error) {
	out, err := scanResolution(s.db.DB(ctx).QueryRow(ctx, resolutionSelect+` where workspace_id=$1 and id=$2`, uuid(workspace), uuid(want)))
	return out, translate(ctx, err)
}

func (s *Store) Save(ctx context.Context, in domain.Resolution) error {
	before, err := json.Marshal(stringIDs(in.CanonicalObservationIDsBefore))
	if err != nil {
		return err
	}
	added, err := json.Marshal(stringIDs(in.AddedObservationIDs))
	if err != nil {
		return err
	}
	tag, err := s.db.DB(ctx).Exec(ctx, `update research.record_resolution set state=$3,reviewed_by=$4,reviewed_at=$5,reversed_by=$6,reversed_at=$7,canonical_observation_ids_before=$8::jsonb,added_observation_ids=$9::jsonb where workspace_id=$1 and id=$2`, uuid(in.WorkspaceID), uuid(in.ID), in.State.String(), uuid(in.ReviewedBy), in.ReviewedAt, uuid(in.ReversedBy), in.ReversedAt, before, added)
	if err != nil {
		return translate(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) ActiveByAlias(ctx context.Context, workspace, alias id.ID) (domain.Resolution, error) {
	out, err := scanResolution(s.db.DB(ctx).QueryRow(ctx, resolutionSelect+` where workspace_id=$1 and alias_record_id=$2 and state in ('proposed','accepted') order by id desc limit 1`, uuid(workspace), uuid(alias)))
	return out, translate(ctx, err)
}

func (s *Store) Page(ctx context.Context, workspace, before id.ID, limit int) ([]domain.Resolution, error) {
	rows, err := s.db.DB(ctx).Query(ctx, resolutionSelect+` where workspace_id=$1 and ($2::uuid is null or id < $2) order by id desc limit $3`, uuid(workspace), uuid(before), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Resolution, 0)
	for rows.Next() {
		one, err := scanResolution(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}
