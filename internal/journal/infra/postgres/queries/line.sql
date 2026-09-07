-- name: InsertLine :execrows
insert into journal.line (
    id, event_id, action, subject, origin, actor, on_behalf_of, workspace_id,
    depth, attempt, decision, correlation_id, causation_id, detail,
    occurred_at, recorded_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
on conflict (event_id) do nothing;

-- name: LineByID :one
select id, event_id, action, subject, origin, actor, on_behalf_of, workspace_id,
       depth, attempt, decision, correlation_id, causation_id, detail,
       occurred_at, recorded_at
from journal.line
where id = $1;

-- name: RecentLines :many
select id, event_id, action, subject, origin, actor, on_behalf_of, workspace_id,
       depth, attempt, decision, correlation_id, causation_id, detail,
       occurred_at, recorded_at
from journal.line
order by occurred_at desc
limit $1;

-- name: LinesForCorrelation :many
select id, event_id, action, subject, origin, actor, on_behalf_of, workspace_id,
       depth, attempt, decision, correlation_id, causation_id, detail,
       occurred_at, recorded_at
from journal.line
where correlation_id = $1
order by occurred_at;

-- name: LinesForOrigin :many
select id, event_id, action, subject, origin, actor, on_behalf_of, workspace_id,
       depth, attempt, decision, correlation_id, causation_id, detail,
       occurred_at, recorded_at
from journal.line
where origin = $1
order by occurred_at desc
limit $2;

-- name: ExpireLinesBefore :execrows
-- BATCHED, and the batch is a bound on the transaction rather than a throughput
-- knob — decisions/0022. An unbounded delete against a year of accumulated rows
-- is one transaction holding one very large lock.
--
-- `decision = false` is the other half of the record: a decision is kept, so a
-- chain older than the window degrades to its skeleton instead of disappearing.
delete from journal.line l
where l.id in (
    select c.id from journal.line c
    where c.occurred_at < $1 and c.decision = false
    limit $2
);
