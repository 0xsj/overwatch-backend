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

-- name: RootsPerFragment :many
-- How many DISTINCT ROOT ENTITIES have an accepted attribution to each of these
-- fragments — decisions/0044 §2's `seen elsewhere`.
--
-- **WITHIN ONE WORKSPACE, and it cannot be otherwise.** `entity.fragment` is
-- (workspace, kind, value), so a fragment does not exist across engagements at
-- all; what this finds is one client's two subsidiaries sharing a host.
--
-- Crossing a workspace was refused in 0044 §2 and the reason is a disclosure:
-- an analyst on one engagement holding `none` on another must not learn that
-- other engagement exists.
select fragment_id, count(distinct entity_id)::int as roots
from entity.attribution
where workspace_id = $1
  and state = 'accepted'
  and fragment_id = any(sqlc.arg(fragments)::uuid[])
group by fragment_id;

-- name: DerivationsAmong :many
-- Every edge BETWEEN THESE FRAGMENTS — decisions/0044 §1. Both ends must be on
-- the canvas: drawing an edge to a node that is not there implies the picture is
-- complete and the reader cannot see why the line stops.
select id, workspace_id, from_fragment_id, to_fragment_id, label,
       invocation_id, artifact_id, mapping_id, created_at
from entity.derivation
where workspace_id = $1
  and from_fragment_id = any(sqlc.arg(fragments)::uuid[])
  and to_fragment_id = any(sqlc.arg(fragments)::uuid[])
order by created_at;

-- name: FragmentForValue :one
-- THE LOOKUP A DERIVATION'S `from` RESOLVES THROUGH — decisions/0040 §5.
--
-- The value is folded by the ADAPTER, not by the caller and not here. The
-- column is folded, `entity.domain.Fold` is the one function that decides what
-- "the same host" means, and the store is the single door every lookup goes
-- through — so a caller cannot forget, and there is no `lower()` here competing
-- with a Go function for the definition.
select id, workspace_id, kind, value, origin, first_seen, last_seen,
       observations, judgement_state, judgement_by, judgement_at,
       judgement_reason, created_at, read_at, read_by
from entity.fragment where workspace_id = $1 and kind = $2 and value = $3;

-- name: InsertDerivation :exec
-- `0003`'s second edge kind. ON CONFLICT DO NOTHING against `derivation_once`:
-- the outbox is at-least-once (0007) and a redelivered extraction must not
-- double the graph.
insert into entity.derivation (
    id, workspace_id, from_fragment_id, to_fragment_id, label,
    invocation_id, artifact_id, mapping_id, created_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
on conflict (from_fragment_id, to_fragment_id, label, invocation_id) do nothing;

-- name: InsertUnresolved :exec
insert into entity.derivation_unresolved (
    id, workspace_id, invocation_id, mapping_id, to_fragment_id,
    from_kind, from_value, from_raw, label, created_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
on conflict (invocation_id, to_fragment_id, from_value, label) do nothing;

-- name: DerivationsForFragment :many
-- BOTH DIRECTIONS in one read. The canvas draws outward from a fragment and
-- does not care which end it is; two queries would make the caller union them
-- and get the ordering wrong.
select id, workspace_id, from_fragment_id, to_fragment_id, label,
       invocation_id, artifact_id, mapping_id, created_at
from entity.derivation
where workspace_id = $1 and (from_fragment_id = $2 or to_fragment_id = $2)
order by created_at desc
limit sqlc.arg(page)::int;

-- name: UnresolvedForInvocation :many
select id, workspace_id, invocation_id, mapping_id, to_fragment_id,
       from_kind, from_value, from_raw, label, created_at
from entity.derivation_unresolved
where workspace_id = $1 and invocation_id = $2
order by from_value;

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

-- name: AllAssets :many
-- Reads the VIEW, which is the point of the view: the word and the query are the
-- same object — 0009.
select id, workspace_id, kind, value, origin, first_seen, last_seen, observations,
       judgement_state, judgement_by, judgement_at, judgement_reason, created_at,
       read_at, read_by,
       attribution_id, claimant, basis, root_entity_id, target_id
from entity.asset
where workspace_id = $1
  and (sqlc.narg(target)::uuid is null or target_id = sqlc.narg(target)::uuid)
order by last_seen desc nulls last, value;

-- name: AcceptedFragmentsForTarget :many
-- Includes non-asset kinds, such as documents, while retaining target and
-- workspace isolation. Proposed and rejected connections supply no attribution.
select distinct a.fragment_id
from entity.attribution a
join entity.entity e on e.id = a.entity_id and e.workspace_id = a.workspace_id
join entity.fragment f on f.id = a.fragment_id and f.workspace_id = a.workspace_id
where a.workspace_id = $1 and e.target_id = $2 and a.state = 'accepted'
order by a.fragment_id;
