-- name: InsertRule :exec
insert into scope.rule (
    id, workspace_id, target_id, pattern, polarity, gate, kinds, tools,
    created_by, created_at, superseded_at, superseded_by
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12);

-- name: RuleByID :one
-- BOTH the workspace and the id, always. The workspace is the tenancy check and
-- not a filter — the same rule target's store follows.
select id, workspace_id, target_id, pattern, polarity, gate, kinds, tools,
       created_by, created_at, superseded_at, superseded_by
from scope.rule where id = $1 and workspace_id = $2;

-- name: LiveRulesForTarget :many
select id, workspace_id, target_id, pattern, polarity, gate, kinds, tools,
       created_by, created_at, superseded_at, superseded_by
from scope.rule
where target_id = $1 and workspace_id = $2 and superseded_at is null
order by created_at;

-- name: AllRulesForTarget :many
select id, workspace_id, target_id, pattern, polarity, gate, kinds, tools,
       created_by, created_at, superseded_at, superseded_by
from scope.rule
where target_id = $1 and workspace_id = $2
order by created_at desc;

-- name: SupersedeRule :execrows
-- The ONLY update in this schema, and it touches the two lifecycle columns and
-- nothing else — decisions/0030.
update scope.rule
set superseded_at = $3, superseded_by = $4
where id = $1 and workspace_id = $2 and superseded_at is null;
