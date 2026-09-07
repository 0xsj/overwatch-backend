-- What a tool eats and what it emits — decisions/0032.
--
-- `0031` already said a tool "declares which fragment kinds it can produce".
-- This is that column plus the other end, and together they are what makes an
-- edge in a check's chain checkable: an edge is legal when the upstream tool's
-- `produces` is in the downstream tool's `consumes`.
--
-- REVERSIBLE: two nullable columns.

alter table tool.tool add column consumes text;
alter table tool.tool add column produces text;

-- CONSUMES IS NULLABLE AND NULL MEANS "NOTHING UPSTREAM" — a source step, seeded
-- from the target's scope rather than fed by another tool. It does not mean
-- "anything", and a check's editor must not offer an inbound edge to one.
--
-- `produces` is nullable only because every row that already exists has no value
-- for it and a backfilled guess would be a fact nobody stated.
--
-- The vocabulary is WIDER than check's `applies_to` — it carries `finding`,
-- which is a thing bytes can be, and not a subject coverage counts. 0032
-- §Consequences argues that the two lists differing is the finding rather than
-- a bug.
alter table tool.tool add constraint tool_feed_known check (
    (consumes is null or consumes in ('domain', 'host', 'ip', 'cidr', 'asn', 'url', 'finding')) and
    (produces is null or produces in ('domain', 'host', 'ip', 'cidr', 'asn', 'url', 'finding'))
);
