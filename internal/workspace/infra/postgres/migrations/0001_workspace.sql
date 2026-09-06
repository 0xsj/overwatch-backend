-- workspace's schema. One table, and no foreign key leaves it.
--
-- IRREVERSIBLE: creates a schema and a table. Reversing this is DROP SCHEMA.

create schema if not exists workspace;

create table workspace.workspace (
    id          uuid        primary key,
    -- org_id names a row in org's schema and is deliberately NOT a reference.
    -- See the note in org's migration: no foreign key crosses out of a schema,
    -- and the cost is that this table cannot catch a workspace whose org does
    -- not exist. The registration transaction is what keeps that from happening.
    org_id      uuid        not null,
    name        text        not null,
    status      text        not null,
    version     integer     not null,
    created_at  timestamptz not null,
    updated_at  timestamptz not null,
    archived_at timestamptz,

    constraint workspace_name_present check (name <> ''),
    constraint workspace_name_length  check (length(name) <= 120),
    constraint workspace_status_known check (status in ('active', 'archived')),
    constraint workspace_version_min  check (version >= 1),
    constraint workspace_archived_iff check (
        (status = 'archived' and archived_at is not null) or
        (status <> 'archived' and archived_at is null)
    )
);

-- PARTIAL. Two live engagements called "Acme Q3" inside one org is a mistake
-- nobody can see on a switcher; a TOTAL index would burn the name for good the
-- first time an engagement closed, which is worse.
create unique index workspace_live_name on workspace.workspace (org_id, lower(name))
    where status <> 'archived';

create index workspace_org on workspace.workspace (org_id, created_at);
