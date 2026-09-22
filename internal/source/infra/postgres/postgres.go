package postgres

import (
	"context"
	"embed"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/source/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const Schema = "source"

//go:embed migrations/*.sql
var migrationFS embed.FS
var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("source: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("source: NewStore with a nil pool")
	}
	return &Store{db}
}
func uuid(i id.ID) pgtype.UUID { return pgtype.UUID{Bytes: i, Valid: !i.IsZero()} }
func nullableUUID(i *id.ID) pgtype.UUID {
	if i == nil {
		return pgtype.UUID{}
	}
	return uuid(*i)
}
func translate(ctx context.Context, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return postgres.Translate(ctx, err, "source")
}

func (s *Store) CreateSource(ctx context.Context, in domain.Source) error {
	_, err := s.db.DB(ctx).Exec(ctx, `insert into source.source(id,workspace_id,title,origin,url,filename,published_at,duplicate_policy,created_by,created_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, uuid(in.ID), uuid(in.WorkspaceID), in.Title, in.Origin, in.URL, in.Filename, in.PublishedAt, in.DuplicatePolicy, uuid(in.CreatedBy), in.CreatedAt)
	return translate(ctx, err)
}

func (s *Store) UpsertWatch(ctx context.Context, in domain.Watch) error {
	_, err := s.db.DB(ctx).Exec(ctx, `insert into source.watch(workspace_id,source_id,enabled,interval_seconds,next_run_at,last_run_at,last_status,last_capture_id,last_error,lease_owner,lease_until,updated_by,updated_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
on conflict (workspace_id,source_id) do update set enabled=excluded.enabled,interval_seconds=excluded.interval_seconds,next_run_at=excluded.next_run_at,last_run_at=excluded.last_run_at,last_status=excluded.last_status,last_capture_id=excluded.last_capture_id,last_error=excluded.last_error,lease_owner=excluded.lease_owner,lease_until=excluded.lease_until,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, uuid(in.WorkspaceID), uuid(in.SourceID), in.Enabled, in.IntervalSeconds, in.NextRunAt, in.LastRunAt, in.LastStatus, uuid(in.LastCaptureID), in.LastError, in.LeaseOwner, in.LeaseUntil, uuid(in.UpdatedBy), in.UpdatedAt)
	return translate(ctx, err)
}

func (s *Store) WatchBySource(ctx context.Context, workspace, source id.ID) (domain.Watch, error) {
	out, err := scanWatch(s.db.DB(ctx).QueryRow(ctx, `select workspace_id,source_id,enabled,interval_seconds,next_run_at,last_run_at,last_status,last_capture_id,last_error,lease_owner,lease_until,updated_by,updated_at from source.watch where workspace_id=$1 and source_id=$2`, uuid(workspace), uuid(source)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultWatch(workspace, source), nil
	}
	if err != nil {
		return domain.Watch{}, translate(ctx, err)
	}
	return out, nil
}

func (s *Store) ClaimDueWatch(ctx context.Context, workspace id.ID, owner string, now, leaseUntil time.Time) (domain.Watch, error) {
	row := s.db.DB(ctx).QueryRow(ctx, `with due as (
select workspace_id,source_id
from source.watch
where workspace_id=$1 and enabled and next_run_at <= $2
  and (lease_until is null or lease_until <= $2)
order by next_run_at,source_id
for update skip locked
limit 1
)
update source.watch as w
set lease_owner=$3,lease_until=$4
from due
where w.workspace_id=due.workspace_id and w.source_id=due.source_id
returning w.workspace_id,w.source_id,w.enabled,w.interval_seconds,w.next_run_at,w.last_run_at,w.last_status,w.last_capture_id,w.last_error,w.lease_owner,w.lease_until,w.updated_by,w.updated_at`, uuid(workspace), now, owner, leaseUntil)
	out, err := scanWatch(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Watch{}, domain.ErrWatchNotDue
	}
	if err != nil {
		return domain.Watch{}, translate(ctx, err)
	}
	return out, nil
}

func (s *Store) CreateAlert(ctx context.Context, in domain.Alert) error {
	_, err := s.db.DB(ctx).Exec(ctx, `insert into source.alert(id,workspace_id,source_id,capture_id,question_id,record_id,cluster_id,kind,title,detail,dedupe_key,active,created_by,created_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
on conflict (workspace_id,dedupe_key) do update set source_id=excluded.source_id,capture_id=excluded.capture_id,question_id=excluded.question_id,record_id=excluded.record_id,cluster_id=excluded.cluster_id,kind=excluded.kind,title=excluded.title,detail=excluded.detail,active=excluded.active,created_by=excluded.created_by,created_at=excluded.created_at`, uuid(in.ID), uuid(in.WorkspaceID), nullableUUID(in.SourceID), nullableUUID(in.CaptureID), nullableUUID(in.QuestionID), nullableUUID(in.RecordID), nullableUUID(in.ClusterID), in.Kind, in.Title, in.Detail, in.DedupeKey, in.Active, uuid(in.CreatedBy), in.CreatedAt)
	return translate(ctx, err)
}

func (s *Store) DeactivateDerivedGapAlerts(ctx context.Context, workspace id.ID, activeKeys []string) error {
	if len(activeKeys) == 0 {
		_, err := s.db.DB(ctx).Exec(ctx, `update source.alert set active=false where workspace_id=$1 and kind = any($2::text[])`, uuid(workspace), []string{domain.AlertKindQuestionGap, domain.AlertKindRecordGap, domain.AlertKindClusterGap})
		return translate(ctx, err)
	}
	_, err := s.db.DB(ctx).Exec(ctx, `update source.alert set active=false where workspace_id=$1 and kind = any($2::text[]) and not (dedupe_key = any($3::text[]))`, uuid(workspace), []string{domain.AlertKindQuestionGap, domain.AlertKindRecordGap, domain.AlertKindClusterGap}, activeKeys)
	return translate(ctx, err)
}

func (s *Store) DeactivateQuestionGapAlerts(ctx context.Context, workspace id.ID, activeKeys []string) error {
	var err error
	if len(activeKeys) == 0 {
		_, err = s.db.DB(ctx).Exec(ctx, `update source.alert set active=false where workspace_id=$1 and kind=$2`, uuid(workspace), domain.AlertKindQuestionGap)
	} else {
		_, err = s.db.DB(ctx).Exec(ctx, `update source.alert set active=false where workspace_id=$1 and kind=$2 and not (dedupe_key = any($3::text[]))`, uuid(workspace), domain.AlertKindQuestionGap, activeKeys)
	}
	return translate(ctx, err)
}

func (s *Store) MarkAlertSeen(ctx context.Context, workspace, alert, account id.ID, at time.Time) error {
	tag, err := s.db.DB(ctx).Exec(ctx, `insert into source.alert_seen(workspace_id,alert_id,account_id,seen_at)
select $1,$2,$3,$4
where exists (select 1 from source.alert where workspace_id=$1 and id=$2)
on conflict (alert_id,account_id) do update set seen_at=excluded.seen_at`, uuid(workspace), uuid(alert), uuid(account), at)
	if err != nil {
		return translate(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) AlertDelivery(ctx context.Context, workspace, account id.ID) (domain.AlertDelivery, error) {
	var email bool
	var kinds []string
	var updated time.Time
	err := s.db.DB(ctx).QueryRow(ctx, `select email_enabled,kinds,updated_at
from source.alert_delivery_preference where workspace_id=$1 and account_id=$2`, uuid(workspace), uuid(account)).Scan(&email, &kinds, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultAlertDelivery(workspace, account), nil
	}
	if err != nil {
		return domain.AlertDelivery{}, translate(ctx, err)
	}
	return domain.NewAlertDelivery(workspace, account, email, kinds, updated)
}

func (s *Store) SaveAlertDelivery(ctx context.Context, preference domain.AlertDelivery) error {
	_, err := s.db.DB(ctx).Exec(ctx, `insert into source.alert_delivery_preference(workspace_id,account_id,email_enabled,kinds,updated_at)
values($1,$2,$3,$4,$5)
on conflict (workspace_id,account_id) do update set email_enabled=excluded.email_enabled,kinds=excluded.kinds,updated_at=excluded.updated_at`, uuid(preference.WorkspaceID), uuid(preference.AccountID), preference.Email, preference.Kinds, preference.UpdatedAt)
	return translate(ctx, err)
}

func (s *Store) AlertPage(ctx context.Context, workspace, account, before id.ID, limit int, maxSensitivity string) ([]domain.AlertRow, error) {
	rows, err := s.db.DB(ctx).Query(ctx, `select a.id,a.workspace_id,a.source_id,a.capture_id,a.question_id,a.record_id,a.cluster_id,a.kind,a.title,a.detail,a.dedupe_key,a.active,a.created_by,a.created_at,coalesce(s.title,''),seen.seen_at
from source.alert a
left join source.source s on s.workspace_id=a.workspace_id and s.id=a.source_id
left join source.alert_seen seen on seen.workspace_id=a.workspace_id and seen.alert_id=a.id and seen.account_id=$2
where a.workspace_id=$1 and a.active
  and (s.id is null or s.sensitivity='public' or ($3='internal' and s.sensitivity='internal') or $3='restricted')
  and ($4::uuid is null or a.id < $4)
order by a.id desc limit $5`, uuid(workspace), uuid(account), maxSensitivity, uuid(before), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.AlertRow, 0)
	for rows.Next() {
		row, err := scanAlert(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, row)
	}
	return out, translate(ctx, rows.Err())
}

func (s *Store) CreateIntakeCandidate(ctx context.Context, in domain.IntakeCandidate) error {
	_, err := s.db.DB(ctx).Exec(ctx, `insert into source.intake_candidate(id,workspace_id,title,origin,url,filename,media_type,content_bytes,note,created_by,created_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, uuid(in.ID), uuid(in.WorkspaceID), in.Title, in.Origin, in.URL, in.Filename, in.MediaType, in.ContentBytes, in.Note, uuid(in.CreatedBy), in.CreatedAt)
	return translate(ctx, err)
}

func (s *Store) ReviewIntakeCandidate(ctx context.Context, in domain.IntakeCandidate) error {
	var reviewedAt time.Time
	if in.ReviewedAt != nil {
		reviewedAt = in.ReviewedAt.UTC()
	}
	tag, err := s.db.DB(ctx).Exec(ctx, `update source.intake_candidate set status=$3,reviewed_by=$4,reviewed_at=$5,review_note=$6,source_id=$7,content_bytes=null where workspace_id=$1 and id=$2 and status='pending'`, uuid(in.WorkspaceID), uuid(in.ID), in.Status, uuid(in.ReviewedBy), reviewedAt, in.ReviewNote, uuid(in.SourceID))
	if err != nil {
		return translate(ctx, err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var status string
	if err := s.db.DB(ctx).QueryRow(ctx, `select status from source.intake_candidate where workspace_id=$1 and id=$2`, uuid(in.WorkspaceID), uuid(in.ID)).Scan(&status); err != nil {
		return translate(ctx, err)
	}
	if status != domain.IntakePending {
		return domain.ErrIntakeAlreadyReviewed
	}
	return domain.ErrNotFound
}

func (s *Store) IntakeByID(ctx context.Context, workspace, intake id.ID) (domain.IntakeCandidate, error) {
	out, err := scanIntake(s.db.DB(ctx).QueryRow(ctx, intakeSelect+`where workspace_id=$1 and id=$2`, uuid(workspace), uuid(intake)))
	return out, translate(ctx, err)
}
func (s *Store) CreateCapture(ctx context.Context, in domain.Capture) error {
	_, err := s.db.DB(ctx).Exec(ctx, `insert into source.capture(id,source_id,workspace_id,version,media_type,sha256,bytes,captured_by,captured_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9)`, uuid(in.ID), uuid(in.SourceID), uuid(in.WorkspaceID), in.Version, in.MediaType, in.SHA256, in.Bytes, uuid(in.CapturedBy), in.CapturedAt)
	return translate(ctx, err)
}

func (s *Store) IndexText(ctx context.Context, workspace, source, capture, extraction id.ID, contentHash string, contentBytes int64, content string, indexedAt time.Time) error {
	document := capture
	if !extraction.IsZero() {
		document = extraction
	}
	// PostgreSQL text cannot carry U+0000. The retained bytes live in the blob
	// store and remain exact; the search index is a derived projection, so strip
	// the unsupported character only at this boundary.
	content = strings.ReplaceAll(content, "\x00", " ")
	_, err := s.db.DB(ctx).Exec(ctx, `insert into source.search_document(id,workspace_id,source_id,capture_id,extraction_id,content_sha256,content_bytes,indexed_at,search_vector)
values($1,$2,$3,$4,$5,$6,$7,$8,to_tsvector('simple',$9))
on conflict (id) do update set content_sha256=excluded.content_sha256,content_bytes=excluded.content_bytes,indexed_at=excluded.indexed_at,search_vector=excluded.search_vector`, uuid(document), uuid(workspace), uuid(source), uuid(capture), uuid(extraction), contentHash, contentBytes, indexedAt, content)
	return translate(ctx, err)
}

func (s *Store) SetRetention(ctx context.Context, workspace, source, editor id.ID, until *time.Time, at time.Time) error {
	tag, err := s.db.DB(ctx).Exec(ctx, `update source.source set retention_until=$3,retention_updated_by=$4,retention_updated_at=$5 where workspace_id=$1 and id=$2`, uuid(workspace), uuid(source), until, uuid(editor), at)
	if err != nil {
		return translate(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) SetPrivacy(ctx context.Context, workspace, source, editor id.ID, sensitivity string, legalHold bool, reason string, at time.Time) error {
	tag, err := s.db.DB(ctx).Exec(ctx, `update source.source set sensitivity=$3,privacy_updated_by=$4,privacy_updated_at=$5,legal_hold=$6,legal_hold_reason=$7 where workspace_id=$1 and id=$2 and purged_at is null`, uuid(workspace), uuid(source), sensitivity, uuid(editor), at, legalHold, reason)
	if err != nil {
		return translate(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		var purged bool
		if err := s.db.DB(ctx).QueryRow(ctx, `select purged_at is not null from source.source where workspace_id=$1 and id=$2`, uuid(workspace), uuid(source)).Scan(&purged); err != nil {
			return translate(ctx, err)
		}
		if purged {
			return domain.ErrPurged
		}
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) SetDuplicatePolicy(ctx context.Context, workspace, source, editor id.ID, policy string, at time.Time) error {
	tag, err := s.db.DB(ctx).Exec(ctx, `update source.source set duplicate_policy=$3 where workspace_id=$1 and id=$2 and purged_at is null`, uuid(workspace), uuid(source), policy)
	if err != nil {
		return translate(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		var purged bool
		if err := s.db.DB(ctx).QueryRow(ctx, `select purged_at is not null from source.source where workspace_id=$1 and id=$2`, uuid(workspace), uuid(source)).Scan(&purged); err != nil {
			return translate(ctx, err)
		}
		if purged {
			return domain.ErrPurged
		}
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) SetPublication(ctx context.Context, workspace, source, editor id.ID, publishedAt *time.Time, at time.Time) error {
	tag, err := s.db.DB(ctx).Exec(ctx, `update source.source set published_at=$3 where workspace_id=$1 and id=$2 and purged_at is null`, uuid(workspace), uuid(source), publishedAt)
	if err != nil {
		return translate(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		var purged bool
		if err := s.db.DB(ctx).QueryRow(ctx, `select purged_at is not null from source.source where workspace_id=$1 and id=$2`, uuid(workspace), uuid(source)).Scan(&purged); err != nil {
			return translate(ctx, err)
		}
		if purged {
			return domain.ErrPurged
		}
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) LockForPurge(ctx context.Context, workspace, source id.ID) (domain.Summary, domain.PurgeDependencies, error) {
	held, err := scanSummary(s.db.DB(ctx).QueryRow(ctx, sourceSelect+`where s.workspace_id=$1 and s.id=$2 for update of s`, uuid(workspace), uuid(source)))
	if err != nil {
		return domain.Summary{}, domain.PurgeDependencies{}, translate(ctx, err)
	}
	var captures, observations, extractions, operations, proposals int64
	err = s.db.DB(ctx).QueryRow(ctx, `select
		(select count(*) from source.capture where workspace_id=$1 and source_id=$2),
		(select count(*) from observation.manual where workspace_id=$1 and source_id=$2),
		(select count(*) from source_extraction.extraction where workspace_id=$1 and source_id=$2),
		(select count(*) from assistance.operation where workspace_id=$1 and source_id=$2),
		(select count(*) from assistance.proposal where workspace_id=$1 and source_id=$2)`, uuid(workspace), uuid(source)).Scan(&captures, &observations, &extractions, &operations, &proposals)
	if err != nil {
		return domain.Summary{}, domain.PurgeDependencies{}, translate(ctx, err)
	}
	return held, domain.PurgeDependencies{Captures: int(captures), ManualObservations: int(observations), Extractions: int(extractions), AssistanceOperations: int(operations), AssistanceProposals: int(proposals)}, nil
}

func (s *Store) MarkPurged(ctx context.Context, workspace, source, actor id.ID, reason string, at time.Time) error {
	tag, err := s.db.DB(ctx).Exec(ctx, `update source.source set purged_at=$3,purged_by=$4,purge_reason=$5 where workspace_id=$1 and id=$2 and purged_at is null`, uuid(workspace), uuid(source), at, uuid(actor), reason)
	if err != nil {
		return translate(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		var purged bool
		if err := s.db.DB(ctx).QueryRow(ctx, `select purged_at is not null from source.source where workspace_id=$1 and id=$2`, uuid(workspace), uuid(source)).Scan(&purged); err != nil {
			return translate(ctx, err)
		}
		if purged {
			return domain.ErrPurged
		}
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) NextVersion(ctx context.Context, workspace, source id.ID) (int, error) {
	var locked pgtype.UUID
	var purged bool
	if err := s.db.DB(ctx).QueryRow(ctx, `select id,purged_at is not null from source.source where workspace_id=$1 and id=$2 for update`, uuid(workspace), uuid(source)).Scan(&locked, &purged); err != nil {
		return 0, translate(ctx, err)
	}
	if purged {
		return 0, domain.ErrPurged
	}
	var version int
	err := s.db.DB(ctx).QueryRow(ctx, `select coalesce(max(version),0)+1 from source.capture where workspace_id=$1 and source_id=$2`, uuid(workspace), uuid(source)).Scan(&version)
	return version, translate(ctx, err)
}

func (s *Store) DuplicatePolicy(ctx context.Context, workspace, source id.ID) (string, error) {
	var policy string
	err := s.db.DB(ctx).QueryRow(ctx, `select duplicate_policy from source.source where workspace_id=$1 and id=$2 and purged_at is null`, uuid(workspace), uuid(source)).Scan(&policy)
	return policy, translate(ctx, err)
}

func (s *Store) CaptureHashExists(ctx context.Context, workspace, source id.ID, hash string) (bool, error) {
	var exists bool
	err := s.db.DB(ctx).QueryRow(ctx, `select exists(select 1 from source.capture where workspace_id=$1 and source_id=$2 and sha256=$3)`, uuid(workspace), uuid(source), hash).Scan(&exists)
	return exists, translate(ctx, err)
}

// A lateral lookup supplies latest metadata without an extra round trip per
// source. Reference-only sources intentionally retain null capture columns.
const sourceSelect = `select s.id,s.workspace_id,s.title,s.origin,s.url,s.filename,s.published_at,s.duplicate_policy,s.created_by,s.created_at,s.retention_until,s.retention_updated_by,s.retention_updated_at,s.sensitivity,s.privacy_updated_by,s.privacy_updated_at,s.legal_hold,s.legal_hold_reason,s.purged_at,s.purged_by,s.purge_reason,
 c.id,c.version,c.media_type,c.sha256,c.bytes,c.captured_by,c.captured_at
 from source.source s left join lateral
 (select * from source.capture where workspace_id=s.workspace_id and source_id=s.id order by version desc limit 1) c on true `

type scanner interface{ Scan(...any) error }

func scanWatch(row scanner) (domain.Watch, error) {
	var out domain.Watch
	var workspace, source, lastCapture, updatedBy pgtype.UUID
	var nextRun, lastRun, leaseUntil pgtype.Timestamptz
	if err := row.Scan(&workspace, &source, &out.Enabled, &out.IntervalSeconds, &nextRun, &lastRun, &out.LastStatus, &lastCapture, &out.LastError, &out.LeaseOwner, &leaseUntil, &updatedBy, &out.UpdatedAt); err != nil {
		return domain.Watch{}, err
	}
	out.WorkspaceID, out.SourceID = id.ID(workspace.Bytes), id.ID(source.Bytes)
	if nextRun.Valid {
		value := nextRun.Time
		out.NextRunAt = &value
	}
	if lastRun.Valid {
		value := lastRun.Time
		out.LastRunAt = &value
	}
	if lastCapture.Valid {
		out.LastCaptureID = id.ID(lastCapture.Bytes)
	}
	if leaseUntil.Valid {
		value := leaseUntil.Time
		out.LeaseUntil = &value
	}
	if updatedBy.Valid {
		out.UpdatedBy = id.ID(updatedBy.Bytes)
	}
	return out, nil
}

func scanAlert(row scanner) (domain.AlertRow, error) {
	var out domain.AlertRow
	var alert, workspace, source, capture, question, record, cluster, createdBy pgtype.UUID
	var seenAt pgtype.Timestamptz
	if err := row.Scan(&alert, &workspace, &source, &capture, &question, &record, &cluster, &out.Kind, &out.Title, &out.Detail, &out.DedupeKey, &out.Active, &createdBy, &out.CreatedAt, &out.SourceTitle, &seenAt); err != nil {
		return domain.AlertRow{}, err
	}
	out.ID, out.WorkspaceID = id.ID(alert.Bytes), id.ID(workspace.Bytes)
	if source.Valid {
		value := id.ID(source.Bytes)
		out.SourceID = &value
	}
	if capture.Valid {
		value := id.ID(capture.Bytes)
		out.CaptureID = &value
	}
	if question.Valid {
		value := id.ID(question.Bytes)
		out.QuestionID = &value
	}
	if record.Valid {
		value := id.ID(record.Bytes)
		out.RecordID = &value
	}
	if cluster.Valid {
		value := id.ID(cluster.Bytes)
		out.ClusterID = &value
	}
	if createdBy.Valid {
		out.CreatedBy = id.ID(createdBy.Bytes)
	}
	if seenAt.Valid {
		value := seenAt.Time
		out.SeenAt = &value
	}
	return out, nil
}

func scanSummary(row scanner) (domain.Summary, error) {
	var out domain.Summary
	var want, workspace, author, retentionBy, privacyBy, capID, capAuthor, purgedBy pgtype.UUID
	var version pgtype.Int4
	var media, hash, duplicatePolicy, sensitivity, legalHoldReason, purgeReason pgtype.Text
	var size pgtype.Int8
	var publishedAt, retentionUntil, retentionUpdatedAt, privacyUpdatedAt, purgedAt, capturedAt pgtype.Timestamptz
	var legalHold pgtype.Bool
	err := row.Scan(&want, &workspace, &out.Title, &out.Origin, &out.URL, &out.Filename, &publishedAt, &duplicatePolicy, &author, &out.CreatedAt, &retentionUntil, &retentionBy, &retentionUpdatedAt, &sensitivity, &privacyBy, &privacyUpdatedAt, &legalHold, &legalHoldReason, &purgedAt, &purgedBy, &purgeReason, &capID, &version, &media, &hash, &size, &capAuthor, &capturedAt)
	if err != nil {
		return domain.Summary{}, err
	}
	out.ID, out.WorkspaceID, out.CreatedBy = id.ID(want.Bytes), id.ID(workspace.Bytes), id.ID(author.Bytes)
	if publishedAt.Valid {
		value := publishedAt.Time
		out.PublishedAt = &value
	}
	out.DuplicatePolicy = duplicatePolicy.String
	if retentionUntil.Valid {
		value := retentionUntil.Time
		out.RetentionUntil = &value
	}
	if retentionBy.Valid {
		out.RetentionUpdatedBy = id.ID(retentionBy.Bytes)
	}
	if retentionUpdatedAt.Valid {
		value := retentionUpdatedAt.Time
		out.RetentionUpdatedAt = &value
	}
	out.Sensitivity = sensitivity.String
	if privacyBy.Valid {
		out.PrivacyUpdatedBy = id.ID(privacyBy.Bytes)
	}
	if privacyUpdatedAt.Valid {
		value := privacyUpdatedAt.Time
		out.PrivacyUpdatedAt = &value
	}
	out.LegalHold = legalHold.Bool
	out.LegalHoldReason = legalHoldReason.String
	if purgedAt.Valid {
		value := purgedAt.Time
		out.PurgedAt = &value
	}
	if purgedBy.Valid {
		out.PurgedBy = id.ID(purgedBy.Bytes)
	}
	out.PurgeReason = purgeReason.String
	if capID.Valid {
		out.LatestCapture = &domain.Capture{ID: id.ID(capID.Bytes), SourceID: out.ID, WorkspaceID: out.WorkspaceID, Version: int(version.Int32), MediaType: media.String, SHA256: hash.String, Bytes: size.Int64, CapturedBy: id.ID(capAuthor.Bytes), CapturedAt: capturedAt.Time}
	}
	return out, nil
}
func (s *Store) Page(ctx context.Context, workspace, before id.ID, query string, limit int, maxSensitivity string) ([]domain.Summary, error) {
	rows, err := s.db.DB(ctx).Query(ctx, sourceSelect+`where s.workspace_id=$1 and ($2::uuid is null or s.id < $2)
  and ($3::text = '' or position(lower($3) in lower(s.id::text)) > 0
    or position(lower($3) in lower(s.title)) > 0
    or position(lower($3) in lower(coalesce(s.url, ''))) > 0
    or position(lower($3) in lower(coalesce(s.filename, ''))) > 0
    or position(lower($3) in lower(s.origin)) > 0
    or position(lower($3) in lower(s.sensitivity)) > 0
    or exists (select 1 from source.search_document d
       where d.workspace_id=s.workspace_id and d.source_id=s.id
         and d.search_vector @@ plainto_tsquery('simple',$3)))
  and (s.sensitivity='public' or ($4='internal' and s.sensitivity='internal') or $4='restricted')
  order by s.id desc limit $5`, uuid(workspace), uuid(before), query, maxSensitivity, limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Summary, 0)
	for rows.Next() {
		row, err := scanSummary(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, row)
	}
	return out, translate(ctx, rows.Err())
}

const intakeSelect = `select id,workspace_id,title,origin,url,filename,media_type,content_bytes,note,created_by,created_at,status,reviewed_by,reviewed_at,review_note,source_id from source.intake_candidate `

func scanIntake(row scanner) (domain.IntakeCandidate, error) {
	var out domain.IntakeCandidate
	var want, workspace, author, reviewedBy, source pgtype.UUID
	var reviewedAt pgtype.Timestamptz
	var content []byte
	if err := row.Scan(&want, &workspace, &out.Title, &out.Origin, &out.URL, &out.Filename, &out.MediaType, &content, &out.Note, &author, &out.CreatedAt, &out.Status, &reviewedBy, &reviewedAt, &out.ReviewNote, &source); err != nil {
		return domain.IntakeCandidate{}, err
	}
	out.ID, out.WorkspaceID, out.CreatedBy = id.ID(want.Bytes), id.ID(workspace.Bytes), id.ID(author.Bytes)
	if reviewedBy.Valid {
		out.ReviewedBy = id.ID(reviewedBy.Bytes)
	}
	if reviewedAt.Valid {
		value := reviewedAt.Time
		out.ReviewedAt = &value
	}
	if source.Valid {
		out.SourceID = id.ID(source.Bytes)
	}
	if content != nil {
		out.ContentBytes = append([]byte(nil), content...)
	}
	return out, nil
}

func (s *Store) IntakePage(ctx context.Context, workspace, before id.ID, status string, limit int) ([]domain.IntakeCandidate, error) {
	rows, err := s.db.DB(ctx).Query(ctx, intakeSelect+`where workspace_id=$1 and ($2::uuid is null or id < $2) and ($3::text = '' or status=$3) order by id desc limit $4`, uuid(workspace), uuid(before), status, limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.IntakeCandidate, 0)
	for rows.Next() {
		candidate, err := scanIntake(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, candidate)
	}
	return out, translate(ctx, rows.Err())
}

func (s *Store) Search(ctx context.Context, workspace, before id.ID, query string, limit int, maxSensitivity string) ([]domain.SearchRow, error) {
	rows, err := s.db.DB(ctx).Query(ctx, `select d.id,d.workspace_id,d.source_id,s.title,d.capture_id,c.version,c.media_type,d.extraction_id,coalesce(e.method,''),d.content_sha256,d.content_bytes
from source.search_document d
join source.source s on s.workspace_id=d.workspace_id and s.id=d.source_id
join source.capture c on c.workspace_id=d.workspace_id and c.source_id=d.source_id and c.id=d.capture_id
left join source_extraction.extraction e on e.workspace_id=d.workspace_id and e.source_id=d.source_id and e.capture_id=d.capture_id and e.id=d.extraction_id
where d.workspace_id=$1 and s.purged_at is null and d.search_vector @@ plainto_tsquery('simple',$2)
  and (s.sensitivity='public' or ($5='internal' and s.sensitivity='internal') or $5='restricted')
  and ($3::uuid is null or d.id < $3)
order by d.id desc limit $4`, uuid(workspace), query, uuid(before), limit, maxSensitivity)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.SearchRow, 0)
	for rows.Next() {
		var row domain.SearchRow
		var document, space, source, capture, extraction pgtype.UUID
		if err := rows.Scan(&document, &space, &source, &row.SourceTitle, &capture, &row.CaptureVersion, &row.MediaType, &extraction, &row.ExtractionMethod, &row.ContentSHA256, &row.ContentBytes); err != nil {
			return nil, translate(ctx, err)
		}
		row.ID, row.WorkspaceID, row.SourceID, row.CaptureID = id.ID(document.Bytes), id.ID(space.Bytes), id.ID(source.Bytes), id.ID(capture.Bytes)
		if extraction.Valid {
			row.ExtractionID = id.ID(extraction.Bytes)
		}
		out = append(out, row)
	}
	return out, translate(ctx, rows.Err())
}
func (s *Store) ByID(ctx context.Context, workspace, source id.ID) (domain.Summary, error) {
	out, err := scanSummary(s.db.DB(ctx).QueryRow(ctx, sourceSelect+`where s.workspace_id=$1 and s.id=$2`, uuid(workspace), uuid(source)))
	return out, translate(ctx, err)
}

const retentionStateExpression = `case
	when s.purged_at is not null then 'purged'
	when s.legal_hold then 'held'
	when s.retention_until is null then 'unscheduled'
	when s.retention_until > $4 then 'scheduled'
	when observations.count > 0 or extractions.count > 0 or operations.count > 0 or proposals.count > 0 then 'blocked'
	else 'due'
end`

const retentionSelect = `select s.id,s.workspace_id,s.title,s.origin,s.url,s.filename,s.published_at,s.duplicate_policy,s.created_by,s.created_at,s.retention_until,s.retention_updated_by,s.retention_updated_at,s.sensitivity,s.privacy_updated_by,s.privacy_updated_at,s.legal_hold,s.legal_hold_reason,s.purged_at,s.purged_by,s.purge_reason,
	captures.count,observations.count,extractions.count,operations.count,proposals.count
from source.source s
left join lateral (select count(*)::bigint as count from source.capture where workspace_id=s.workspace_id and source_id=s.id) captures on true
left join lateral (select count(*)::bigint as count from observation.manual where workspace_id=s.workspace_id and source_id=s.id) observations on true
left join lateral (select count(*)::bigint as count from source_extraction.extraction where workspace_id=s.workspace_id and source_id=s.id) extractions on true
left join lateral (select count(*)::bigint as count from assistance.operation where workspace_id=s.workspace_id and source_id=s.id) operations on true
left join lateral (select count(*)::bigint as count from assistance.proposal where workspace_id=s.workspace_id and source_id=s.id) proposals on true `

func scanRetention(row scanner, now time.Time) (domain.RetentionQueueItem, error) {
	var source domain.Source
	var want, workspace, author, retentionBy, privacyBy, purgedBy pgtype.UUID
	var publishedAt, retentionUntil, retentionUpdatedAt, privacyUpdatedAt, purgedAt pgtype.Timestamptz
	var duplicatePolicy, sensitivity, legalHoldReason, purgeReason pgtype.Text
	var legalHold pgtype.Bool
	var captures, observations, extractions, operations, proposals int64
	err := row.Scan(&want, &workspace, &source.Title, &source.Origin, &source.URL, &source.Filename, &publishedAt, &duplicatePolicy, &author, &source.CreatedAt, &retentionUntil, &retentionBy, &retentionUpdatedAt, &sensitivity, &privacyBy, &privacyUpdatedAt, &legalHold, &legalHoldReason, &purgedAt, &purgedBy, &purgeReason, &captures, &observations, &extractions, &operations, &proposals)
	if err != nil {
		return domain.RetentionQueueItem{}, err
	}
	source.ID, source.WorkspaceID, source.CreatedBy = id.ID(want.Bytes), id.ID(workspace.Bytes), id.ID(author.Bytes)
	if publishedAt.Valid {
		value := publishedAt.Time
		source.PublishedAt = &value
	}
	source.DuplicatePolicy = duplicatePolicy.String
	if retentionUntil.Valid {
		value := retentionUntil.Time
		source.RetentionUntil = &value
	}
	if retentionBy.Valid {
		source.RetentionUpdatedBy = id.ID(retentionBy.Bytes)
	}
	if retentionUpdatedAt.Valid {
		value := retentionUpdatedAt.Time
		source.RetentionUpdatedAt = &value
	}
	source.Sensitivity = sensitivity.String
	if privacyBy.Valid {
		source.PrivacyUpdatedBy = id.ID(privacyBy.Bytes)
	}
	if privacyUpdatedAt.Valid {
		value := privacyUpdatedAt.Time
		source.PrivacyUpdatedAt = &value
	}
	source.LegalHold = legalHold.Bool
	source.LegalHoldReason = legalHoldReason.String
	if purgedAt.Valid {
		value := purgedAt.Time
		source.PurgedAt = &value
	}
	if purgedBy.Valid {
		source.PurgedBy = id.ID(purgedBy.Bytes)
	}
	source.PurgeReason = purgeReason.String
	dependencies := domain.PurgeDependencies{Captures: int(captures), ManualObservations: int(observations), Extractions: int(extractions), AssistanceOperations: int(operations), AssistanceProposals: int(proposals)}
	return domain.RetentionQueueItem{Source: source, Review: source.ReviewPurge(now, dependencies)}, nil
}

func (s *Store) RetentionPage(ctx context.Context, workspace, before id.ID, limit int, state string, now time.Time) ([]domain.RetentionQueueItem, error) {
	rows, err := s.db.DB(ctx).Query(ctx, retentionSelect+`where s.workspace_id=$1 and ($2::uuid is null or s.id < $2) and ($3='' or (`+retentionStateExpression+`=$3)) order by s.id desc limit $5`, uuid(workspace), uuid(before), state, now, limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.RetentionQueueItem, 0)
	for rows.Next() {
		item, err := scanRetention(rows, now)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, item)
	}
	return out, translate(ctx, rows.Err())
}

const captureSelect = `select id,source_id,workspace_id,version,media_type,sha256,bytes,captured_by,captured_at from source.capture `

func scanCapture(row scanner) (domain.Capture, error) {
	var out domain.Capture
	var want, source, workspace, author pgtype.UUID
	err := row.Scan(&want, &source, &workspace, &out.Version, &out.MediaType, &out.SHA256, &out.Bytes, &author, &out.CapturedAt)
	out.ID, out.SourceID, out.WorkspaceID, out.CapturedBy = id.ID(want.Bytes), id.ID(source.Bytes), id.ID(workspace.Bytes), id.ID(author.Bytes)
	return out, err
}
func (s *Store) Captures(ctx context.Context, workspace, source id.ID) ([]domain.Capture, error) {
	rows, err := s.db.DB(ctx).Query(ctx, captureSelect+`where workspace_id=$1 and source_id=$2 order by version desc`, uuid(workspace), uuid(source))
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Capture, 0)
	for rows.Next() {
		row, err := scanCapture(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, row)
	}
	return out, translate(ctx, rows.Err())
}
func (s *Store) Capture(ctx context.Context, workspace, source, capture id.ID) (domain.Capture, error) {
	out, err := scanCapture(s.db.DB(ctx).QueryRow(ctx, captureSelect+`where workspace_id=$1 and source_id=$2 and id=$3`, uuid(workspace), uuid(source), uuid(capture)))
	return out, translate(ctx, err)
}
