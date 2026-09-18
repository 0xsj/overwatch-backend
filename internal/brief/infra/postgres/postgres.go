package postgres

import (
	"context"
	"embed"
	"encoding/json"
	stderrors "errors"

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
       coalesce((select json_agg(bq.question_id order by bq.question_id) from brief.working_question bq where bq.workspace_id=b.workspace_id and bq.brief_id=b.id), '[]'::json)::text,
       coalesce((select json_agg(bc.connection_id order by bc.connection_id) from brief.working_connection bc where bc.workspace_id=b.workspace_id and bc.brief_id=b.id), '[]'::json)::text
from brief.working b`

const snapshotSelect = `
select s.id,s.workspace_id,s.brief_id,s.title,s.question,s.current_account,s.alternatives,s.limitations,s.next_steps,
       s.author,s.updated_by,s.frozen_by,s.source_updated_at,s.frozen_at,
       coalesce((select json_agg(so.observation_id order by so.observation_id) from brief.snapshot_observation so where so.workspace_id=s.workspace_id and so.snapshot_id=s.id), '[]'::json)::text,
       coalesce((select json_agg(json_build_object('question_id',sq.question_id,'question',sq.question,'state',sq.state,'resolution',sq.resolution,'observation_ids',sq.observation_ids) order by sq.question_id) from brief.snapshot_question sq where sq.workspace_id=s.workspace_id and sq.snapshot_id=s.id), '[]'::json)::text,
       coalesce((select json_agg(json_build_object('connection_id',sc.connection_id,'from_record_id',sc.from_record_id,'from_record_kind',sc.from_record_kind,'from_record_name',sc.from_record_name,'from_record_description',sc.from_record_description,'from_record_observation_ids',sc.from_record_observation_ids,'to_record_id',sc.to_record_id,'to_record_kind',sc.to_record_kind,'to_record_name',sc.to_record_name,'to_record_description',sc.to_record_description,'to_record_observation_ids',sc.to_record_observation_ids,'kind',sc.kind,'state',sc.state,'rationale',sc.rationale,'supporting_observation_ids',sc.supporting_observation_ids,'opposing_observation_ids',sc.opposing_observation_ids) order by sc.connection_id) from brief.snapshot_connection sc where sc.workspace_id=s.workspace_id and sc.snapshot_id=s.id), '[]'::json)::text
from brief.snapshot s`

func scanBrief(row interface{ Scan(...any) error }) (domain.Brief, error) {
	var out domain.Brief
	var briefID, workspace, author, updatedBy pgtype.UUID
	var observations, questions, connections []byte
	if err := row.Scan(&briefID, &workspace, &out.Title, &out.Question, &out.CurrentAccount, &out.Alternatives, &out.Limitations, &out.NextSteps, &author, &updatedBy, &out.CreatedAt, &out.UpdatedAt, &observations, &questions, &connections); err != nil {
		return domain.Brief{}, err
	}
	var observationIDs, questionIDs []string
	if err := json.Unmarshal(observations, &observationIDs); err != nil {
		return domain.Brief{}, err
	}
	if err := json.Unmarshal(questions, &questionIDs); err != nil {
		return domain.Brief{}, err
	}
	var connectionIDs []string
	if err := json.Unmarshal(connections, &connectionIDs); err != nil {
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
	parsedConnections, err := parseIDs(connectionIDs)
	if err != nil {
		return domain.Brief{}, err
	}
	out.ID, out.WorkspaceID = id.ID(briefID.Bytes), id.ID(workspace.Bytes)
	out.Author, out.UpdatedBy = id.ID(author.Bytes), id.ID(updatedBy.Bytes)
	out.ObservationIDs, out.QuestionIDs, out.ConnectionIDs = parsedObservations, parsedQuestions, parsedConnections
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

func scanSnapshot(row interface{ Scan(...any) error }) (domain.Snapshot, error) {
	var out domain.Snapshot
	var snapshotID, workspace, briefID, author, updatedBy, frozenBy pgtype.UUID
	var observations, questions, connections []byte
	if err := row.Scan(&snapshotID, &workspace, &briefID, &out.Title, &out.Question, &out.CurrentAccount, &out.Alternatives, &out.Limitations, &out.NextSteps, &author, &updatedBy, &frozenBy, &out.SourceUpdatedAt, &out.FrozenAt, &observations, &questions, &connections); err != nil {
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
	out.ID, out.WorkspaceID, out.BriefID = id.ID(snapshotID.Bytes), id.ID(workspace.Bytes), id.ID(briefID.Bytes)
	out.Author, out.UpdatedBy, out.FrozenBy = id.ID(author.Bytes), id.ID(updatedBy.Bytes), id.ID(frozenBy.Bytes)
	out.ObservationIDs, out.Questions, out.Connections = parsedObservations, parsedQuestions, parsedConnections
	return out, nil
}

func (s *Store) ByWorkspace(ctx context.Context, workspace id.ID) (domain.Brief, error) {
	out, err := scanBrief(s.db.DB(ctx).QueryRow(ctx, briefSelect+` where b.workspace_id=$1`, uuid(workspace)))
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
			group by c.id,c.from_record_id,fr.kind,fr.name,fr.description,c.to_record_id,tr.kind,tr.name,tr.description,c.kind,c.state,c.rationale`, uuid(workspace), uuid(connection)).Scan(
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
