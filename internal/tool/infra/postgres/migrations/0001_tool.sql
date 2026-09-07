-- tool's schema. Two tables, both keyed on the ORG.
--
-- IRREVERSIBLE: creates a schema and two tables. Reversing this is DROP SCHEMA.

create schema if not exists tool;

-- ORG_ID AND NOT WORKSPACE_ID — decisions/0031. 0005 puts a workspace on every
-- product row; 0031 narrows that to rows recording something OBSERVED. A tool is
-- a capability the firm owns, and the correction loop is the argument: fixing a
-- mapping once must fix it everywhere.
create table tool.tool (
    id          uuid        primary key,
    org_id      uuid        not null,

    name        text        not null,
    -- The argv template, stored as text and never interpreted by this schema.
    argv        text        not null,
    -- What a scope rule qualifies — 0010. The spelling matches scope's, and the
    -- two are compared across a boundary neither package may cross.
    intensity   text        not null,

    status      text        not null,
    version     integer     not null,
    created_by  uuid        not null,
    created_at  timestamptz not null,
    updated_at  timestamptz not null,
    archived_at timestamptz,

    constraint tool_name_present     check (name <> '' and length(name) <= 120),
    constraint tool_argv_present     check (argv <> '' and length(argv) <= 2000),
    constraint tool_intensity_known  check (intensity in ('passive', 'light', 'loud')),
    constraint tool_status_known     check (status in ('active', 'archived')),
    constraint tool_archived_paired  check (
        (status = 'archived') = (archived_at is not null)
    )
);

-- One live name per org, case-insensitively; archiving RELEASES it. The
-- predicate is the lifecycle rule, the same shape workspace and target use.
create unique index tool_live_name on tool.tool (org_id, lower(name))
    where status <> 'archived';

create index tool_org on tool.tool (org_id, created_at);

-- A mapping version is never edited — an observation cites the version that
-- produced it. The only UPDATE this table takes moves the two lifecycle columns.
create table tool.mapping (
    id          uuid        primary key,
    org_id      uuid        not null,
    -- Not a foreign key even inside this schema's own pair, for consistency with
    -- every other reference here: the store owns the check.
    tool_id     uuid        not null,

    field       text        not null,
    expression  text        not null,
    -- Counts within (tool, field). It is what a citation carries, so it is
    -- assigned once and never reused.
    version     integer     not null,

    state       text        not null,
    created_by  uuid        not null,
    created_at  timestamptz not null,
    promoted_at timestamptz,
    retired_at  timestamptz,

    constraint mapping_field_present      check (field <> '' and length(field) <= 120),
    constraint mapping_expression_present check (expression <> '' and length(expression) <= 4000),
    constraint mapping_version_positive   check (version >= 1),
    constraint mapping_state_known        check (state in ('draft', 'live', 'retired')),
    constraint mapping_retired_paired     check ((state = 'retired') = (retired_at is not null))
);

-- A version is assigned once. This is what makes "cited version 3" mean one row.
create unique index mapping_version on tool.mapping (tool_id, field, version);

-- EXACTLY ONE LIVE VERSION per field. Promotion retires the incumbent first, so
-- a crash between the two writes leaves a field with NO live mapping rather than
-- two — silence, which is recoverable, over ambiguity, which is not.
create unique index mapping_live on tool.mapping (tool_id, field)
    where state = 'live';

create index mapping_tool on tool.mapping (tool_id, field, version desc);
