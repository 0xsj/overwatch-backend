-- The newest units of work, with their correlation. Start here, then take a
-- correlation into chain.sql.
--
-- Usage:  psql "$DATABASE_URL" -f scripts/recent.sql

\set ON_ERROR_STOP on

select
    to_char(occurred_at, 'HH24:MI:SS.MS')              as at,
    origin,
    actor,
    action,
    case when decision then 'decision' else '' end     as kind,
    depth,
    subject,
    correlation_id
from journal.line
order by occurred_at desc
limit 40;
