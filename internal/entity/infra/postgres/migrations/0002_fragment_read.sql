-- A HUMAN READ, and it is NOT a judgement — decisions/0037.
--
-- `0011` says READ BY YOU being a check is "what keeps never read and no
-- judgement separable", and `CLAUDE.md` holds the pair by name: never read vs no
-- judgement — no human looked / no human ruled.
--
-- Computing the coverage cell from the judgement would BE that collapse. `0009`
-- sealed four judgement values and `unopened` is not "read but unruled" — a
-- person can open a fragment, read it, and decline to rule, which is the
-- commonest thing an analyst does.
--
-- REVERSIBLE: two nullable columns.

alter table entity.fragment add column read_at timestamptz;
alter table entity.fragment add column read_by uuid;

-- The pair moves together. A read with no reader is a claim nobody made.
alter table entity.fragment add constraint fragment_read_paired check (
    (read_at is null) = (read_by is null)
);

-- THE VIEW HAS TO BE RECREATED. A Postgres view freezes its column list at
-- creation time — even one written with `select f.*` — so two columns added to
-- the table afterwards are invisible to it until it is redefined.
--
-- That is worth knowing before the second view: a view is not a saved query, it
-- is a saved RESULT SHAPE, and every column added to a table underneath one is a
-- second edit somewhere else.
drop view entity.asset;

create view entity.asset as
select
    f.id, f.workspace_id, f.kind, f.value, f.origin,
    f.first_seen, f.last_seen, f.observations,
    f.judgement_state, f.judgement_by, f.judgement_at, f.judgement_reason,
    f.created_at, f.read_at, f.read_by,
    a.id   as attribution_id,
    a.claimant,
    a.basis,
    e.id   as root_entity_id,
    e.target_id
from entity.fragment f
join entity.attribution a on a.fragment_id = f.id and a.state = 'accepted'
join entity.entity e on e.id = a.entity_id and e.target_id is not null
where f.kind in ('host', 'cidr', 'ip', 'asn', 'url', 'repo', 'email');
