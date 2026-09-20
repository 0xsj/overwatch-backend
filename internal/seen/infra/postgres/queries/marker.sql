-- name: MarkerByAccountWorkspace :one
select account_id, workspace_id, seen_at
from seen.marker
where account_id = $1 and workspace_id = $2;

-- name: UpsertMarker :exec
insert into seen.marker (account_id, workspace_id, seen_at)
values ($1, $2, $3)
on conflict (account_id, workspace_id) do update
set seen_at = excluded.seen_at;
