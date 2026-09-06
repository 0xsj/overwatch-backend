-- org's schema. Two tables, and no foreign key leaves it.
--
-- IRREVERSIBLE: creates a schema and two tables. Reversing this is DROP SCHEMA.

create schema if not exists org;

-- ------------------------------------------------------------------- org ---

create table org.org (
    id         uuid        primary key,
    name       text        not null,
    version    integer     not null,
    created_at timestamptz not null,
    updated_at timestamptz not null,

    constraint org_name_present check (name <> ''),
    constraint org_name_length  check (length(name) <= 120),
    constraint org_version_min  check (version >= 1)
);

-- Names are NOT unique. A personal org is named after its owner and two people
-- called Sam Lee are two orgs; a globally unique name would make the second
-- signup fail for a reason nobody could act on.

-- ---------------------------------------------------------------- member ---

create table org.member (
    id          uuid        primary key,
    org_id      uuid        not null references org.org (id) on delete restrict,
    -- account_id names a row in identity's schema and is deliberately NOT a
    -- reference: no foreign key crosses out of this schema, which is what makes
    -- extracting this domain a dump of one schema. The cost is that a member
    -- naming an account that does not exist is a bug this table cannot catch,
    -- and keeping that impossible is the registration transaction's job.
    account_id  uuid        not null,
    role        text        not null,
    status      text        not null,
    version     integer     not null,
    created_at  timestamptz not null,
    updated_at  timestamptz not null,
    archived_at timestamptz,

    constraint member_role_known   check (role in ('owner')),
    constraint member_status_known check (status in ('active', 'archived')),
    constraint member_version_min  check (version >= 1),
    -- archived_at is set exactly when the status says so. Two fields that can
    -- disagree are two fields that eventually do.
    constraint member_archived_iff check (
        (status = 'archived' and archived_at is not null) or
        (status <> 'archived' and archived_at is null)
    )
);

-- PARTIAL, and the predicate is the decision. An archived membership keeps its
-- row forever because every attribution made under it still points at the
-- account; a total unique index would stop somebody ever rejoining an org they
-- once left.
create unique index member_live_account on org.member (org_id, account_id)
    where status <> 'archived';

create index member_account on org.member (account_id) where status <> 'archived';
create index member_org     on org.member (org_id, created_at);
