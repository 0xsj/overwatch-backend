-- journal's schema. One table, append-only, and expirable — which is the whole
-- reason it is not audit's.
--
-- IRREVERSIBLE: creates a schema and a table. Reversing this is DROP SCHEMA.

create schema if not exists journal;

create table journal.line (
    id             uuid        primary key,
    event_id       uuid        not null,
    action         text        not null,
    subject        text        not null,
    origin         text        not null,
    actor          text        not null,
    on_behalf_of   text        not null default '',
    workspace_id   text        not null default '',
    depth          integer     not null default 0,
    attempt        integer     not null default 0,
    decision       boolean     not null default false,
    correlation_id uuid,
    causation_id   uuid,
    detail         jsonb       not null default '{}'::jsonb,
    occurred_at    timestamptz not null,
    recorded_at    timestamptz not null,

    constraint line_action_length   check (length(action) <= 128),
    constraint line_subject_length  check (length(subject) <= 256),
    constraint line_actor_present   check (actor <> ''),
    constraint line_subject_present check (subject <> ''),
    constraint line_origin_known    check (
        origin in ('unknown', 'request', 'schedule', 'replay', 'backfill', 'startup')),
    constraint line_depth_sane      check (depth >= 0),
    constraint line_attempt_sane    check (attempt >= 0)
);

-- At-least-once delivery. A redelivered event writes one row.
create unique index line_event_key on journal.line (event_id);

-- The default view: newest first, which is what the surface opens on.
create index line_recent on journal.line (occurred_at desc);

-- The Chain toggle. Everything caused by one request, in order.
create index line_correlation on journal.line (correlation_id, occurred_at)
    where correlation_id is not null;

-- The facets that are worth an index. Origin and actor are the two that narrow
-- hardest; depth and attempt are read off the row rather than filtered on until
-- something proves otherwise.
create index line_origin on journal.line (origin, occurred_at desc);
create index line_actor  on journal.line (actor, occurred_at desc);

-- Every decision is also a line, so this is the join back to audit.entry — and
-- PARTIAL, because decisions are a small minority of a large table.
create index line_decision on journal.line (occurred_at desc) where decision;
