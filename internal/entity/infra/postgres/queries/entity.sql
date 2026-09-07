-- name: InsertEntity :exec
insert into entity.entity (
    id, workspace_id, kind, label, target_id,
    judgement_state, judgement_by, judgement_at, judgement_reason, created_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
-- IDEMPOTENT ON REDELIVERY. `entity_one_root_per_target` refuses a second root
-- and the outbox delivers at least once, so a redelivered `target.added` must be
-- a no-op rather than an error — decisions/0007.
on conflict do nothing;

-- name: EntityByID :one
select id, workspace_id, kind, label, target_id,
       judgement_state, judgement_by, judgement_at, judgement_reason, created_at
from entity.entity where id = $1 and workspace_id = $2;

-- name: RootEntityForTarget :one
select id, workspace_id, kind, label, target_id,
       judgement_state, judgement_by, judgement_at, judgement_reason, created_at
from entity.entity where target_id = $1;

-- name: EntitiesForWorkspace :many
select id, workspace_id, kind, label, target_id,
       judgement_state, judgement_by, judgement_at, judgement_reason, created_at
from entity.entity where workspace_id = $1 order by created_at
limit sqlc.arg(page)::int;

-- name: JudgeEntity :execrows
update entity.entity
set judgement_state = $3, judgement_by = $4, judgement_at = $5, judgement_reason = $6
where id = $1 and workspace_id = $2;

-- name: UpsertFragment :one
-- THE DEDUP, as one statement — decisions/0036. A fragment IS the tuple, so the
-- conflict target is the tuple.
--
-- `last_seen` uses greatest() and `first_seen` least(), so a re-extraction of an
-- old artifact moves neither in the wrong direction. `observations` ADDS,
-- because two statements about one fragment are two statements.
--
-- ORIGIN is promoted, never demoted: a manual fragment a tool later finds
-- becomes observed, which is a stronger claim than either alone. `least()` over
-- the text does that by luck of the alphabet, so it is spelled out instead.
insert into entity.fragment (
    id, workspace_id, kind, value, origin, first_seen, last_seen, observations, created_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
on conflict (workspace_id, kind, value) do update
set observations = entity.fragment.observations + excluded.observations,
    first_seen   = least(entity.fragment.first_seen, excluded.first_seen),
    last_seen    = greatest(entity.fragment.last_seen, excluded.last_seen),
    origin       = case when excluded.origin = 'observed' then 'observed'
                        else entity.fragment.origin end
returning id, workspace_id, kind, value, origin, first_seen, last_seen,
          observations, judgement_state, judgement_by, judgement_at,
          judgement_reason, created_at, read_at, read_by, (xmax = 0) as inserted;

-- name: FragmentByID :one
select id, workspace_id, kind, value, origin, first_seen, last_seen,
       observations, judgement_state, judgement_by, judgement_at,
       judgement_reason, created_at, read_at, read_by
from entity.fragment where id = $1 and workspace_id = $2;

-- name: FragmentsForWorkspace :many
select id, workspace_id, kind, value, origin, first_seen, last_seen,
       observations, judgement_state, judgement_by, judgement_at,
       judgement_reason, created_at, read_at, read_by
from entity.fragment
where workspace_id = $1
  and (sqlc.narg(kind)::text is null or kind = sqlc.narg(kind)::text)
order by last_seen desc nulls last, value
limit sqlc.arg(page)::int;

-- name: JudgeFragment :execrows
update entity.fragment
set judgement_state = $3, judgement_by = $4, judgement_at = $5, judgement_reason = $6
where id = $1 and workspace_id = $2;

-- name: InsertAttribution :exec
insert into entity.attribution (
    id, workspace_id, entity_id, fragment_id, claimant, claimant_ref,
    confidence, basis, state, decided_at, decided_by, decided_note, created_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
-- One claim per (entity, fragment), and a redelivered subscriber event must not
-- write a second.
on conflict (entity_id, fragment_id) do nothing;

-- name: AttributionByID :one
select id, workspace_id, entity_id, fragment_id, claimant, claimant_ref,
       confidence, basis, state, decided_at, decided_by, decided_note, created_at
from entity.attribution where id = $1 and workspace_id = $2;

-- name: AttributionsForFragment :many
select id, workspace_id, entity_id, fragment_id, claimant, claimant_ref,
       confidence, basis, state, decided_at, decided_by, decided_note, created_at
from entity.attribution where fragment_id = $1 order by created_at;

-- name: AttributionsForEntity :many
select id, workspace_id, entity_id, fragment_id, claimant, claimant_ref,
       confidence, basis, state, decided_at, decided_by, decided_note, created_at
from entity.attribution
where entity_id = $1
  and (sqlc.narg(state)::text is null or state = sqlc.narg(state)::text)
order by created_at
limit sqlc.arg(page)::int;

-- name: DecideAttribution :execrows
-- It never touches `claimant` — 0008's title — and the predicate refuses a
-- second ruling, so two people accepting at once produce one winner.
update entity.attribution
set state = $3, decided_at = $4, decided_by = $5, decided_note = $6
where id = $1 and workspace_id = $2 and state = 'proposed';

-- name: Assets :many
-- Reads the VIEW, which is the point of the view: the word and the query are the
-- same object — 0009.
select id, workspace_id, kind, value, origin, first_seen, last_seen, observations,
       judgement_state, judgement_by, judgement_at, judgement_reason, created_at,
       read_at, read_by,
       attribution_id, claimant, basis, root_entity_id, target_id
from entity.asset
where workspace_id = $1
  and (sqlc.narg(target)::uuid is null or target_id = sqlc.narg(target)::uuid)
order by last_seen desc nulls last, value
limit sqlc.arg(page)::int;

-- name: MarkRead :execrows
-- A READ, not a ruling — decisions/0037. It deliberately does not touch the
-- judgement columns: reading is not ruling, and a client that wants both makes
-- two calls because they are two acts.
update entity.fragment
set read_at = $3, read_by = $4
where id = $1 and workspace_id = $2;
