-- name: InsertEntry :execrows
insert into audit.entry (
    id, event_id, scope, action, subject, actor, on_behalf_of, workspace_id,
    correlation_id, causation_id, detail, occurred_at, recorded_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
on conflict (event_id) do nothing;

-- name: EntryByID :one
select id, event_id, scope, action, subject, actor, on_behalf_of, workspace_id,
       correlation_id, causation_id, detail, occurred_at, recorded_at
from audit.entry
where id = $1;

-- name: EntriesForSubject :many
select id, event_id, scope, action, subject, actor, on_behalf_of, workspace_id,
       correlation_id, causation_id, detail, occurred_at, recorded_at
from audit.entry
where subject = $1
order by occurred_at desc
limit $2;

-- name: EntriesForActor :many
select id, event_id, scope, action, subject, actor, on_behalf_of, workspace_id,
       correlation_id, causation_id, detail, occurred_at, recorded_at
from audit.entry
where actor = $1
order by occurred_at desc
limit $2;

-- name: EntriesForWorkspace :many
select id, event_id, scope, action, subject, actor, on_behalf_of, workspace_id,
       correlation_id, causation_id, detail, occurred_at, recorded_at
from audit.entry
where workspace_id = $1
order by occurred_at desc
limit $2;
