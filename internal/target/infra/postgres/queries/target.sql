-- name: InsertTarget :exec
insert into target.target (
    id, workspace_id, name, kind, status, version, created_by,
    created_at, updated_at, archived_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: TargetByID :one
-- BOTH ids, always. The workspace is not a filter here — it is the tenancy
-- check, and a lookup by id alone would return another engagement's target to a
-- caller who guessed one.
select id, workspace_id, name, kind, status, version, created_by,
       created_at, updated_at, archived_at, root_entity_id
from target.target where id = $1 and workspace_id = $2;

-- name: LiveTargetsForWorkspace :many
select id, workspace_id, name, kind, status, version, created_by,
       created_at, updated_at, archived_at, root_entity_id
from target.target
where workspace_id = $1 and status <> 'archived'
order by created_at;

-- name: AllTargetsForWorkspace :many
-- ARCHIVED ONES INCLUDED. The live read feeds the list; this one is what makes
-- an archived target reachable, and therefore reopenable — the same pair
-- workspace uses for ForOrg and AllForOrg.
select id, workspace_id, name, kind, status, version, created_by,
       created_at, updated_at, archived_at, root_entity_id
from target.target
where workspace_id = $1
order by created_at;

-- name: UpdateTarget :execrows
update target.target
set name = $3, status = $4, version = $5, updated_at = $6, archived_at = $7
where id = $1 and workspace_id = $2 and version = $8;

-- name: SetRootEntity :execrows
-- Written by a SUBSCRIBER on `entity.root.created`, and idempotent on
-- redelivery: the `is null` predicate means a second delivery affects no rows
-- rather than overwriting a root with itself and looking like a change.
update target.target
set root_entity_id = $2
where id = $1 and root_entity_id is null;
