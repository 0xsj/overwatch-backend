-- audit's schema. One table, append-only, and the indexes are the three reads.
--
-- IRREVERSIBLE: creates a schema and a table. There is no down migration and
-- reversing this is DROP SCHEMA.
--
-- The schema is created by postgres.Migrate (see InSchema) because the ledger is
-- written before this file runs. The line below restates it for a reader who
-- opens this file alone.

create schema if not exists audit;

create table audit.entry (
    id             uuid        primary key,
    event_id       uuid        not null,
    scope          text        not null,
    action         text        not null,
    subject        text        not null,
    actor          text        not null,
    on_behalf_of   text        not null default '',
    workspace_id   text        not null default '',
    correlation_id uuid,
    causation_id   uuid,
    detail         jsonb       not null default '{}'::jsonb,
    occurred_at    timestamptz not null,
    recorded_at    timestamptz not null,

    constraint entry_scope_known    check (scope in ('system', 'account', 'workspace')),
    constraint entry_action_length  check (length(action) <= 128),
    constraint entry_subject_length check (length(subject) <= 256),
    -- "we did not know who" and "we forgot to look" must stay distinguishable,
    -- so a blank actor is refused. Write the anonymous actor instead.
    constraint entry_actor_present  check (actor <> ''),
    constraint entry_subject_present check (subject <> ''),
    -- decisions/0006: workspace_id is set exactly when the scope is a workspace.
    -- Both halves, because a workspace_id under a system scope is a row nobody
    -- can read correctly and an empty one under a workspace scope is invisible.
    constraint entry_workspace_iff_scoped check (
        (scope = 'workspace' and workspace_id <> '') or
        (scope <> 'workspace' and workspace_id = '')
    )
);

-- The at-least-once gate, in one line. A redelivered event produces a second
-- INSERT with the same event_id; this turns it into a no-op instead of a second
-- row claiming the thing happened twice.
create unique index entry_event_key on audit.entry (event_id);

-- The three reads of decisions/0006, as three indexes. They are the reason this
-- is one table: the projections are queries, and a query can be added.
create index entry_subject     on audit.entry (subject, occurred_at desc);
create index entry_actor       on audit.entry (actor, occurred_at desc);
create index entry_workspace   on audit.entry (workspace_id, occurred_at desc)
    where workspace_id <> '';

-- "what else was part of this request" — the join to the journal, and the reason
-- one act producing two rows is legible rather than duplication.
create index entry_correlation on audit.entry (correlation_id)
    where correlation_id is not null;
