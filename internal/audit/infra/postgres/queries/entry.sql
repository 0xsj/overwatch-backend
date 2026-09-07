-- name: InsertEntry :execrows
insert into audit.entry (
    id, event_id, scope, action, subject, actor, on_behalf_of, workspace_id,
    correlation_id, causation_id, detail, occurred_at, recorded_at, org_id
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
on conflict (event_id) do nothing;

-- name: EntryByID :one
select id, event_id, scope, action, subject, actor, on_behalf_of, workspace_id,
       correlation_id, causation_id, detail, occurred_at, recorded_at, org_id
from audit.entry
where id = $1;

-- name: EntriesForSubject :many
select id, event_id, scope, action, subject, actor, on_behalf_of, workspace_id,
       correlation_id, causation_id, detail, occurred_at, recorded_at, org_id
from audit.entry
where subject = $1
order by occurred_at desc
limit $2;

-- name: EntriesForActor :many
select id, event_id, scope, action, subject, actor, on_behalf_of, workspace_id,
       correlation_id, causation_id, detail, occurred_at, recorded_at, org_id
from audit.entry
where actor = $1
order by occurred_at desc
limit $2;

-- name: EntriesForWorkspace :many
select id, event_id, scope, action, subject, actor, on_behalf_of, workspace_id,
       correlation_id, causation_id, detail, occurred_at, recorded_at, org_id
from audit.entry
where workspace_id = $1
order by occurred_at desc
limit $2;

-- name: EntriesForSubjectPage :many
select id, event_id, scope, action, subject, actor, on_behalf_of, workspace_id,
       correlation_id, causation_id, detail, occurred_at, recorded_at, org_id
from audit.entry
where subject = $1
  and ($2::timestamptz is null or (occurred_at, id) < ($2::timestamptz, $3::uuid))
order by occurred_at desc, id desc
limit $4;

-- name: EntriesForWorkspacePage :many
select id, event_id, scope, action, subject, actor, on_behalf_of, workspace_id,
       correlation_id, causation_id, detail, occurred_at, recorded_at, org_id
from audit.entry
where workspace_id = $1
  and ($2::timestamptz is null or (occurred_at, id) < ($2::timestamptz, $3::uuid))
order by occurred_at desc, id desc
limit $4;

-- name: FacetsForSubject :many
-- The action's FIRST SEGMENT, which is what the mock's facet row counts. It is
-- computed rather than stored because the action is the fact and the facet is a
-- reading of it: a stored column would be a second spelling that drifts the
-- first time an action is renamed.
select split_part(action, '.', 1) as facet, count(*) as total
from audit.entry
where subject = $1
group by 1
order by 2 desc, 1;

-- name: FacetsForWorkspace :many
select split_part(action, '.', 1) as facet, count(*) as total
from audit.entry
where workspace_id = $1
group by 1
order by 2 desc, 1;

-- name: EntriesForSubjectFacetPage :many
select id, event_id, scope, action, subject, actor, on_behalf_of, workspace_id,
       correlation_id, causation_id, detail, occurred_at, recorded_at, org_id
from audit.entry
where subject = $1
  and split_part(action, '.', 1) = $2
  and ($3::timestamptz is null or (occurred_at, id) < ($3::timestamptz, $4::uuid))
order by occurred_at desc, id desc
limit $5;

-- name: EntriesForWorkspaceFacetPage :many
select id, event_id, scope, action, subject, actor, on_behalf_of, workspace_id,
       correlation_id, causation_id, detail, occurred_at, recorded_at, org_id
from audit.entry
where workspace_id = $1
  and split_part(action, '.', 1) = $2
  and ($3::timestamptz is null or (occurred_at, id) < ($3::timestamptz, $4::uuid))
order by occurred_at desc, id desc
limit $5;

-- name: EntriesForOrgPage :many
select id, event_id, scope, action, subject, actor, on_behalf_of, workspace_id,
       correlation_id, causation_id, detail, occurred_at, recorded_at, org_id
from audit.entry
where org_id = $1
  and ($2::timestamptz is null or (occurred_at, id) < ($2::timestamptz, $3::uuid))
order by occurred_at desc, id desc
limit $4;

-- name: FacetsForOrg :many
select split_part(action, '.', 1) as facet, count(*) as total
from audit.entry
where org_id = $1
group by 1
order by 2 desc, 1;
