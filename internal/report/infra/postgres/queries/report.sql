-- name: InsertReport :exec
insert into report.report (
    id, workspace_id, target_id, title, prepared_by,
    period_start, period_end, revisions, created_by, created_at, updated_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: ReportByID :one
-- BOTH IDS. A report read without its engagement is another client's.
select id, workspace_id, target_id, title, prepared_by, period_start, period_end,
       revisions, created_by, created_at, updated_at
from report.report where id = $1 and workspace_id = $2;

-- name: ReportsForTarget :many
select id, workspace_id, target_id, title, prepared_by, period_start, period_end,
       revisions, created_by, created_at, updated_at
from report.report
where workspace_id = $1
  and (sqlc.narg(target)::uuid is null or target_id = sqlc.narg(target)::uuid)
order by created_at desc
limit sqlc.arg(page)::int;

-- name: SaveReport :execrows
-- Everything a person can edit. `revisions` moves here too, because issuing
-- increments it in the same transaction that writes the revision — a count that
-- could drift from the rows it counts is a count nobody can trust.
update report.report
set title = $3, prepared_by = $4, period_start = $5, period_end = $6,
    revisions = $7, updated_at = $8
where id = $1 and workspace_id = $2;

-- name: SetSection :exec
insert into report.section (report_id, section, enabled)
values ($1, $2, $3)
on conflict (report_id, section) do update set enabled = excluded.enabled;

-- name: SectionsForReport :many
select section, enabled from report.section where report_id = $1;

-- name: InsertRevision :exec
insert into report.revision (
    id, report_id, workspace_id, number, hash, bytes, media_type,
    sections, issued_by, issued_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: NextRevisionNumber :one
-- coalesce, because the first revision of a report has no predecessor and max()
-- over no rows is null rather than zero.
select coalesce(max(number), 0)::int + 1 from report.revision where report_id = $1;

-- name: RevisionsForReport :many
select id, report_id, workspace_id, number, hash, bytes, media_type,
       sections, issued_by, issued_at
from report.revision where report_id = $1 order by number desc;

-- name: RevisionByID :one
-- Read by WORKSPACE and not through its report, because a client reaching a
-- delivered document must not need to read the configuration that produced it.
select id, report_id, workspace_id, number, hash, bytes, media_type,
       sections, issued_by, issued_at
from report.revision where id = $1 and workspace_id = $2;
