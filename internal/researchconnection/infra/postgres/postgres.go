package postgres

import (
	"context"
	"embed"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/researchconnection/domain"
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
		panic("researchconnection: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("researchconnection: NewStore with a nil pool")
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

const connectionSelect = "select c.id,c.workspace_id,c.from_record_id,c.to_record_id,c.kind,c.state,c.rationale," +
	"c.author,c.updated_by,c.created_at,c.updated_at," +
	"coalesce(json_agg(ce.observation_id order by ce.observation_id) filter (where ce.polarity='supporting'), '[]'::json)::text," +
	"coalesce(json_agg(ce.observation_id order by ce.observation_id) filter (where ce.polarity='opposing'), '[]'::json)::text " +
	"from research.connection c left join research.connection_evidence ce on ce.connection_id=c.id "

type scanner interface{ Scan(...any) error }

func scanConnection(row scanner) (domain.Connection, error) {
	var out domain.Connection
	var connection, workspace, from, to, author, updatedBy pgtype.UUID
	var kind, state string
	var supportingRaw, opposingRaw []byte
	if err := row.Scan(&connection, &workspace, &from, &to, &kind, &state, &out.Rationale,
		&author, &updatedBy, &out.CreatedAt, &out.UpdatedAt, &supportingRaw, &opposingRaw); err != nil {
		return domain.Connection{}, err
	}
	parsedKind, err := domain.ParseKind(kind)
	if err != nil {
		return domain.Connection{}, err
	}
	parsedState, err := domain.ParseState(state)
	if err != nil {
		return domain.Connection{}, err
	}
	var supporting, opposing []string
	if err := json.Unmarshal(supportingRaw, &supporting); err != nil {
		return domain.Connection{}, err
	}
	if err := json.Unmarshal(opposingRaw, &opposing); err != nil {
		return domain.Connection{}, err
	}
	out.ID, out.WorkspaceID, out.FromRecordID, out.ToRecordID = id.ID(connection.Bytes), id.ID(workspace.Bytes), id.ID(from.Bytes), id.ID(to.Bytes)
	out.Kind, out.State, out.Author, out.UpdatedBy = parsedKind, parsedState, id.ID(author.Bytes), id.ID(updatedBy.Bytes)
	out.SupportingObservationIDs, err = parseIDs(supporting)
	if err != nil {
		return domain.Connection{}, err
	}
	out.OpposingObservationIDs, err = parseIDs(opposing)
	if err != nil {
		return domain.Connection{}, err
	}
	return out, nil
}

func scanRevision(row scanner) (domain.Revision, error) {
	var out domain.Revision
	var revisionID, workspace, connection, from, to, changedBy pgtype.UUID
	var fromKind, fromName, fromDescription, toKind, toName, toDescription, kind, state string
	var fromObservationRaw, toObservationRaw, supportingRaw, opposingRaw []byte
	if err := row.Scan(&revisionID, &workspace, &connection, &out.Revision, &from, &fromKind, &fromName, &fromDescription, &fromObservationRaw, &to, &toKind, &toName, &toDescription, &toObservationRaw, &kind, &state, &out.Rationale, &supportingRaw, &opposingRaw, &changedBy, &out.ChangedAt); err != nil {
		return domain.Revision{}, err
	}
	parsedKind, err := domain.ParseKind(kind)
	if err != nil {
		return domain.Revision{}, err
	}
	parsedState, err := domain.ParseState(state)
	if err != nil {
		return domain.Revision{}, err
	}
	var fromObservations, toObservations, supporting, opposing []string
	if err := json.Unmarshal(fromObservationRaw, &fromObservations); err != nil {
		return domain.Revision{}, err
	}
	if err := json.Unmarshal(toObservationRaw, &toObservations); err != nil {
		return domain.Revision{}, err
	}
	if err := json.Unmarshal(supportingRaw, &supporting); err != nil {
		return domain.Revision{}, err
	}
	if err := json.Unmarshal(opposingRaw, &opposing); err != nil {
		return domain.Revision{}, err
	}
	parsedSupporting, err := parseIDs(supporting)
	if err != nil {
		return domain.Revision{}, err
	}
	parsedOpposing, err := parseIDs(opposing)
	if err != nil {
		return domain.Revision{}, err
	}
	parsedFromObservations, err := parseIDs(fromObservations)
	if err != nil {
		return domain.Revision{}, err
	}
	parsedToObservations, err := parseIDs(toObservations)
	if err != nil {
		return domain.Revision{}, err
	}
	out.ID, out.WorkspaceID, out.ConnectionID = id.ID(revisionID.Bytes), id.ID(workspace.Bytes), id.ID(connection.Bytes)
	out.FromRecordID, out.FromRecordKind, out.FromRecordName, out.FromRecordDescription = id.ID(from.Bytes), fromKind, fromName, fromDescription
	out.FromRecordObservationIDs = parsedFromObservations
	out.ToRecordID, out.ToRecordKind, out.ToRecordName, out.ToRecordDescription = id.ID(to.Bytes), toKind, toName, toDescription
	out.ToRecordObservationIDs = parsedToObservations
	out.Kind, out.State = parsedKind, parsedState
	out.SupportingObservationIDs, out.OpposingObservationIDs = parsedSupporting, parsedOpposing
	out.ChangedBy = id.ID(changedBy.Bytes)
	return out, nil
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

func (s *Store) Create(ctx context.Context, in domain.Connection) error {
	row := s.db.DB(ctx).QueryRow(ctx,
		"insert into research.connection (id,workspace_id,from_record_id,to_record_id,kind,state,rationale,author,updated_by,created_at,updated_at) "+
			"select $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11 "+
			"where exists (select 1 from research.record where id=$3 and workspace_id=$2) "+
			"and exists (select 1 from research.record where id=$4 and workspace_id=$2) returning id",
		uuid(in.ID), uuid(in.WorkspaceID), uuid(in.FromRecordID), uuid(in.ToRecordID), in.Kind.String(), in.State.String(), in.Rationale, uuid(in.Author), uuid(in.UpdatedBy), in.CreatedAt, in.UpdatedAt)
	var created pgtype.UUID
	if err := row.Scan(&created); err != nil {
		return translate(ctx, err)
	}
	return nil
}

func (s *Store) ByID(ctx context.Context, workspace, want id.ID) (domain.Connection, error) {
	row := s.db.DB(ctx).QueryRow(ctx, connectionSelect+
		"where c.workspace_id=$1 and c.id=$2 "+
		"group by c.id,c.workspace_id,c.from_record_id,c.to_record_id,c.kind,c.state,c.rationale,c.author,c.updated_by,c.created_at,c.updated_at",
		uuid(workspace), uuid(want))
	out, err := scanConnection(row)
	if err != nil {
		return domain.Connection{}, translate(ctx, err)
	}
	return out, nil
}

func (s *Store) Save(ctx context.Context, in domain.Connection) error {
	tag, err := s.db.DB(ctx).Exec(ctx,
		"update research.connection set kind=$3,state=$4,rationale=$5,updated_by=$6,updated_at=$7 where workspace_id=$1 and id=$2",
		uuid(in.WorkspaceID), uuid(in.ID), in.Kind.String(), in.State.String(), in.Rationale, uuid(in.UpdatedBy), in.UpdatedAt)
	if err != nil {
		return translate(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) ReplaceEvidence(ctx context.Context, workspace, connection id.ID, supporting, opposing []id.ID) error {
	if _, err := s.db.DB(ctx).Exec(ctx, "delete from research.connection_evidence where connection_id=$1", uuid(connection)); err != nil {
		return translate(ctx, err)
	}
	add := func(observation id.ID, polarity string) error {
		tag, err := s.db.DB(ctx).Exec(ctx,
			"insert into research.connection_evidence (connection_id,workspace_id,observation_id,polarity) "+
				"select $1,$2,$3,$4 where exists (select 1 from research.connection where id=$1 and workspace_id=$2) "+
				"and exists (select 1 from observation.manual where id=$3 and workspace_id=$2)",
			uuid(connection), uuid(workspace), uuid(observation), polarity)
		if err != nil {
			return translate(ctx, err)
		}
		if tag.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
		return nil
	}
	for _, observation := range supporting {
		if err := add(observation, "supporting"); err != nil {
			return err
		}
	}
	for _, observation := range opposing {
		if err := add(observation, "opposing"); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CreateRevision(ctx context.Context, in domain.Revision) error {
	supporting, err := json.Marshal(stringIDs(in.SupportingObservationIDs))
	if err != nil {
		return err
	}
	opposing, err := json.Marshal(stringIDs(in.OpposingObservationIDs))
	if err != nil {
		return err
	}
	_, err = s.db.DB(ctx).Exec(ctx, `
		insert into research.connection_revision(id,workspace_id,connection_id,revision,from_record_id,from_record_kind,from_record_name,from_record_description,from_record_observation_ids,to_record_id,to_record_kind,to_record_name,to_record_description,to_record_observation_ids,kind,state,rationale,supporting_observation_ids,opposing_observation_ids,changed_by,changed_at)
		select $1,$2,$3,coalesce((select max(revision)+1 from research.connection_revision where connection_id=$3),1),$4,fr.kind,fr.name,fr.description,coalesce((select json_agg(ro.observation_id order by ro.observation_id) from research.record_observation ro where ro.workspace_id=fr.workspace_id and ro.record_id=fr.id), '[]'::json)::jsonb,$5,tr.kind,tr.name,tr.description,coalesce((select json_agg(ro.observation_id order by ro.observation_id) from research.record_observation ro where ro.workspace_id=tr.workspace_id and ro.record_id=tr.id), '[]'::json)::jsonb,$6,$7,$8,$9::jsonb,$10::jsonb,$11,$12
		from research.connection c
		join research.record fr on fr.id=c.from_record_id and fr.workspace_id=c.workspace_id
		join research.record tr on tr.id=c.to_record_id and tr.workspace_id=c.workspace_id
		where c.id=$3 and c.workspace_id=$2`,
		uuid(in.ID), uuid(in.WorkspaceID), uuid(in.ConnectionID), uuid(in.FromRecordID), uuid(in.ToRecordID), in.Kind.String(), in.State.String(), in.Rationale,
		string(supporting), string(opposing), uuid(in.ChangedBy), in.ChangedAt)
	return translate(ctx, err)
}

func stringIDs(values []id.ID) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.String())
	}
	return out
}

func (s *Store) Revisions(ctx context.Context, workspace, connection id.ID) ([]domain.Revision, error) {
	rows, err := s.db.DB(ctx).Query(ctx, `
		select id,workspace_id,connection_id,revision,from_record_id,from_record_kind,from_record_name,from_record_description,from_record_observation_ids,to_record_id,to_record_kind,to_record_name,to_record_description,to_record_observation_ids,kind,state,rationale,supporting_observation_ids,opposing_observation_ids,changed_by,changed_at
		from research.connection_revision where workspace_id=$1 and connection_id=$2 order by revision asc`, uuid(workspace), uuid(connection))
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

func (s *Store) RevisionByID(ctx context.Context, workspace, connection, revision id.ID) (domain.Revision, error) {
	row := s.db.DB(ctx).QueryRow(ctx, `
		select id,workspace_id,connection_id,revision,from_record_id,from_record_kind,from_record_name,from_record_description,from_record_observation_ids,to_record_id,to_record_kind,to_record_name,to_record_description,to_record_observation_ids,kind,state,rationale,supporting_observation_ids,opposing_observation_ids,changed_by,changed_at
		from research.connection_revision where workspace_id=$1 and connection_id=$2 and id=$3`, uuid(workspace), uuid(connection), uuid(revision))
	out, err := scanRevision(row)
	if err != nil {
		return domain.Revision{}, translate(ctx, err)
	}
	return out, nil
}

func (s *Store) Page(ctx context.Context, workspace, before id.ID, limit int) ([]domain.Connection, error) {
	rows, err := s.db.DB(ctx).Query(ctx, connectionSelect+
		"where c.workspace_id=$1 and ($2::uuid is null or c.id < $2) "+
		"group by c.id,c.workspace_id,c.from_record_id,c.to_record_id,c.kind,c.state,c.rationale,c.author,c.updated_by,c.created_at,c.updated_at "+
		"order by c.id desc limit $3",
		uuid(workspace), uuid(before), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Connection, 0)
	for rows.Next() {
		one, err := scanConnection(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}
