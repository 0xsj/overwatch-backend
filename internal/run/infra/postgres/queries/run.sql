-- name: InsertRun :exec
insert into run.run (
    id, workspace_id, target_id, check_id, state, started_by,
    started_at, finished_at, version
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: RunByID :one
-- BOTH IDS. A run read without its engagement is another client's.
select id, workspace_id, target_id, check_id, state, started_by,
       started_at, finished_at, version
from run.run where id = $1 and workspace_id = $2;

-- name: RunsForWorkspace :many
-- Newest first, and PAGED BY A CURSOR rather than an offset — the same keyset
-- shape the audit ledger uses, because this list only grows.
select id, workspace_id, target_id, check_id, state, started_by,
       started_at, finished_at, version
from run.run
where workspace_id = $1
  and (sqlc.narg(target)::uuid is null or target_id = sqlc.narg(target)::uuid)
  and (sqlc.narg(before)::timestamptz is null
       or (started_at, id) < (sqlc.narg(before)::timestamptz, sqlc.narg(before_id)::uuid))
order by started_at desc, id desc
limit sqlc.arg(page)::int;

-- name: FinishRun :execrows
update run.run
set state = $3, finished_at = $4, version = $5
where id = $1 and workspace_id = $2 and version = $6;

-- name: InsertInvocation :exec
insert into run.invocation (
    id, run_id, workspace_id, step_id, tool_id, sequence, phase,
    argv, binary_path, exit_code, signal, refusal_rule, refusal_reason,
    skipped_because, unavailable, started_at, finished_at, duration_ms, permit_rule,
    subject_kind, subject_value
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21);

-- name: InvocationsForRun :many
select id, run_id, workspace_id, step_id, tool_id, sequence, phase,
       argv, binary_path, exit_code, signal, refusal_rule, refusal_reason,
       skipped_because, unavailable, started_at, finished_at, duration_ms, permit_rule,
       subject_kind, subject_value
from run.invocation where run_id = $1 order by sequence;

-- name: InvocationByID :one
select id, run_id, workspace_id, step_id, tool_id, sequence, phase,
       argv, binary_path, exit_code, signal, refusal_rule, refusal_reason,
       skipped_because, unavailable, started_at, finished_at, duration_ms, permit_rule,
       subject_kind, subject_value
from run.invocation where id = $1 and workspace_id = $2;

-- name: SaveInvocation :execrows
-- Every column an outcome can touch. It must match what the domain can change —
-- an UPDATE that silently omits one returns a correctly-moved record having
-- moved nothing, which identity's ConfirmEmailChange did for a day.
update run.invocation
set phase = $2, argv = $3, binary_path = $4, exit_code = $5, signal = $6,
    refusal_rule = $7, refusal_reason = $8, skipped_because = $9,
    unavailable = $10, started_at = $11, finished_at = $12, duration_ms = $13,
    permit_rule = $14
where id = $1;

-- name: InsertArtifact :exec
insert into run.artifact (
    id, workspace_id, invocation_id, stream, hash, bytes, truncated,
    media_type, created_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
-- A retried write of the same bytes is one blob and must be one row. The
-- content address makes the second write identical, so ignoring it is correct
-- rather than merely convenient.
on conflict (invocation_id, stream) do nothing;

-- name: ArtifactsForInvocation :many
select id, workspace_id, invocation_id, stream, hash, bytes, truncated,
       media_type, created_at
from run.artifact where invocation_id = $1 order by stream;

-- name: ArtifactByID :one
select id, workspace_id, invocation_id, stream, hash, bytes, truncated,
       media_type, created_at
from run.artifact where id = $1 and workspace_id = $2;

-- name: ClaimRun :many
-- The worker's read — decisions/0033. FOR UPDATE SKIP LOCKED so two workers
-- never claim one run, and so a worker that dies mid-run leaves the row visible
-- to the next boot rather than losing it.
--
-- THE PREDICATE IS `state = 'running'` AND NOTHING ELSE. It once also required a
-- PENDING invocation, which read as an optimisation and was a bug: a run whose
-- every step the spawn gate refused has nothing pending, was never claimed, and
-- sat at `running` forever — while being, by 0033, a COMPLETE answer to "may we
-- look at this".
--
-- Claiming a run with no work is cheap and it is what finishes it. There is
-- deliberately no separate "unclaimed" flag: a state that can get out of step
-- with the work is the thing that produced the bug above.
select r.id, r.workspace_id, r.target_id, r.check_id, r.state, r.started_by,
       r.started_at, r.finished_at, r.version
from run.run r
where r.state = 'running'
order by r.started_at
limit sqlc.arg(batch)::int
for update of r skip locked;

-- name: RefusalsForRule :many
-- What makes an append-only scope ledger worth keeping: which spawns did this
-- rule refuse. 0030 keeps the rule because three surfaces cite it; this is one.
select id, run_id, workspace_id, step_id, tool_id, sequence, phase,
       argv, binary_path, exit_code, signal, refusal_rule, refusal_reason,
       skipped_because, unavailable, started_at, finished_at, duration_ms, permit_rule,
       subject_kind, subject_value
from run.invocation
where workspace_id = $1 and refusal_rule = $2
order by finished_at desc
limit sqlc.arg(page)::int;

-- name: LatestCheckedPerSubject :many
-- What COVERAGE reads — decisions/0037. For one engagement, the newest FINISHED
-- invocation per (check, subject).
--
-- `finished_at is not null` and not `phase = 'ok'`: a check that RAN and found
-- nothing has been checked, and a check that ran and BROKE has been attempted.
-- Only `pending` and `refused` are excluded, and both are "no process existed".
--
-- A REFUSED invocation is deliberately not a check. The scope gate said no, so
-- nothing looked — which is `never`, and rendering it as checked would make a
-- refusal look like coverage.
select r.check_id, i.subject_kind, i.subject_value,
       max(i.finished_at)::timestamptz as last_checked
from run.invocation i
join run.run r on r.id = i.run_id
where i.workspace_id = $1
  and i.subject_value is not null
  and i.finished_at is not null
  and i.phase in ('ok', 'failed')
  and (sqlc.narg(target)::uuid is null or r.target_id = sqlc.narg(target)::uuid)
group by r.check_id, i.subject_kind, i.subject_value;

-- name: InvocationChecks :many
-- Which CHECK each invocation belonged to — the other half of coverage's join,
-- because a discovery tool is aimed at a seed and finds new subjects, and both
-- are "this check looked at this asset".
select i.id as invocation_id, r.check_id
from run.invocation i
join run.run r on r.id = i.run_id
where i.workspace_id = $1
  and (sqlc.narg(target)::uuid is null or r.target_id = sqlc.narg(target)::uuid);

-- name: LastStartedPerPair :many
-- The newest START of each (target, check) — decisions/0038 §5.
--
-- Nothing new is stored: a `next_due` column would be a second authority over a
-- time this table already knows, and it would be wrong every time an interval
-- changed. `run_check` already indexes it.
--
-- GLOBAL, with no workspace filter, because the scheduler is one lifecycle for
-- the whole process — the same shape the executor's claim has.
select target_id, check_id, max(started_at)::timestamptz as last_started
from run.run
group by target_id, check_id;
