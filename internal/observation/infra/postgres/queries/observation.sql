-- name: InsertObservation :exec
insert into observation.observation (
    id, workspace_id, subject_kind, subject_value, field, value,
    invocation_id, artifact_id, mapping_id, mapping_version,
    observed_at, recorded_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12);

-- name: ObservationByID :one
select id, workspace_id, subject_kind, subject_value, field, value,
       invocation_id, artifact_id, mapping_id, mapping_version,
       observed_at, recorded_at
from observation.observation where id = $1 and workspace_id = $2;

-- name: ObservationsForInvocation :many
select id, workspace_id, subject_kind, subject_value, field, value,
       invocation_id, artifact_id, mapping_id, mapping_version,
       observed_at, recorded_at
from observation.observation
where invocation_id = $1 and workspace_id = $2
order by subject_value, field, observed_at desc
limit sqlc.arg(page)::int;

-- name: ObservationsForSubject :many
-- The asset drawer's read: everything known about one subject. NOT deduplicated
-- — two runs a day apart are two statements, and collapsing them loses the
-- second date. "The state of each field" is the caller taking the first row per
-- field off this, which is what the ordering is for.
select id, workspace_id, subject_kind, subject_value, field, value,
       invocation_id, artifact_id, mapping_id, mapping_version,
       observed_at, recorded_at
from observation.observation
where workspace_id = $1 and subject_kind = $2 and subject_value = $3
order by field, observed_at desc
limit sqlc.arg(page)::int;

-- name: SubjectsForWorkspace :many
-- Distinct subjects, with how much is known about each. This is what stands in
-- for an asset list until `fragment` exists — and it is deliberately a GROUP BY
-- over observations rather than a table, because that is what a fragment will
-- be a dedup of.
select subject_kind, subject_value,
       count(*)::int as observations,
       count(distinct field)::int as fields,
       max(observed_at)::timestamptz as last_seen
from observation.observation
where workspace_id = $1
group by subject_kind, subject_value
order by max(observed_at) desc
limit sqlc.arg(page)::int;

-- name: InsertUnmapped :exec
insert into observation.unmapped (
    id, workspace_id, invocation_id, artifact_id, path, seen, sample, recorded_at
) values ($1, $2, $3, $4, $5, $6, $7, $8)
-- Re-extracting the same bytes must not double the LEFT ALONE count. The count
-- is REPLACED rather than added to, because it is a property of the artifact and
-- not of how many times it was read.
on conflict (artifact_id, path) do update
set seen = excluded.seen, sample = excluded.sample, recorded_at = excluded.recorded_at;

-- name: UnmappedForInvocation :many
select id, workspace_id, invocation_id, artifact_id, path, seen, sample, recorded_at
from observation.unmapped
where invocation_id = $1 and workspace_id = $2
order by seen desc, path;

-- name: ExtractionQuality :one
-- The numbers the whole correction loop is measured by, returned TOGETHER
-- because a ratio without its denominator is what 0011 refuses.
--
-- **MAPPED IS COUNTED IN PATHS, NOT IN OBSERVATIONS.** It was `count(*)` for an
-- hour, which made `FIELDS SEEN = MAPPED + LEFT ALONE` false the moment a
-- `.tech[]` flatten produced two values from one path — the walk showed
-- `FIELDS SEEN 9` for a record with eight distinct paths. `distinct mapping_id`
-- is the right count: a mapping that matched nothing produced no observations
-- and correctly does not appear, which is what the draft's "22 became
-- observations" means.
select
    (select count(distinct o.mapping_id)::int from observation.observation o
     where o.workspace_id = sqlc.arg(workspace)::uuid
       and o.invocation_id = sqlc.arg(invocation)::uuid) as mapped,
    (select count(*)::int from observation.observation o
     where o.workspace_id = sqlc.arg(workspace)::uuid
       and o.invocation_id = sqlc.arg(invocation)::uuid) as observations,
    (select count(*)::int from observation.unmapped u
     where u.workspace_id = sqlc.arg(workspace)::uuid
       and u.invocation_id = sqlc.arg(invocation)::uuid) as left_alone,
    (select count(distinct o.field)::int from observation.observation o
     where o.workspace_id = sqlc.arg(workspace)::uuid
       and o.invocation_id = sqlc.arg(invocation)::uuid) as fields;

-- name: SubjectsPerInvocation :many
-- What each invocation actually SAID something about — coverage's second
-- source, decisions/0037.
--
-- The first source is what an invocation was AIMED AT, which is what keeps
-- `never checked` and `found nothing` apart. This one catches DISCOVERY: a tool
-- pointed at a seed produces subjects nobody aimed at, and the check that found
-- them has plainly looked at them.
select invocation_id, subject_kind, subject_value,
       max(observed_at)::timestamptz as last_seen
from observation.observation
where workspace_id = $1
group by invocation_id, subject_kind, subject_value;
