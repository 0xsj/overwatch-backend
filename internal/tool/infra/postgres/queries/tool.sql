-- name: InsertTool :exec
insert into tool.tool (
    id, org_id, name, argv, intensity, status, version,
    created_by, created_at, updated_at, archived_at, consumes, produces,
    success_exit_codes
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14);

-- name: ToolByID :one
-- BOTH IDS. A tool read without its org is another firm's, and a signature that
-- cannot express the mistake is worth more than a rule about remembering.
select id, org_id, name, argv, intensity, status, version,
       created_by, created_at, updated_at, archived_at, consumes, produces,
       success_exit_codes
from tool.tool where id = $1 and org_id = $2;

-- name: ToolsForOrg :many
select id, org_id, name, argv, intensity, status, version,
       created_by, created_at, updated_at, archived_at, consumes, produces,
       success_exit_codes
from tool.tool
where org_id = $1 and (sqlc.arg(include_archived)::bool or status <> 'archived')
order by lower(name);

-- name: UpdateTool :execrows
-- The column list must match what the domain can change. It did not once, in
-- identity, and the command returned a correctly-moved record having moved
-- nothing.
update tool.tool
set argv = $3, intensity = $4, status = $5, version = $6,
    updated_at = $7, archived_at = $8, consumes = $9, produces = $10,
    success_exit_codes = $11
where id = $1 and org_id = $2 and version = $12;

-- name: InsertMapping :exec
insert into tool.mapping (
    id, org_id, tool_id, field, expression, version, state,
    created_by, created_at, promoted_at, retired_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: MappingByID :one
select id, org_id, tool_id, field, expression, version, state,
       created_by, created_at, promoted_at, retired_at
from tool.mapping where id = $1 and org_id = $2;

-- name: MappingsForTool :many
select id, org_id, tool_id, field, expression, version, state,
       created_by, created_at, promoted_at, retired_at
from tool.mapping where tool_id = $1 and org_id = $2
order by field, version desc;

-- name: LiveMappingFor :one
select id, org_id, tool_id, field, expression, version, state,
       created_by, created_at, promoted_at, retired_at
from tool.mapping where tool_id = $1 and field = $2 and state = 'live';

-- name: NextMappingVersion :one
-- coalesce, because the first version of a field has no predecessor and max()
-- over no rows is null rather than zero.
select coalesce(max(version), 0)::int + 1
from tool.mapping where tool_id = $1 and field = $2;

-- name: SetMappingState :execrows
-- The ONLY update this table takes. It moves the two lifecycle columns and
-- nothing else — the expression is what citations point at.
update tool.mapping
set state = $3, promoted_at = $4, retired_at = $5
where id = $1 and org_id = $2;
