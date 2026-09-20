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

func (s *Store) UpsertSourceLink(ctx context.Context, in domain.SourceLink) (domain.SourceLink, error) {
	row := s.db.DB(ctx).QueryRow(ctx, `
insert into review.source_link
 (id,workspace_id,downstream_observation_id,upstream_observation_id,rationale,author,created_at,updated_at)
select $1,$2,$3,$4,$5,$6,$7,$8
where exists (select 1 from observation.manual where id=$3 and workspace_id=$2)
  and exists (select 1 from observation.manual where id=$4 and workspace_id=$2)
on conflict (workspace_id,downstream_observation_id,upstream_observation_id)
do update set rationale=excluded.rationale, author=excluded.author, updated_at=excluded.updated_at
returning id,workspace_id,downstream_observation_id,upstream_observation_id,rationale,author,created_at,updated_at`,
		uuid(in.ID), uuid(in.WorkspaceID), uuid(in.DownstreamObservationID), uuid(in.UpstreamObservationID), in.Rationale, uuid(in.Author), in.CreatedAt, in.UpdatedAt)
	var out domain.SourceLink
	var link, workspace, downstream, upstream, author pgtype.UUID
	if err := row.Scan(&link, &workspace, &downstream, &upstream, &out.Rationale, &author, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return domain.SourceLink{}, translate(ctx, err)
	}
	out.ID, out.WorkspaceID = id.ID(link.Bytes), id.ID(workspace.Bytes)
	out.DownstreamObservationID, out.UpstreamObservationID, out.Author = id.ID(downstream.Bytes), id.ID(upstream.Bytes), id.ID(author.Bytes)
	return out, nil
}

func (s *Store) Create(ctx context.Context, in domain.Cluster) (domain.Cluster, error) {
	row := s.db.DB(ctx).QueryRow(ctx, `
insert into review.cluster
 (id,workspace_id,kind,title,description,author,updated_by,created_at,updated_at)
values ($1,$2,$3,$4,$5,$6,$7,$8,$9)
returning id,workspace_id,kind,title,description,author,updated_by,created_at,updated_at`,
		uuid(in.ID), uuid(in.WorkspaceID), in.Kind.String(), in.Title, in.Description, uuid(in.Author), uuid(in.UpdatedBy), in.CreatedAt, in.UpdatedAt)
	out, err := scanCluster(row)
	if err != nil {
		return domain.Cluster{}, translate(ctx, err)
	}
	if err := s.replaceClusterObservations(ctx, out); err != nil {
		return domain.Cluster{}, translate(ctx, err)
	}
	return out, nil
}

func (s *Store) ByID(ctx context.Context, workspace, want id.ID) (domain.Cluster, error) {
	row := s.db.DB(ctx).QueryRow(ctx, `
select id,workspace_id,kind,title,description,author,updated_by,created_at,updated_at
from review.cluster where workspace_id=$1 and id=$2`, uuid(workspace), uuid(want))
	out, err := scanCluster(row)
	if err != nil {
		return domain.Cluster{}, translate(ctx, err)
	}
	if err := s.loadClusterObservations(ctx, &out); err != nil {
		return domain.Cluster{}, translate(ctx, err)
	}
	return out, nil
}

func (s *Store) Update(ctx context.Context, in domain.Cluster) (domain.Cluster, error) {
	row := s.db.DB(ctx).QueryRow(ctx, `
update review.cluster set kind=$3,title=$4,description=$5,updated_by=$6,updated_at=$7
where workspace_id=$1 and id=$2
returning id,workspace_id,kind,title,description,author,updated_by,created_at,updated_at`,
		uuid(in.WorkspaceID), uuid(in.ID), in.Kind.String(), in.Title, in.Description, uuid(in.UpdatedBy), in.UpdatedAt)
	out, err := scanCluster(row)
	if err != nil {
		return domain.Cluster{}, translate(ctx, err)
	}
	if err := s.replaceClusterObservations(ctx, in); err != nil {
		return domain.Cluster{}, translate(ctx, err)
	}
	out.ObservationIDs = append([]id.ID(nil), in.ObservationIDs...)
	return out, nil
}

func (s *Store) PageClusters(ctx context.Context, workspace, before id.ID, limit int) ([]domain.Cluster, error) {
	rows, err := s.db.DB(ctx).Query(ctx, `
select id,workspace_id,kind,title,description,author,updated_by,created_at,updated_at
from review.cluster
where workspace_id=$1 and ($2::uuid is null or id < $2)
order by id desc limit $3`, uuid(workspace), uuid(before), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	out := make([]domain.Cluster, 0)
	for rows.Next() {
		one, err := scanCluster(rows)
		if err != nil {
			rows.Close()
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, translate(ctx, err)
	}
	rows.Close()
	for index := range out {
		if err := s.loadClusterObservations(ctx, &out[index]); err != nil {
			return nil, translate(ctx, err)
		}
	}
	return out, nil
}

func (s *Store) PageClusterCoverage(ctx context.Context, workspace, before id.ID, limit int) ([]domain.ClusterCoverage, error) {
	rows, err := s.db.DB(ctx).Query(ctx, `
with base as (
  select c.id,
         (select count(*)::int from review.cluster_observation co where co.cluster_id=c.id) as observation_count,
         (select count(distinct m.source_id)::int
          from review.cluster_observation co
          join observation.manual m on m.id=co.observation_id
          where co.cluster_id=c.id and m.workspace_id=$1) as distinct_source_count
  from review.cluster c
  where c.workspace_id=$1 and ($2::uuid is null or c.id < $2)
), coverage as (
  select base.*,
    (select count(*)::int from review.relation r
     where r.workspace_id=$1
       and exists (select 1 from review.cluster_observation co where co.cluster_id=base.id and co.observation_id=r.left_observation_id)
       and exists (select 1 from review.cluster_observation co where co.cluster_id=base.id and co.observation_id=r.right_observation_id)) as internal_reviewed_pairs,
    (select count(*)::int from review.relation r
     where r.workspace_id=$1 and r.kind='supports'
       and exists (select 1 from review.cluster_observation co where co.cluster_id=base.id and co.observation_id=r.left_observation_id)
       and exists (select 1 from review.cluster_observation co where co.cluster_id=base.id and co.observation_id=r.right_observation_id)) as supporting_count,
    (select count(*)::int from review.relation r
     where r.workspace_id=$1 and r.kind='contradicts'
       and exists (select 1 from review.cluster_observation co where co.cluster_id=base.id and co.observation_id=r.left_observation_id)
       and exists (select 1 from review.cluster_observation co where co.cluster_id=base.id and co.observation_id=r.right_observation_id)) as contradicting_count,
    (select count(*)::int from review.relation r
     where r.workspace_id=$1 and r.kind='repeats'
       and exists (select 1 from review.cluster_observation co where co.cluster_id=base.id and co.observation_id=r.left_observation_id)
       and exists (select 1 from review.cluster_observation co where co.cluster_id=base.id and co.observation_id=r.right_observation_id)) as repeating_count,
    (select count(*)::int from review.relation r
     where r.workspace_id=$1 and r.kind='unresolved'
       and exists (select 1 from review.cluster_observation co where co.cluster_id=base.id and co.observation_id=r.left_observation_id)
       and exists (select 1 from review.cluster_observation co where co.cluster_id=base.id and co.observation_id=r.right_observation_id)) as unresolved_count,
    (select count(*)::int from (
       select r.left_observation_id as observation_id from review.relation r
       where r.workspace_id=$1
         and exists (select 1 from review.cluster_observation co where co.cluster_id=base.id and co.observation_id=r.left_observation_id)
         and exists (select 1 from review.cluster_observation co where co.cluster_id=base.id and co.observation_id=r.right_observation_id)
       union
       select r.right_observation_id as observation_id from review.relation r
       where r.workspace_id=$1
         and exists (select 1 from review.cluster_observation co where co.cluster_id=base.id and co.observation_id=r.left_observation_id)
         and exists (select 1 from review.cluster_observation co where co.cluster_id=base.id and co.observation_id=r.right_observation_id)
    ) touched) as reviewed_observation_count
  from base
), final as (
  select coverage.*,
    (observation_count * greatest(observation_count - 1, 0) / 2)::int as possible_internal_pairs
  from coverage
)
select id,observation_count,distinct_source_count,reviewed_observation_count,supporting_count,contradicting_count,
       repeating_count,unresolved_count,internal_reviewed_pairs,possible_internal_pairs,
       greatest(possible_internal_pairs - internal_reviewed_pairs, 0)::int as unreviewed_internal_pairs,
       case when observation_count=0 then 'no_evidence'
            when contradicting_count>0 then 'contradiction_found'
            when unresolved_count>0 then 'unresolved'
            when observation_count<2 or distinct_source_count<2 or (supporting_count=0 and repeating_count>0) then 'needs_corroboration'
            when internal_reviewed_pairs < possible_internal_pairs then 'review_incomplete'
            else 'covered' end as status
from final order by id desc limit $3`, uuid(workspace), uuid(before), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.ClusterCoverage, 0)
	for rows.Next() {
		var one domain.ClusterCoverage
		var cluster pgtype.UUID
		if err := rows.Scan(&cluster, &one.ObservationCount, &one.DistinctSourceCount, &one.ReviewedObservationCount, &one.SupportingCount,
			&one.ContradictingCount, &one.RepeatingCount, &one.UnresolvedCount, &one.InternalReviewedPairs,
			&one.PossibleInternalPairs, &one.UnreviewedInternalPairs, &one.Status); err != nil {
			return nil, translate(ctx, err)
		}
		one.ClusterID = id.ID(cluster.Bytes)
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}

func (s *Store) replaceClusterObservations(ctx context.Context, cluster domain.Cluster) error {
	if _, err := s.db.DB(ctx).Exec(ctx, `delete from review.cluster_observation where cluster_id=$1`, uuid(cluster.ID)); err != nil {
		return err
	}
	for ordinal, observation := range cluster.ObservationIDs {
		result, err := s.db.DB(ctx).Exec(ctx, `
insert into review.cluster_observation (cluster_id,observation_id,ordinal)
select $1,m.id,$3 from observation.manual m
where m.id=$2 and m.workspace_id=$4`, uuid(cluster.ID), uuid(observation), ordinal, uuid(cluster.WorkspaceID))
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (s *Store) loadClusterObservations(ctx context.Context, cluster *domain.Cluster) error {
	rows, err := s.db.DB(ctx).Query(ctx, `
select observation_id from review.cluster_observation
where cluster_id=$1 order by ordinal`, uuid(cluster.ID))
	if err != nil {
		return err
	}
	defer rows.Close()
	cluster.ObservationIDs = make([]id.ID, 0)
	for rows.Next() {
		var observation pgtype.UUID
		if err := rows.Scan(&observation); err != nil {
			return err
		}
		cluster.ObservationIDs = append(cluster.ObservationIDs, id.ID(observation.Bytes))
	}
	return rows.Err()
}

type scanner interface{ Scan(...any) error }

func scanCluster(row scanner) (domain.Cluster, error) {
	var out domain.Cluster
	var cluster, workspace, author, updatedBy pgtype.UUID
	if err := row.Scan(&cluster, &workspace, &out.Kind, &out.Title, &out.Description, &author, &updatedBy, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return domain.Cluster{}, err
	}
	out.ID, out.WorkspaceID, out.Author, out.UpdatedBy = id.ID(cluster.Bytes), id.ID(workspace.Bytes), id.ID(author.Bytes), id.ID(updatedBy.Bytes)
	return out, nil
}

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

func (s *Store) PageSourceLinks(ctx context.Context, workspace, before id.ID, limit int) ([]domain.SourceLinkItem, error) {
	rows, err := s.db.DB(ctx).Query(ctx, `
with recursive edges as (
  select workspace_id, upstream_observation_id as parent, downstream_observation_id as child
  from review.source_link where workspace_id=$1
), reach(workspace_id,start_node,node) as (
  select workspace_id,parent,child from edges
  union
  select reach.workspace_id,reach.start_node,edges.child
  from reach join edges on edges.workspace_id=reach.workspace_id and edges.parent=reach.node
), links as (
  select l.id,l.workspace_id,l.downstream_observation_id,l.upstream_observation_id,l.rationale,l.author,l.created_at,l.updated_at,
         downstream_source.id as downstream_source_id, downstream_source.title as downstream_source_title, downstream.capture_id as downstream_capture_id, downstream.statement as downstream_statement,
         upstream_source.id as upstream_source_id, upstream_source.title as upstream_source_title, upstream.capture_id as upstream_capture_id, upstream.statement as upstream_statement,
         exists (select 1 from reach where reach.workspace_id=l.workspace_id and reach.start_node=l.downstream_observation_id and reach.node=l.upstream_observation_id) as cycle_detected
  from review.source_link l
  join observation.manual downstream on downstream.workspace_id=l.workspace_id and downstream.id=l.downstream_observation_id
  join source.source downstream_source on downstream_source.workspace_id=l.workspace_id and downstream_source.id=downstream.source_id
  join observation.manual upstream on upstream.workspace_id=l.workspace_id and upstream.id=l.upstream_observation_id
  join source.source upstream_source on upstream_source.workspace_id=l.workspace_id and upstream_source.id=upstream.source_id
  where l.workspace_id=$1 and ($2::uuid is null or l.id < $2)
)
select id,workspace_id,downstream_observation_id,upstream_observation_id,rationale,author,created_at,updated_at,
       downstream_source_id,downstream_source_title,downstream_capture_id,downstream_statement,
       upstream_source_id,upstream_source_title,upstream_capture_id,upstream_statement,cycle_detected
from links order by id desc limit $3`, uuid(workspace), uuid(before), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.SourceLinkItem, 0)
	for rows.Next() {
		var one domain.SourceLinkItem
		var link, space, downstream, upstream, author, downstreamSource, downstreamCapture, upstreamSource, upstreamCapture pgtype.UUID
		if err := rows.Scan(&link, &space, &downstream, &upstream, &one.Rationale, &author, &one.CreatedAt, &one.UpdatedAt,
			&downstreamSource, &one.DownstreamSourceTitle, &downstreamCapture, &one.DownstreamStatement,
			&upstreamSource, &one.UpstreamSourceTitle, &upstreamCapture, &one.UpstreamStatement, &one.CycleDetected); err != nil {
			return nil, translate(ctx, err)
		}
		one.ID, one.WorkspaceID = id.ID(link.Bytes), id.ID(space.Bytes)
		one.DownstreamObservationID, one.UpstreamObservationID, one.Author = id.ID(downstream.Bytes), id.ID(upstream.Bytes), id.ID(author.Bytes)
		one.DownstreamSourceID, one.UpstreamSourceID = id.ID(downstreamSource.Bytes), id.ID(upstreamSource.Bytes)
		one.DownstreamCaptureID, one.UpstreamCaptureID = id.ID(downstreamCapture.Bytes), id.ID(upstreamCapture.Bytes)
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

func (s *Store) PageBoard(ctx context.Context, workspace, before id.ID, filters domain.BoardFilters, limit int) ([]domain.BoardItem, error) {
	rows, err := s.db.DB(ctx).Query(ctx, `
with raw as (
  select m.id,m.workspace_id,m.source_id,s.title,m.capture_id,m.statement,m.quote,
         m.extraction_id,m.quote_start,m.quote_end,m.locator,m.author,m.recorded_at,
         (select count(*)::int from review.relation r where r.workspace_id=m.workspace_id and (r.left_observation_id=m.id or r.right_observation_id=m.id) and r.kind='supports') as supports,
         (select count(*)::int from review.relation r where r.workspace_id=m.workspace_id and (r.left_observation_id=m.id or r.right_observation_id=m.id) and r.kind='contradicts') as contradicts,
         (select count(*)::int from review.relation r where r.workspace_id=m.workspace_id and (r.left_observation_id=m.id or r.right_observation_id=m.id) and r.kind='repeats') as repeats,
         (select count(*)::int from review.relation r where r.workspace_id=m.workspace_id and (r.left_observation_id=m.id or r.right_observation_id=m.id) and r.kind='unresolved') as unresolved,
         (select count(distinct co.cluster_id)::int from review.cluster_observation co join review.cluster c on c.id=co.cluster_id and c.workspace_id=m.workspace_id where co.observation_id=m.id) as cluster_count
  from observation.manual m
  join source.source s on s.workspace_id=m.workspace_id and s.id=m.source_id
  where m.workspace_id=$1
    and ($2::uuid is null or m.id < $2)
    and ($3::uuid is null or m.source_id = $3)
    and ($4::uuid is null or exists (select 1 from research.record_observation ro where ro.workspace_id=m.workspace_id and ro.record_id=$4 and ro.observation_id=m.id))
    and ($5::uuid is null or exists (select 1 from timeline.event_observation eo where eo.workspace_id=m.workspace_id and eo.event_id=$5 and eo.observation_id=m.id))
    and ($6::timestamptz is null or m.recorded_at >= $6)
    and ($7::timestamptz is null or m.recorded_at < $7)
    and ($8 = '' or s.title ilike '%' || $8 || '%' or m.statement ilike '%' || $8 || '%' or m.quote ilike '%' || $8 || '%' or m.locator ilike '%' || $8 || '%')
), board as (
  select raw.*,
    case when supports=0 and contradicts=0 and repeats=0 and unresolved=0 then 'unreviewed'
         when contradicts>0 then 'contradiction'
         when unresolved>0 then 'unresolved'
         else 'reviewed' end as review_state
  from raw
)
select id,workspace_id,source_id,title,capture_id,statement,quote,
       extraction_id,quote_start,quote_end,locator,author,recorded_at,
       review_state,supports,contradicts,repeats,unresolved,cluster_count
from board
where ($9 = '' or review_state = $9)
  and (not $10 or unresolved > 0)
order by id desc limit $11`, uuid(workspace), uuid(before), uuid(filters.SourceID), uuid(filters.RecordID), uuid(filters.EventID), filters.RecordedFrom, filters.RecordedTo, filters.Query, filters.State.String(), filters.UnresolvedOnly, limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.BoardItem, 0)
	for rows.Next() {
		one, err := scanBoard(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
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

func scanBoard(row scanner) (domain.BoardItem, error) {
	var one domain.BoardItem
	var observation, space, source, capture, extraction, author pgtype.UUID
	var quote []byte
	var state string
	if err := row.Scan(&observation, &space, &source, &one.SourceTitle, &capture, &one.Statement, &quote,
		&extraction, &one.QuoteStart, &one.QuoteEnd, &one.Locator, &author, &one.RecordedAt,
		&state, &one.Supports, &one.Contradicts, &one.Repeats, &one.Unresolved, &one.ClusterCount); err != nil {
		return domain.BoardItem{}, err
	}
	one.ID, one.WorkspaceID, one.SourceID, one.CaptureID, one.Author = id.ID(observation.Bytes), id.ID(space.Bytes), id.ID(source.Bytes), id.ID(capture.Bytes), id.ID(author.Bytes)
	if extraction.Valid {
		value := id.ID(extraction.Bytes)
		one.ExtractionID = &value
	}
	one.Quote = string(quote)
	parsed, err := domain.ParseBoardState(state)
	if err != nil {
		return domain.BoardItem{}, err
	}
	one.ReviewState = parsed
	return one, nil
}
