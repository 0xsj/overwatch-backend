-- name: InsertCheck :exec
insert into checks.check (
    id, org_id, name, question, applies_to, interval_seconds, enabled,
    status, version, created_by, created_at, updated_at, archived_at, human
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14);

-- name: CheckByID :one
-- BOTH IDS. A check read without its org is another firm's.
select id, org_id, name, question, applies_to, interval_seconds, enabled,
       status, version, created_by, created_at, updated_at, archived_at, human
from checks.check where id = $1 and org_id = $2;

-- name: ChecksForOrg :many
select id, org_id, name, question, applies_to, interval_seconds, enabled,
       status, version, created_by, created_at, updated_at, archived_at, human
from checks.check
where org_id = $1 and (sqlc.arg(include_archived)::bool or status <> 'archived')
order by lower(name);

-- name: UpdateCheck :execrows
-- The column list must match what the domain can change.
update checks.check
set name = $3, question = $4, applies_to = $5, interval_seconds = $6,
    enabled = $7, status = $8, version = $9, updated_at = $10, archived_at = $11,
    human = $12
where id = $1 and org_id = $2 and version = $13;

-- name: StepsForCheck :many
select id, check_id, tool_id, x, y, pinned
from checks.step where check_id = $1 order by id;

-- name: FlowsForCheck :many
select check_id, from_step, to_step
from checks.flow where check_id = $1 order by from_step, to_step;

-- name: InsertStep :exec
insert into checks.step (id, check_id, tool_id, x, y, pinned)
values ($1, $2, $3, $4, $5, $6);

-- name: UpdateStep :execrows
-- A step's TOOL can change and its ID cannot — that is the whole reason a save
-- is a diff rather than a delete-and-insert.
update checks.step set tool_id = $3, x = $4, y = $5, pinned = $6
where id = $1 and check_id = $2;

-- name: DeleteStepsExcept :exec
-- Removing a step removes its flows too, and this runs BEFORE the flow rewrite
-- so an orphan edge never exists even inside the transaction.
delete from checks.step where check_id = $1 and not (id = any(sqlc.arg(keep)::uuid[]));

-- name: DeleteFlowsForCheck :exec
-- Flows ARE replaced wholesale, unlike steps. Nothing cites an edge — an
-- invocation names a step — so there is no identity to preserve, and a diff
-- would be more code for a set of two-column rows.
delete from checks.flow where check_id = $1;

-- name: InsertFlow :exec
insert into checks.flow (check_id, from_step, to_step) values ($1, $2, $3);

-- name: ChecksUsingTool :many
-- What archiving a tool has to ask, and the reason the chain is rows rather than
-- a jsonb column — 0032. A blob answers this with a scan and a parse.
select distinct c.id, c.name
from checks.check c
join checks.step s on s.check_id = c.id
where c.org_id = $1 and s.tool_id = $2 and c.status <> 'archived'
order by c.name;

-- name: SchedulableChecks :many
-- Every check that a schedule could start, ACROSS ALL ORGS — decisions/0038 §4.
--
-- The scheduler is driven from here because checks are the smallest set: a firm
-- has six, not six thousand targets. The three exclusions are three different
-- facts, and none of them is "this check is broken":
--
--   not enabled     the standing authorisation is withdrawn
--   human           nothing spawns; a person reading it is the whole act
--   no interval     it runs when somebody asks, not on a clock
select id, org_id, name, question, applies_to, interval_seconds, enabled,
       status, version, created_by, created_at, updated_at, archived_at, human
from checks.check
where status <> 'archived'
  and enabled
  and not human
  and interval_seconds is not null
order by org_id, lower(name);
