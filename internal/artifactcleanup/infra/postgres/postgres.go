package postgres

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/artifactcleanup/domain"
	pkgerrors "github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	pg "github.com/0xsj/overwatch-backend/pkg/postgres"
)

type Store struct{ db *pg.Pool }

const Schema = "artifact_cleanup"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []pg.Migration {
	ms, err := pg.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("artifact cleanup: " + err.Error())
	}
	return ms
}

func NewStore(db *pg.Pool) *Store {
	if db == nil {
		panic("artifact cleanup: NewStore with a nil pool")
	}
	return &Store{db: db}
}

func uuid(i id.ID) pgtype.UUID { return pgtype.UUID{Bytes: i, Valid: !i.IsZero()} }

// Inventory is deliberately global-reference aware. A content address may be
// present in several workspaces and in domains that do not know about source
// retention, so a source-local count is not enough to prove that unlinking is
// safe.
func (s *Store) Inventory(ctx context.Context, workspace id.ID, limit int) (domain.Inventory, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.DB(ctx).Query(ctx, `
with candidate as (
    select c.sha256 as hex, 'source_capture'::text as kind,
           c.workspace_id, c.source_id, c.id as capture_id,
           null::uuid as extraction_id, c.bytes,
           so.purged_at, c.captured_at as created_at
      from source.capture c
      join source.source so on so.id=c.source_id and so.workspace_id=c.workspace_id
     where c.workspace_id=$1 and so.purged_at is not null
    union all
    select e.output_sha256 as hex, 'source_extraction'::text as kind,
           e.workspace_id, e.source_id, e.capture_id,
           e.id as extraction_id, e.output_bytes as bytes,
           so.purged_at, e.created_at
      from source_extraction.extraction e
      join source.source so on so.id=e.source_id and so.workspace_id=e.workspace_id
     where e.workspace_id=$1 and so.purged_at is not null and e.output_sha256 <> ''
), distinct_candidate as (
    select distinct on (hex) hex, kind, workspace_id, source_id, capture_id,
           extraction_id, bytes, purged_at, created_at
      from candidate
     order by hex, kind, created_at, source_id
)
select 'sha256:'||c.hex, c.kind, c.workspace_id, c.source_id,
       c.capture_id, c.extraction_id, c.bytes, c.purged_at, c.created_at
  from distinct_candidate c
 where not exists (
       select 1
         from source.capture live
         join source.source live_source on live_source.id=live.source_id
                                      and live_source.workspace_id=live.workspace_id
        where live.sha256=c.hex and live_source.purged_at is null
   )
   and not exists (
       select 1
         from source_extraction.extraction live
         join source.source live_source on live_source.id=live.source_id
                                      and live_source.workspace_id=live.workspace_id
        where live.output_sha256=c.hex and live.output_sha256 <> ''
          and live_source.purged_at is null
   )
   and not exists (select 1 from run.artifact where hash='sha256:'||c.hex)
   and not exists (select 1 from report.revision where hash='sha256:'||c.hex)
 order by c.purged_at, c.hex
 limit $2`, uuid(workspace), limit)
	if err != nil {
		return domain.Inventory{}, pg.Translate(ctx, err, "artifact cleanup: inventory")
	}
	defer rows.Close()
	out := domain.Inventory{Items: []domain.Candidate{}}
	for rows.Next() {
		var ref, kind string
		var workspaceID, sourceID, captureID, extractionID pgtype.UUID
		var bytes int64
		var purgedAt, createdAt pgtype.Timestamptz
		if err := rows.Scan(&ref, &kind, &workspaceID, &sourceID, &captureID, &extractionID, &bytes, &purgedAt, &createdAt); err != nil {
			return domain.Inventory{}, pg.Translate(ctx, err, "artifact cleanup: scan inventory")
		}
		candidate := domain.Candidate{Ref: ref, Kind: kind, WorkspaceID: id.ID(workspaceID.Bytes), SourceID: id.ID(sourceID.Bytes), Bytes: bytes, PurgedAt: purgedAt.Time, CreatedAt: createdAt.Time}
		if captureID.Valid {
			candidate.CaptureID = id.ID(captureID.Bytes)
		}
		if extractionID.Valid {
			candidate.ExtractionID = id.ID(extractionID.Bytes)
		}
		out.Items = append(out.Items, candidate)
		out.CandidateCount++
		out.CandidateBytes += bytes
	}
	if err := rows.Err(); err != nil {
		return domain.Inventory{}, pg.Translate(ctx, err, "artifact cleanup: inventory rows")
	}
	return out, nil
}

func (s *Store) References(ctx context.Context, workspace id.ID, beforeHex, exactHex string, limit int) ([]domain.Reference, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 101 {
		limit = 101
	}
	rows, err := s.db.DB(ctx).Query(ctx, `
with reference as (
    select c.sha256 as hex, 'source_capture'::text as kind,
           c.workspace_id, c.source_id, c.id as capture_id,
           null::uuid as extraction_id, c.bytes,
           so.purged_at, c.captured_at as created_at
      from source.capture c
      join source.source so on so.id=c.source_id and so.workspace_id=c.workspace_id
     where c.workspace_id=$1
    union all
    select e.output_sha256 as hex, 'source_extraction'::text as kind,
           e.workspace_id, e.source_id, e.capture_id,
           e.id as extraction_id, e.output_bytes as bytes,
           so.purged_at, e.created_at
      from source_extraction.extraction e
      join source.source so on so.id=e.source_id and so.workspace_id=e.workspace_id
     where e.workspace_id=$1 and e.status='succeeded' and e.output_sha256 <> ''
), ranked as (
    select r.*,
           count(*) over (partition by r.hex) as reference_count,
           count(*) filter (where r.kind='source_capture' and r.purged_at is null) over (partition by r.hex) as live_source_count,
           count(*) filter (where r.kind='source_extraction' and r.purged_at is null) over (partition by r.hex) as live_extraction_count,
           row_number() over (partition by r.hex order by (r.purged_at is null) desc, r.kind, r.created_at, r.source_id) as row_number
      from reference r
)
select 'sha256:'||r.hex, r.kind, r.workspace_id, r.source_id,
       r.capture_id, r.extraction_id, r.bytes, r.purged_at, r.created_at,
       r.reference_count, r.live_source_count, r.live_extraction_count,
       exists (select 1 from run.artifact where hash='sha256:'||r.hex),
       exists (select 1 from report.revision where hash='sha256:'||r.hex)
 from ranked r
 where r.row_number=1 and ($2='' or r.hex>$2) and ($3='' or r.hex=$3)
 order by r.hex
 limit $4`, uuid(workspace), beforeHex, exactHex, limit)
	if err != nil {
		return nil, pg.Translate(ctx, err, "artifact cleanup: references")
	}
	defer rows.Close()
	out := make([]domain.Reference, 0)
	for rows.Next() {
		var ref, kind string
		var workspaceID, sourceID, captureID, extractionID pgtype.UUID
		var bytes, referenceCount, liveSourceCount, liveExtractionCount int64
		var purgedAt pgtype.Timestamptz
		var createdAt pgtype.Timestamptz
		var runReferenced, reportReferenced bool
		if err := rows.Scan(&ref, &kind, &workspaceID, &sourceID, &captureID, &extractionID, &bytes, &purgedAt, &createdAt, &referenceCount, &liveSourceCount, &liveExtractionCount, &runReferenced, &reportReferenced); err != nil {
			return nil, pg.Translate(ctx, err, "artifact cleanup: scan reference")
		}
		reference := domain.Reference{
			Ref:                 ref,
			Kind:                kind,
			WorkspaceID:         id.ID(workspaceID.Bytes),
			SourceID:            id.ID(sourceID.Bytes),
			Bytes:               bytes,
			CreatedAt:           createdAt.Time,
			ReferenceCount:      int(referenceCount),
			LiveSourceCount:     int(liveSourceCount),
			LiveExtractionCount: int(liveExtractionCount),
			RunReferenced:       runReferenced,
			ReportReferenced:    reportReferenced,
		}
		if captureID.Valid {
			reference.CaptureID = id.ID(captureID.Bytes)
		}
		if extractionID.Valid {
			reference.ExtractionID = id.ID(extractionID.Bytes)
		}
		if purgedAt.Valid {
			purged := purgedAt.Time
			reference.PurgedAt = &purged
		}
		out = append(out, reference)
	}
	if err := rows.Err(); err != nil {
		return nil, pg.Translate(ctx, err, "artifact cleanup: reference rows")
	}
	return out, nil
}

func (s *Store) LastSweepItems(ctx context.Context, workspace id.ID, limit int) (map[string]domain.SweepItem, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 101 {
		limit = 101
	}
	rows, err := s.db.DB(ctx).Query(ctx, `
with latest as (
    select i.*,
           row_number() over (partition by i.ref order by i.recorded_at desc, i.sweep_id desc) as row_number
      from artifact_cleanup.sweep_item i
     where i.workspace_id=$1
)
select sweep_id,workspace_id,ref,kind,source_id,capture_id,extraction_id,bytes,outcome,reason,recorded_at
  from latest
 where row_number=1
 order by recorded_at desc, ref
 limit $2`, uuid(workspace), limit)
	if err != nil {
		return nil, pg.Translate(ctx, err, "artifact cleanup: last sweep items")
	}
	defer rows.Close()
	out := make(map[string]domain.SweepItem)
	for rows.Next() {
		var item domain.SweepItem
		var sweepID, workspaceID, sourceID, captureID, extractionID pgtype.UUID
		if err := rows.Scan(&sweepID, &workspaceID, &item.Ref, &item.Kind, &sourceID, &captureID, &extractionID, &item.Bytes, &item.Outcome, &item.Reason, &item.RecordedAt); err != nil {
			return nil, pg.Translate(ctx, err, "artifact cleanup: scan sweep item")
		}
		item.SweepID = id.ID(sweepID.Bytes)
		item.WorkspaceID = id.ID(workspaceID.Bytes)
		item.SourceID = id.ID(sourceID.Bytes)
		if captureID.Valid {
			item.CaptureID = id.ID(captureID.Bytes)
		}
		if extractionID.Valid {
			item.ExtractionID = id.ID(extractionID.Bytes)
		}
		out[item.Ref] = item
	}
	if err := rows.Err(); err != nil {
		return nil, pg.Translate(ctx, err, "artifact cleanup: sweep item rows")
	}
	return out, nil
}

func (s *Store) RecordSweepItem(ctx context.Context, item domain.SweepItem) error {
	_, err := s.db.DB(ctx).Exec(ctx, `
insert into artifact_cleanup.sweep_item(sweep_id,workspace_id,ref,kind,source_id,capture_id,extraction_id,bytes,outcome,reason,recorded_at)
values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
on conflict (sweep_id,ref) do update set outcome=excluded.outcome,reason=excluded.reason,recorded_at=excluded.recorded_at`,
		uuid(item.SweepID), uuid(item.WorkspaceID), item.Ref, item.Kind, uuid(item.SourceID), uuid(item.CaptureID), uuid(item.ExtractionID), item.Bytes, item.Outcome, item.Reason, item.RecordedAt)
	return pg.Translate(ctx, err, "artifact cleanup: record sweep item")
}

func (s *Store) OpenReview(ctx context.Context, workspace id.ID) (domain.Review, error) {
	rows, err := s.db.DB(ctx).Query(ctx, `
select id,workspace_id,created_by,updated_by,status,created_at,updated_at,completed_at,sweep_id,discarded_by,discarded_at,discard_reason
  from artifact_cleanup.review
 where workspace_id=$1 and status='open'
 order by updated_at desc
 limit 1`, uuid(workspace))
	if err != nil {
		return domain.Review{}, pg.Translate(ctx, err, "artifact cleanup: open review")
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return domain.Review{}, pg.Translate(ctx, err, "artifact cleanup: open review rows")
		}
		return domain.Review{WorkspaceID: workspace, Status: domain.ReviewNone, Items: []domain.Candidate{}, SelectedRefs: []string{}}, nil
	}
	review, err := scanReview(rows)
	if err != nil {
		return domain.Review{}, pg.Translate(ctx, err, "artifact cleanup: scan open review")
	}
	if err := rows.Err(); err != nil {
		return domain.Review{}, pg.Translate(ctx, err, "artifact cleanup: open review rows")
	}
	return s.loadReviewItems(ctx, review)
}

func (s *Store) LatestReview(ctx context.Context, workspace id.ID) (domain.Review, error) {
	rows, err := s.db.DB(ctx).Query(ctx, `
select id,workspace_id,created_by,updated_by,status,created_at,updated_at,completed_at,sweep_id,discarded_by,discarded_at,discard_reason
  from artifact_cleanup.review
 where workspace_id=$1
 order by updated_at desc
 limit 1`, uuid(workspace))
	if err != nil {
		return domain.Review{}, pg.Translate(ctx, err, "artifact cleanup: latest review")
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return domain.Review{}, pg.Translate(ctx, err, "artifact cleanup: latest review rows")
		}
		return domain.Review{WorkspaceID: workspace, Status: domain.ReviewNone, Items: []domain.Candidate{}, SelectedRefs: []string{}}, nil
	}
	review, err := scanReview(rows)
	if err != nil {
		return domain.Review{}, pg.Translate(ctx, err, "artifact cleanup: scan latest review")
	}
	if err := rows.Err(); err != nil {
		return domain.Review{}, pg.Translate(ctx, err, "artifact cleanup: latest review rows")
	}
	return s.loadReviewItems(ctx, review)
}

func (s *Store) ReadReview(ctx context.Context, workspace, reviewID id.ID) (domain.Review, error) {
	rows, err := s.db.DB(ctx).Query(ctx, `
select id,workspace_id,created_by,updated_by,status,created_at,updated_at,completed_at,sweep_id,discarded_by,discarded_at,discard_reason
  from artifact_cleanup.review
 where workspace_id=$1 and id=$2`, uuid(workspace), uuid(reviewID))
	if err != nil {
		return domain.Review{}, pg.Translate(ctx, err, "artifact cleanup: read review")
	}
	defer rows.Close()
	if !rows.Next() {
		return domain.Review{}, pkgerrors.New(pkgerrors.NotFound, "cleanup review not found")
	}
	review, err := scanReview(rows)
	if err != nil {
		return domain.Review{}, pg.Translate(ctx, err, "artifact cleanup: scan review")
	}
	if err := rows.Err(); err != nil {
		return domain.Review{}, pg.Translate(ctx, err, "artifact cleanup: review rows")
	}
	return s.loadReviewItems(ctx, review)
}

func scanReview(rows interface{ Scan(...any) error }) (domain.Review, error) {
	var review domain.Review
	var reviewID, workspaceID, createdBy, updatedBy pgtype.UUID
	var completedAt pgtype.Timestamptz
	var sweepID pgtype.UUID
	var discardedBy pgtype.UUID
	var discardedAt pgtype.Timestamptz
	if err := rows.Scan(&reviewID, &workspaceID, &createdBy, &updatedBy, &review.Status, &review.CreatedAt, &review.UpdatedAt, &completedAt, &sweepID, &discardedBy, &discardedAt, &review.DiscardReason); err != nil {
		return domain.Review{}, err
	}
	review.ID = id.ID(reviewID.Bytes)
	review.WorkspaceID = id.ID(workspaceID.Bytes)
	review.CreatedBy = id.ID(createdBy.Bytes)
	review.UpdatedBy = id.ID(updatedBy.Bytes)
	if completedAt.Valid {
		completed := completedAt.Time
		review.CompletedAt = &completed
	}
	if sweepID.Valid {
		parsedSweepID := id.ID(sweepID.Bytes)
		review.SweepID = &parsedSweepID
	}
	if discardedBy.Valid {
		parsedDiscardedBy := id.ID(discardedBy.Bytes)
		review.DiscardedBy = &parsedDiscardedBy
	}
	if discardedAt.Valid {
		discarded := discardedAt.Time
		review.DiscardedAt = &discarded
	}
	return review, nil
}

func (s *Store) loadReviewItems(ctx context.Context, review domain.Review) (domain.Review, error) {
	rows, err := s.db.DB(ctx).Query(ctx, `
select ref,kind,workspace_id,source_id,capture_id,extraction_id,bytes,purged_at,created_at
  from artifact_cleanup.review_item
 where review_id=$1
 order by ref`, uuid(review.ID))
	if err != nil {
		return domain.Review{}, pg.Translate(ctx, err, "artifact cleanup: review items")
	}
	defer rows.Close()
	review.Items = []domain.Candidate{}
	review.SelectedRefs = []string{}
	for rows.Next() {
		var item domain.Candidate
		var workspaceID, sourceID, captureID, extractionID pgtype.UUID
		if err := rows.Scan(&item.Ref, &item.Kind, &workspaceID, &sourceID, &captureID, &extractionID, &item.Bytes, &item.PurgedAt, &item.CreatedAt); err != nil {
			return domain.Review{}, pg.Translate(ctx, err, "artifact cleanup: scan review item")
		}
		item.WorkspaceID = id.ID(workspaceID.Bytes)
		item.SourceID = id.ID(sourceID.Bytes)
		if captureID.Valid {
			item.CaptureID = id.ID(captureID.Bytes)
		}
		if extractionID.Valid {
			item.ExtractionID = id.ID(extractionID.Bytes)
		}
		review.Items = append(review.Items, item)
		review.SelectedRefs = append(review.SelectedRefs, item.Ref)
	}
	if err := rows.Err(); err != nil {
		return domain.Review{}, pg.Translate(ctx, err, "artifact cleanup: review item rows")
	}
	return review, nil
}

func (s *Store) SaveReview(ctx context.Context, review domain.Review) error {
	return s.db.InTx(ctx, func(ctx context.Context) error {
		if _, err := s.db.DB(ctx).Exec(ctx, `
insert into artifact_cleanup.review(id,workspace_id,created_by,updated_by,status,created_at,updated_at,completed_at,sweep_id,discarded_by,discarded_at,discard_reason)
values($1,$2,$3,$4,$5,$6,$7,null,null,null,null,'')
on conflict (id) do update set updated_by=excluded.updated_by,status=excluded.status,updated_at=excluded.updated_at,completed_at=null,sweep_id=null,discarded_by=null,discarded_at=null,discard_reason=''`,
			uuid(review.ID), uuid(review.WorkspaceID), uuid(review.CreatedBy), uuid(review.UpdatedBy), review.Status, review.CreatedAt, review.UpdatedAt); err != nil {
			return pg.Translate(ctx, err, "artifact cleanup: save review")
		}
		if _, err := s.db.DB(ctx).Exec(ctx, `delete from artifact_cleanup.review_item where review_id=$1`, uuid(review.ID)); err != nil {
			return pg.Translate(ctx, err, "artifact cleanup: replace review items")
		}
		for _, item := range review.Items {
			if _, err := s.db.DB(ctx).Exec(ctx, `
insert into artifact_cleanup.review_item(review_id,workspace_id,ref,kind,source_id,capture_id,extraction_id,bytes,purged_at,created_at)
values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
				uuid(review.ID), uuid(review.WorkspaceID), item.Ref, item.Kind, uuid(item.SourceID), uuid(item.CaptureID), uuid(item.ExtractionID), item.Bytes, item.PurgedAt, item.CreatedAt); err != nil {
				return pg.Translate(ctx, err, "artifact cleanup: save review item")
			}
		}
		return nil
	})
}

func (s *Store) RecentReviews(ctx context.Context, workspace id.ID, limit int) ([]domain.ReviewSummary, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	rows, err := s.db.DB(ctx).Query(ctx, `
select r.id,r.workspace_id,r.created_by,r.updated_by,r.status,r.created_at,r.updated_at,r.completed_at,r.sweep_id,r.discarded_by,r.discarded_at,r.discard_reason,
       count(i.ref),coalesce(sum(i.bytes),0)
  from artifact_cleanup.review r
  left join artifact_cleanup.review_item i on i.review_id=r.id
 where r.workspace_id=$1
 group by r.id
 order by r.updated_at desc
 limit $2`, uuid(workspace), limit)
	if err != nil {
		return nil, pg.Translate(ctx, err, "artifact cleanup: recent reviews")
	}
	defer rows.Close()
	out := []domain.ReviewSummary{}
	for rows.Next() {
		item, err := scanReviewSummary(rows)
		if err != nil {
			return nil, pg.Translate(ctx, err, "artifact cleanup: scan review summary")
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, pg.Translate(ctx, err, "artifact cleanup: recent review rows")
	}
	return out, nil
}

func scanReviewSummary(rows interface{ Scan(...any) error }) (domain.ReviewSummary, error) {
	var summary domain.ReviewSummary
	var reviewID, workspaceID, createdBy, updatedBy, sweepID, discardedBy pgtype.UUID
	var completedAt, discardedAt pgtype.Timestamptz
	var itemCount int64
	if err := rows.Scan(&reviewID, &workspaceID, &createdBy, &updatedBy, &summary.Status, &summary.CreatedAt, &summary.UpdatedAt, &completedAt, &sweepID, &discardedBy, &discardedAt, &summary.DiscardReason, &itemCount, &summary.ItemBytes); err != nil {
		return domain.ReviewSummary{}, err
	}
	summary.ID = id.ID(reviewID.Bytes)
	summary.WorkspaceID = id.ID(workspaceID.Bytes)
	summary.CreatedBy = id.ID(createdBy.Bytes)
	summary.UpdatedBy = id.ID(updatedBy.Bytes)
	summary.ItemCount = int(itemCount)
	if completedAt.Valid {
		completed := completedAt.Time
		summary.CompletedAt = &completed
	}
	if sweepID.Valid {
		parsedSweepID := id.ID(sweepID.Bytes)
		summary.SweepID = &parsedSweepID
	}
	if discardedBy.Valid {
		parsedDiscardedBy := id.ID(discardedBy.Bytes)
		summary.DiscardedBy = &parsedDiscardedBy
	}
	if discardedAt.Valid {
		discarded := discardedAt.Time
		summary.DiscardedAt = &discarded
	}
	return summary, nil
}

func (s *Store) DiscardReview(ctx context.Context, review domain.Review) error {
	if review.DiscardedBy == nil || review.DiscardedAt == nil {
		return fmt.Errorf("artifact cleanup: discarded review is missing actor or time")
	}
	result, err := s.db.DB(ctx).Exec(ctx, `
update artifact_cleanup.review
   set status='discarded',updated_by=$3,updated_at=$4,discarded_by=$3,discarded_at=$4,discard_reason=$5
 where id=$1 and workspace_id=$2 and status='open'`,
		uuid(review.ID), uuid(review.WorkspaceID), uuid(*review.DiscardedBy), *review.DiscardedAt, review.DiscardReason)
	if err != nil {
		return pg.Translate(ctx, err, "artifact cleanup: discard review")
	}
	if result.RowsAffected() != 1 {
		return pkgerrors.New(pkgerrors.PreconditionRequired, "only an open cleanup review can be discarded")
	}
	return nil
}

func (s *Store) StartSweep(ctx context.Context, run domain.SweepRun) error {
	return s.db.InTx(ctx, func(ctx context.Context) error {
		if _, err := s.db.DB(ctx).Exec(ctx, `update artifact_cleanup.sweep_run set status='interrupted',finished_at=$2,error='interrupted by a later sweep' where workspace_id=$1 and status='running'`, uuid(run.WorkspaceID), run.StartedAt); err != nil {
			return pg.Translate(ctx, err, "artifact cleanup: interrupt prior sweep")
		}
		var reviewID pgtype.UUID
		if run.ReviewID != nil {
			reviewID = uuid(*run.ReviewID)
		}
		_, err := s.db.DB(ctx).Exec(ctx, `insert into artifact_cleanup.sweep_run(id,workspace_id,requested_by,status,"limit",candidate_count,candidate_bytes,deleted_count,deleted_bytes,already_gone_count,skipped_count,error,started_at,review_id) values($1,$2,$3,$4,$5,0,0,0,0,0,0,'',$6,$7)`, uuid(run.ID), uuid(run.WorkspaceID), uuid(run.RequestedBy), run.Status, run.Limit, run.StartedAt, reviewID)
		return pg.Translate(ctx, err, "artifact cleanup: start sweep")
	})
}

func (s *Store) FinishSweep(ctx context.Context, run domain.SweepRun) error {
	return s.db.InTx(ctx, func(ctx context.Context) error {
		result, err := s.db.DB(ctx).Exec(ctx, `update artifact_cleanup.sweep_run set status=$3,candidate_count=$4,candidate_bytes=$5,deleted_count=$6,deleted_bytes=$7,already_gone_count=$8,skipped_count=$9,error='',finished_at=$10 where id=$1 and workspace_id=$2`, uuid(run.ID), uuid(run.WorkspaceID), run.Status, run.CandidateCount, run.CandidateBytes, run.DeletedCount, run.DeletedBytes, run.AlreadyGoneCount, run.SkippedCount, run.FinishedAt)
		if err != nil {
			return pg.Translate(ctx, err, "artifact cleanup: finish sweep")
		}
		if result.RowsAffected() != 1 {
			return fmt.Errorf("artifact cleanup: finish sweep %s: row was not running", run.ID)
		}
		if run.ReviewID != nil {
			_, err := s.db.DB(ctx).Exec(ctx, `update artifact_cleanup.review set status='completed',completed_at=$3,updated_at=$3,updated_by=$4,sweep_id=$5 where id=$1 and workspace_id=$2 and status='open'`, uuid(*run.ReviewID), uuid(run.WorkspaceID), run.FinishedAt, uuid(run.RequestedBy), uuid(run.ID))
			if err != nil {
				return pg.Translate(ctx, err, "artifact cleanup: complete review")
			}
		}
		return nil
	})
}

func (s *Store) FailSweep(ctx context.Context, run domain.SweepRun) error {
	result, err := s.db.DB(ctx).Exec(ctx, `update artifact_cleanup.sweep_run set status='failed',candidate_count=$2,candidate_bytes=$3,deleted_count=$4,deleted_bytes=$5,already_gone_count=$6,skipped_count=$7,error=$8,finished_at=$9 where id=$1 and status='running'`, uuid(run.ID), run.CandidateCount, run.CandidateBytes, run.DeletedCount, run.DeletedBytes, run.AlreadyGoneCount, run.SkippedCount, run.Error, run.FinishedAt)
	if err != nil {
		return pg.Translate(ctx, err, "artifact cleanup: fail sweep")
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("artifact cleanup: fail sweep %s: row was not running", run.ID)
	}
	return nil
}

func (s *Store) RecentSweeps(ctx context.Context, workspace id.ID, limit int) ([]domain.SweepRun, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	rows, err := s.db.DB(ctx).Query(ctx, `select id,workspace_id,requested_by,status,"limit",candidate_count,candidate_bytes,deleted_count,deleted_bytes,already_gone_count,skipped_count,error,started_at,finished_at,review_id from artifact_cleanup.sweep_run where workspace_id=$1 order by started_at desc limit $2`, uuid(workspace), limit)
	if err != nil {
		return nil, pg.Translate(ctx, err, "artifact cleanup: recent sweeps")
	}
	defer rows.Close()
	out := []domain.SweepRun{}
	for rows.Next() {
		var sweep domain.SweepRun
		var sweepID, workspaceID, requestedBy pgtype.UUID
		var finishedAt pgtype.Timestamptz
		var reviewID pgtype.UUID
		if err := rows.Scan(&sweepID, &workspaceID, &requestedBy, &sweep.Status, &sweep.Limit, &sweep.CandidateCount, &sweep.CandidateBytes, &sweep.DeletedCount, &sweep.DeletedBytes, &sweep.AlreadyGoneCount, &sweep.SkippedCount, &sweep.Error, &sweep.StartedAt, &finishedAt, &reviewID); err != nil {
			return nil, pg.Translate(ctx, err, "artifact cleanup: scan sweep")
		}
		sweep.ID, sweep.WorkspaceID, sweep.RequestedBy = id.ID(sweepID.Bytes), id.ID(workspaceID.Bytes), id.ID(requestedBy.Bytes)
		if finishedAt.Valid {
			finished := finishedAt.Time
			sweep.FinishedAt = &finished
		}
		if reviewID.Valid {
			idCopy := id.ID(reviewID.Bytes)
			sweep.ReviewID = &idCopy
		}
		out = append(out, sweep)
	}
	if err := rows.Err(); err != nil {
		return nil, pg.Translate(ctx, err, "artifact cleanup: recent sweep rows")
	}
	return out, nil
}

var _ interface {
	Inventory(context.Context, id.ID, int) (domain.Inventory, error)
	References(context.Context, id.ID, string, string, int) ([]domain.Reference, error)
	LastSweepItems(context.Context, id.ID, int) (map[string]domain.SweepItem, error)
	OpenReview(context.Context, id.ID) (domain.Review, error)
	LatestReview(context.Context, id.ID) (domain.Review, error)
	ReadReview(context.Context, id.ID, id.ID) (domain.Review, error)
	SaveReview(context.Context, domain.Review) error
	RecentReviews(context.Context, id.ID, int) ([]domain.ReviewSummary, error)
	DiscardReview(context.Context, domain.Review) error
	RecordSweepItem(context.Context, domain.SweepItem) error
	StartSweep(context.Context, domain.SweepRun) error
	FinishSweep(context.Context, domain.SweepRun) error
	FailSweep(context.Context, domain.SweepRun) error
	RecentSweeps(context.Context, id.ID, int) ([]domain.SweepRun, error)
} = (*Store)(nil)
