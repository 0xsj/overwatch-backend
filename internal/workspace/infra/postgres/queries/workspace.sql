-- name: InsertWorkspace :exec
insert into workspace.workspace (
    id, org_id, name, status, version, created_at, updated_at, archived_at, source_event_id
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: WorkspaceByID :one
select id, org_id, name, status, version, created_at, updated_at, archived_at, source_event_id
from workspace.workspace where id = $1;

-- name: WorkspacesForOrg :many
select id, org_id, name, status, version, created_at, updated_at, archived_at, source_event_id
from workspace.workspace
where org_id = $1 and status <> 'archived'
order by created_at;

-- name: UpdateWorkspace :execrows
update workspace.workspace
set name = $2, status = $3, version = $4, updated_at = $5, archived_at = $6
where id = $1 and version = $7;

-- name: WorkspaceBySourceEvent :one
select id, org_id, name, status, version, created_at, updated_at, archived_at, source_event_id
from workspace.workspace where source_event_id = $1;
