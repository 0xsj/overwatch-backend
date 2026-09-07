-- target's schema. One table, and the first product row in the system.
--
-- IRREVERSIBLE: creates a schema and a table. Reversing this is DROP SCHEMA.

create schema if not exists target;

create table target.target (
    id           uuid        primary key,

    -- NON-NULL, and this is the discipline decisions/0005 could not enforce
    -- until there was a product table to put it in. It names workspace's row
    -- and is NOT a foreign key: no reference leaves this schema — 0017.
    --
    -- A query that omits it returns another engagement's targets, with the
    -- caller authenticated and nothing about the code path looking wrong.
    workspace_id uuid        not null,

    name         text        not null,
    -- Two kinds, because CLAUDE.md names two framings and calls them "the same
    -- machinery at different roots" — decisions/0029. `program` is named there
    -- as later-not-dropped and is deliberately absent.
    kind         text        not null,
    status       text        not null,
    version      integer     not null,

    -- Who started looking at this. Immutable, and NEVER consulted by the
    -- authorisation gate — the same rule as workspace.created_by (0020).
    created_by   uuid        not null,

    created_at   timestamptz not null,
    updated_at   timestamptz not null,
    archived_at  timestamptz,

    constraint target_name_present check (name <> ''),
    constraint target_name_length  check (length(name) <= 200),
    constraint target_kind_known   check (kind in ('organisation', 'person')),
    constraint target_status_known check (status in ('active', 'archived')),
    constraint target_version_min  check (version >= 1),
    -- archived_at is set exactly when the status says so. Two fields that can
    -- disagree are two fields that eventually do.
    constraint target_archived_iff check (
        (status = 'archived' and archived_at is not null) or
        (status <> 'archived' and archived_at is null)
    )
);

-- PARTIAL, and the predicate is the decision — the same shape as
-- workspace_live_name. An archived target releases its name, so a closed piece
-- of work can be reopened under it; two LIVE targets called "Northbeam" in one
-- engagement is a mistake nobody can see on a list.
--
-- Folded, because a list showing "Northbeam" beside "northbeam" is the same
-- mistake with extra steps, and the database cannot see that unless the value
-- arrives lowered.
create unique index target_live_name on target.target (workspace_id, lower(name))
    where status <> 'archived';

-- The default read: one engagement's live targets, oldest first.
create index target_workspace on target.target (workspace_id, created_at)
    where status <> 'archived';

-- root_entity_id is NOT here — decisions/0029. `entity` does not exist, and a
-- nullable column nothing fills is the modelled state with no caller CLAUDE.md
-- §5 refuses. It arrives in the migration that creates the row it points at.
