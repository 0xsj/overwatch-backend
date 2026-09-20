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

const resolutionSetSelect = `
select s.id,s.workspace_id,s.canonical_record_id,s.state,s.rationale,s.proposed_by,s.proposed_at,
       s.reviewed_by,s.reviewed_at,s.reversed_by,s.reversed_at,s.canonical_observation_ids_before,s.added_observation_ids,
       coalesce(array_agg(m.record_id::text order by m.record_id) filter (where m.role='alias'), '{}')
from research.record_resolution_set s
left join research.record_resolution_set_member m on m.resolution_set_id=s.id and m.workspace_id=s.workspace_id`

func scanResolutionSet(row scanner) (domain.ResolutionSet, error) {
	var out domain.ResolutionSet
	var setID, workspace, canonical, proposedBy, reviewedBy, reversedBy pgtype.UUID
	var state string
	var reviewedAt, reversedAt *time.Time
	var beforeRaw, addedRaw []byte
	var aliasRaw []string
	if err := row.Scan(&setID, &workspace, &canonical, &state, &out.Rationale, &proposedBy, &out.ProposedAt, &reviewedBy, &reviewedAt, &reversedBy, &reversedAt, &beforeRaw, &addedRaw, &aliasRaw); err != nil {
		return domain.ResolutionSet{}, err
	}
	parsed, err := domain.ParseState(state)
	if err != nil {
		return domain.ResolutionSet{}, err
	}
	before, err := parseIDs(beforeRaw)
	if err != nil {
		return domain.ResolutionSet{}, err
	}
	added, err := parseIDs(addedRaw)
	if err != nil {
		return domain.ResolutionSet{}, err
	}
	aliases := make([]id.ID, 0, len(aliasRaw))
	for _, one := range aliasRaw {
		parsedAlias, err := id.Parse(one)
		if err != nil {
			return domain.ResolutionSet{}, err
		}
		aliases = append(aliases, parsedAlias)
	}
	out.ID, out.WorkspaceID, out.CanonicalRecordID = id.ID(setID.Bytes), id.ID(workspace.Bytes), id.ID(canonical.Bytes)
	out.AliasRecordIDs, out.State, out.ProposedBy, out.ReviewedBy, out.ReviewedAt = aliases, parsed, id.ID(proposedBy.Bytes), id.ID(reviewedBy.Bytes), reviewedAt
	out.ReversedBy, out.ReversedAt = id.ID(reversedBy.Bytes), reversedAt
	out.CanonicalObservationIDsBefore, out.AddedObservationIDs = before, added
	return out, nil
}

func (s *Store) CreateSet(ctx context.Context, in domain.ResolutionSet) error {
	before, err := json.Marshal(stringIDs(in.CanonicalObservationIDsBefore))
	if err != nil {
		return err
	}
	added, err := json.Marshal(stringIDs(in.AddedObservationIDs))
	if err != nil {
		return err
	}
	if _, err := s.db.DB(ctx).Exec(ctx, `insert into research.record_resolution_set
		(id,workspace_id,canonical_record_id,state,rationale,proposed_by,proposed_at,reviewed_by,reviewed_at,reversed_by,reversed_at,canonical_observation_ids_before,added_observation_ids)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13::jsonb)`, uuid(in.ID), uuid(in.WorkspaceID), uuid(in.CanonicalRecordID), in.State.String(), in.Rationale, uuid(in.ProposedBy), in.ProposedAt, uuid(in.ReviewedBy), in.ReviewedAt, uuid(in.ReversedBy), in.ReversedAt, before, added); err != nil {
		return translate(ctx, err)
	}
	for _, alias := range in.AliasRecordIDs {
		if _, err := s.db.DB(ctx).Exec(ctx, `insert into research.record_resolution_set_member(resolution_set_id,workspace_id,record_id,role,active) values ($1,$2,$3,'alias',true)`, uuid(in.ID), uuid(in.WorkspaceID), uuid(alias)); err != nil {
			return translate(ctx, err)
		}
	}
	if _, err := s.db.DB(ctx).Exec(ctx, `insert into research.record_resolution_set_member(resolution_set_id,workspace_id,record_id,role,active) values ($1,$2,$3,'canonical',true)`, uuid(in.ID), uuid(in.WorkspaceID), uuid(in.CanonicalRecordID)); err != nil {
		return translate(ctx, err)
	}
	return nil
}

func (s *Store) BySetID(ctx context.Context, workspace, want id.ID) (domain.ResolutionSet, error) {
	out, err := scanResolutionSet(s.db.DB(ctx).QueryRow(ctx, resolutionSetSelect+` where s.workspace_id=$1 and s.id=$2 group by s.id`, uuid(workspace), uuid(want)))
	return out, translate(ctx, err)
}

func (s *Store) SaveSet(ctx context.Context, in domain.ResolutionSet) error {
	before, err := json.Marshal(stringIDs(in.CanonicalObservationIDsBefore))
	if err != nil {
		return err
	}
	added, err := json.Marshal(stringIDs(in.AddedObservationIDs))
	if err != nil {
		return err
	}
	tag, err := s.db.DB(ctx).Exec(ctx, `update research.record_resolution_set set state=$3,reviewed_by=$4,reviewed_at=$5,reversed_by=$6,reversed_at=$7,canonical_observation_ids_before=$8::jsonb,added_observation_ids=$9::jsonb where workspace_id=$1 and id=$2`, uuid(in.WorkspaceID), uuid(in.ID), in.State.String(), uuid(in.ReviewedBy), in.ReviewedAt, uuid(in.ReversedBy), in.ReversedAt, before, added)
	if err != nil {
		return translate(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	active := in.State == domain.Proposed || in.State == domain.Accepted
	if _, err := s.db.DB(ctx).Exec(ctx, `update research.record_resolution_set_member set active=$3 where workspace_id=$1 and resolution_set_id=$2`, uuid(in.WorkspaceID), uuid(in.ID), active); err != nil {
		return translate(ctx, err)
	}
	return nil
}

func (s *Store) ActiveSetByRecord(ctx context.Context, workspace, record id.ID) (domain.ResolutionSet, error) {
	out, err := scanResolutionSet(s.db.DB(ctx).QueryRow(ctx, resolutionSetSelect+` join research.record_resolution_set_member target on target.resolution_set_id=s.id and target.workspace_id=s.workspace_id where s.workspace_id=$1 and target.record_id=$2 and target.active group by s.id order by s.id desc limit 1`, uuid(workspace), uuid(record)))
	return out, translate(ctx, err)
}

func (s *Store) PageSets(ctx context.Context, workspace, before id.ID, limit int) ([]domain.ResolutionSet, error) {
	rows, err := s.db.DB(ctx).Query(ctx, resolutionSetSelect+` where s.workspace_id=$1 and ($2::uuid is null or s.id < $2) group by s.id order by s.id desc limit $3`, uuid(workspace), uuid(before), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.ResolutionSet, 0)
	for rows.Next() {
		one, err := scanResolutionSet(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}

func (s *Store) ImpactSet(ctx context.Context, workspace, resolutionSet id.ID) (domain.Impact, error) {
	if _, err := s.BySetID(ctx, workspace, resolutionSet); err != nil {
		return domain.Impact{}, err
	}
	impact := domain.Impact{Connections: []domain.ConnectionImpact{}, Events: []domain.EventImpact{}, Briefs: []domain.BriefImpact{}, Snapshots: []domain.SnapshotImpact{}}
	target := `select record_id from research.record_resolution_set_member where workspace_id=$1 and resolution_set_id=$2`

	connectionRows, err := s.db.DB(ctx).Query(ctx, `
		select c.id,c.from_record_id,coalesce(fr.name,''),c.to_record_id,coalesce(tr.name,''),c.kind,c.state
		from research.connection c
		left join research.record fr on fr.workspace_id=c.workspace_id and fr.id=c.from_record_id
		left join research.record tr on tr.workspace_id=c.workspace_id and tr.id=c.to_record_id
		where c.workspace_id=$1 and (c.from_record_id in (`+target+`) or c.to_record_id in (`+target+`))
		order by c.id desc`, uuid(workspace), uuid(resolutionSet))
	if err != nil {
		return domain.Impact{}, translate(ctx, err)
	}
	for connectionRows.Next() {
		var connectionID, fromRecordID, toRecordID pgtype.UUID
		var fromName, toName, kind, state string
		if err := connectionRows.Scan(&connectionID, &fromRecordID, &fromName, &toRecordID, &toName, &kind, &state); err != nil {
			connectionRows.Close()
			return domain.Impact{}, translate(ctx, err)
		}
		impact.Connections = append(impact.Connections, domain.ConnectionImpact{ID: id.ID(connectionID.Bytes), FromRecordID: id.ID(fromRecordID.Bytes), FromRecordName: fromName, ToRecordID: id.ID(toRecordID.Bytes), ToRecordName: toName, Kind: kind, State: state})
	}
	if err := connectionRows.Err(); err != nil {
		connectionRows.Close()
		return domain.Impact{}, translate(ctx, err)
	}
	connectionRows.Close()

	eventRows, err := s.db.DB(ctx).Query(ctx, `
		select distinct e.id,e.title,e.sort_date
		from timeline.event e
		left join timeline.event_participant ep on ep.workspace_id=e.workspace_id and ep.event_id=e.id
		where e.workspace_id=$1 and (e.location_record_id in (`+target+`) or ep.record_id in (`+target+`))
		order by e.id desc`, uuid(workspace), uuid(resolutionSet))
	if err != nil {
		return domain.Impact{}, translate(ctx, err)
	}
	for eventRows.Next() {
		var eventID pgtype.UUID
		var title, sortDate string
		if err := eventRows.Scan(&eventID, &title, &sortDate); err != nil {
			eventRows.Close()
			return domain.Impact{}, translate(ctx, err)
		}
		impact.Events = append(impact.Events, domain.EventImpact{ID: id.ID(eventID.Bytes), Title: title, SortDate: sortDate})
	}
	if err := eventRows.Err(); err != nil {
		eventRows.Close()
		return domain.Impact{}, translate(ctx, err)
	}
	eventRows.Close()

	briefRows, err := s.db.DB(ctx).Query(ctx, `
		select distinct b.id,b.title,b.updated_at
		from brief.working b
		left join brief.working_connection wc on wc.workspace_id=b.workspace_id and wc.brief_id=b.id
		left join research.connection c on c.workspace_id=b.workspace_id and c.id=wc.connection_id
		left join brief.working_event we on we.workspace_id=b.workspace_id and we.brief_id=b.id
		left join timeline.event e on e.workspace_id=b.workspace_id and e.id=we.event_id
		left join timeline.event_participant ep on ep.workspace_id=e.workspace_id and ep.event_id=e.id
		where b.workspace_id=$1 and (c.from_record_id in (`+target+`) or c.to_record_id in (`+target+`) or e.location_record_id in (`+target+`) or ep.record_id in (`+target+`))
		order by b.id desc`, uuid(workspace), uuid(resolutionSet))
	if err != nil {
		return domain.Impact{}, translate(ctx, err)
	}
	for briefRows.Next() {
		var briefID pgtype.UUID
		var title string
		var updatedAt time.Time
		if err := briefRows.Scan(&briefID, &title, &updatedAt); err != nil {
			briefRows.Close()
			return domain.Impact{}, translate(ctx, err)
		}
		impact.Briefs = append(impact.Briefs, domain.BriefImpact{ID: id.ID(briefID.Bytes), Title: title, UpdatedAt: updatedAt})
	}
	if err := briefRows.Err(); err != nil {
		briefRows.Close()
		return domain.Impact{}, translate(ctx, err)
	}
	briefRows.Close()

	snapshotRows, err := s.db.DB(ctx).Query(ctx, `
		select distinct s.id,s.brief_id,s.title,s.frozen_at
		from brief.snapshot s
		left join brief.snapshot_connection sc on sc.workspace_id=s.workspace_id and sc.snapshot_id=s.id
		left join brief.snapshot_event se on se.workspace_id=s.workspace_id and se.snapshot_id=s.id
		where s.workspace_id=$1 and (sc.from_record_id in (`+target+`) or sc.to_record_id in (`+target+`) or exists (select 1 from jsonb_array_elements(coalesce(se.participant_records,'[]'::jsonb)) participant where participant->>'record_id' in (select record_id::text from research.record_resolution_set_member where workspace_id=$1 and resolution_set_id=$2)) or se.location_record->>'record_id' in (select record_id::text from research.record_resolution_set_member where workspace_id=$1 and resolution_set_id=$2))
		order by s.id desc`, uuid(workspace), uuid(resolutionSet))
	if err != nil {
		return domain.Impact{}, translate(ctx, err)
	}
	for snapshotRows.Next() {
		var snapshotID, briefID pgtype.UUID
		var title string
		var frozenAt time.Time
		if err := snapshotRows.Scan(&snapshotID, &briefID, &title, &frozenAt); err != nil {
			snapshotRows.Close()
			return domain.Impact{}, translate(ctx, err)
		}
		impact.Snapshots = append(impact.Snapshots, domain.SnapshotImpact{ID: id.ID(snapshotID.Bytes), BriefID: id.ID(briefID.Bytes), Title: title, FrozenAt: frozenAt})
	}
	if err := snapshotRows.Err(); err != nil {
		snapshotRows.Close()
		return domain.Impact{}, translate(ctx, err)
	}
	snapshotRows.Close()
	return impact, nil
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

func (s *Store) Impact(ctx context.Context, workspace, resolution id.ID) (domain.Impact, error) {
	if _, err := s.ByID(ctx, workspace, resolution); err != nil {
		return domain.Impact{}, err
	}
	impact := domain.Impact{
		Connections: []domain.ConnectionImpact{},
		Events:      []domain.EventImpact{},
		Briefs:      []domain.BriefImpact{},
		Snapshots:   []domain.SnapshotImpact{},
	}

	connectionRows, err := s.db.DB(ctx).Query(ctx, `
		with target as (
			select workspace_id, alias_record_id, canonical_record_id
			from research.record_resolution
			where workspace_id=$1 and id=$2
		)
		select c.id,c.from_record_id,coalesce(fr.name,''),c.to_record_id,coalesce(tr.name,''),c.kind,c.state
		from target t
		join research.connection c on c.workspace_id=t.workspace_id
		left join research.record fr on fr.workspace_id=c.workspace_id and fr.id=c.from_record_id
		left join research.record tr on tr.workspace_id=c.workspace_id and tr.id=c.to_record_id
		where c.from_record_id in (t.alias_record_id,t.canonical_record_id)
		   or c.to_record_id in (t.alias_record_id,t.canonical_record_id)
		order by c.id desc`, uuid(workspace), uuid(resolution))
	if err != nil {
		return domain.Impact{}, translate(ctx, err)
	}
	for connectionRows.Next() {
		var connectionID, fromRecordID, toRecordID pgtype.UUID
		var fromName, toName, kind, state string
		if err := connectionRows.Scan(&connectionID, &fromRecordID, &fromName, &toRecordID, &toName, &kind, &state); err != nil {
			connectionRows.Close()
			return domain.Impact{}, translate(ctx, err)
		}
		impact.Connections = append(impact.Connections, domain.ConnectionImpact{ID: id.ID(connectionID.Bytes), FromRecordID: id.ID(fromRecordID.Bytes), FromRecordName: fromName, ToRecordID: id.ID(toRecordID.Bytes), ToRecordName: toName, Kind: kind, State: state})
	}
	if err := connectionRows.Err(); err != nil {
		connectionRows.Close()
		return domain.Impact{}, translate(ctx, err)
	}
	connectionRows.Close()

	eventRows, err := s.db.DB(ctx).Query(ctx, `
		with target as (
			select workspace_id, alias_record_id, canonical_record_id
			from research.record_resolution
			where workspace_id=$1 and id=$2
		)
		select distinct e.id,e.title,e.sort_date
		from target t
		join timeline.event e on e.workspace_id=t.workspace_id
		left join timeline.event_participant ep on ep.workspace_id=e.workspace_id and ep.event_id=e.id
		where e.location_record_id in (t.alias_record_id,t.canonical_record_id)
		   or ep.record_id in (t.alias_record_id,t.canonical_record_id)
		order by e.id desc`, uuid(workspace), uuid(resolution))
	if err != nil {
		return domain.Impact{}, translate(ctx, err)
	}
	for eventRows.Next() {
		var eventID pgtype.UUID
		var title, sortDate string
		if err := eventRows.Scan(&eventID, &title, &sortDate); err != nil {
			eventRows.Close()
			return domain.Impact{}, translate(ctx, err)
		}
		impact.Events = append(impact.Events, domain.EventImpact{ID: id.ID(eventID.Bytes), Title: title, SortDate: sortDate})
	}
	if err := eventRows.Err(); err != nil {
		eventRows.Close()
		return domain.Impact{}, translate(ctx, err)
	}
	eventRows.Close()

	briefRows, err := s.db.DB(ctx).Query(ctx, `
		with target as (
			select workspace_id, alias_record_id, canonical_record_id
			from research.record_resolution
			where workspace_id=$1 and id=$2
		)
		select distinct b.id,b.title,b.updated_at
		from target t
		join brief.working b on b.workspace_id=t.workspace_id
		left join brief.working_connection wc on wc.workspace_id=b.workspace_id and wc.brief_id=b.id
		left join research.connection c on c.workspace_id=t.workspace_id and c.id=wc.connection_id
		left join brief.working_event we on we.workspace_id=b.workspace_id and we.brief_id=b.id
		left join timeline.event e on e.workspace_id=t.workspace_id and e.id=we.event_id
		left join timeline.event_participant ep on ep.workspace_id=e.workspace_id and ep.event_id=e.id
		where c.from_record_id in (t.alias_record_id,t.canonical_record_id)
		   or c.to_record_id in (t.alias_record_id,t.canonical_record_id)
		   or e.location_record_id in (t.alias_record_id,t.canonical_record_id)
		   or ep.record_id in (t.alias_record_id,t.canonical_record_id)
		order by b.id desc`, uuid(workspace), uuid(resolution))
	if err != nil {
		return domain.Impact{}, translate(ctx, err)
	}
	for briefRows.Next() {
		var briefID pgtype.UUID
		var title string
		var updatedAt time.Time
		if err := briefRows.Scan(&briefID, &title, &updatedAt); err != nil {
			briefRows.Close()
			return domain.Impact{}, translate(ctx, err)
		}
		impact.Briefs = append(impact.Briefs, domain.BriefImpact{ID: id.ID(briefID.Bytes), Title: title, UpdatedAt: updatedAt})
	}
	if err := briefRows.Err(); err != nil {
		briefRows.Close()
		return domain.Impact{}, translate(ctx, err)
	}
	briefRows.Close()

	snapshotRows, err := s.db.DB(ctx).Query(ctx, `
		with target as (
			select workspace_id, alias_record_id, canonical_record_id
			from research.record_resolution
			where workspace_id=$1 and id=$2
		)
		select distinct s.id,s.brief_id,s.title,s.frozen_at
		from target t
		join brief.snapshot s on s.workspace_id=t.workspace_id
		left join brief.snapshot_connection sc on sc.workspace_id=s.workspace_id and sc.snapshot_id=s.id
		left join brief.snapshot_event se on se.workspace_id=s.workspace_id and se.snapshot_id=s.id
		where sc.from_record_id in (t.alias_record_id,t.canonical_record_id)
		   or sc.to_record_id in (t.alias_record_id,t.canonical_record_id)
		   or exists (
				select 1
				from jsonb_array_elements(coalesce(se.participant_records,'[]'::jsonb)) participant
				where participant->>'record_id' in (t.alias_record_id::text,t.canonical_record_id::text)
			)
		   or se.location_record->>'record_id' in (t.alias_record_id::text,t.canonical_record_id::text)
		order by s.id desc`, uuid(workspace), uuid(resolution))
	if err != nil {
		return domain.Impact{}, translate(ctx, err)
	}
	for snapshotRows.Next() {
		var snapshotID, briefID pgtype.UUID
		var title string
		var frozenAt time.Time
		if err := snapshotRows.Scan(&snapshotID, &briefID, &title, &frozenAt); err != nil {
			snapshotRows.Close()
			return domain.Impact{}, translate(ctx, err)
		}
		impact.Snapshots = append(impact.Snapshots, domain.SnapshotImpact{ID: id.ID(snapshotID.Bytes), BriefID: id.ID(briefID.Bytes), Title: title, FrozenAt: frozenAt})
	}
	if err := snapshotRows.Err(); err != nil {
		snapshotRows.Close()
		return domain.Impact{}, translate(ctx, err)
	}
	snapshotRows.Close()
	return impact, nil
}
