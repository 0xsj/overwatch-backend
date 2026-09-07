-- The causation tree for one correlation, indented by depth.
--
-- correlation groups a request; causation is the edge inside it — see
-- notes/concepts/correlation-is-the-tree-causation-is-the-edge. A line's
-- causation_id names the EVENT ID of the line that caused it, which is why this
-- joins causation_id to event_id and not to id.
--
-- Usage:  psql "$DATABASE_URL" -v c="<correlation-uuid>" -f scripts/chain.sql

\set ON_ERROR_STOP on

with recursive tree as (
    select l.*, 0 as level
    from journal.line l
    where l.correlation_id = :'c'::uuid
      and l.causation_id is null

    union all

    select l.*, t.level + 1
    from journal.line l
    join tree t on l.causation_id = t.event_id
    where l.correlation_id = :'c'::uuid
)
select
    repeat('   ', level) || action                       as chain,
    case when decision then 'decision' else 'work' end   as kind,
    origin,
    actor,
    subject,
    to_char(occurred_at, 'HH24:MI:SS.MS')                as at,
    depth
from tree
order by occurred_at;
