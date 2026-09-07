-- The role set decisions/0019 sealed, and the grant table that narrows it.
--
-- IRREVERSIBLE for the grant table: reversing it is DROP TABLE. The constraint
-- widening below IS reversible only while no row uses a new value.

-- --------------------------------------------------------------- roles ---

-- 0001 shipped `check (role in ('owner'))` because one role existed. Widening a
-- check constraint is a drop and a recreate; there is no ALTER for it, and the
-- recreate re-validates every existing row, which is what makes this safe rather
-- than merely permitted.
alter table org.member drop constraint member_role_known;
alter table org.member add constraint member_role_known
    check (role in ('owner', 'admin', 'member', 'guest', 'client'));

-- At least one live owner per org is decisions/0019's invariant and is NOT
-- expressible as a table constraint: it is a property of a SET of rows under a
-- predicate, and Postgres has no assertion. It lives in the command that changes
-- a role or archives a member, which must count live owners first, and
-- org.ErrLastOwner is what it raises. Written here because the next person to
-- read this file will look for it and must find out where it went.

-- --------------------------------------------------------------- grant ---

create table org.grant (
    id           uuid        primary key,
    org_id       uuid        not null references org.org (id) on delete restrict,
    -- account_id names identity's row and workspace_id names workspace's, and
    -- NEITHER is a foreign key: no reference leaves this schema. The cost is
    -- that a grant naming a workspace that does not exist is a bug this table
    -- cannot catch, and the command that writes it is what keeps that
    -- impossible -- decisions/0017.
    account_id   uuid        not null,
    workspace_id uuid        not null,
    level        text        not null,
    version      integer     not null,
    created_at   timestamptz not null,
    updated_at   timestamptz not null,

    -- 'none' is deliberately NOT a storable level. A grant of none is a
    -- revocation, and revoking deletes the row; storing it would give absence
    -- two spellings, and the two would disagree the first time a query
    -- remembered only one of them.
    constraint grant_level_known  check (level in ('read', 'write', 'admin')),
    constraint grant_version_min  check (version >= 1)
);

-- TOTAL rather than partial, and that is the difference from member_live_account
-- above. A grant has no archived state to preserve: it is deleted when revoked,
-- because a grant is a live permission and not a claim anybody authored. Nothing
-- in the record points back at it.
create unique index grant_member_workspace on org.grant (org_id, account_id, workspace_id);

-- The two reads. The first is the gate -- "what may THIS caller do HERE" -- and
-- runs on every workspace operation, so it is the one that must not scan.
create index grant_account   on org.grant (account_id, workspace_id);
create index grant_workspace on org.grant (workspace_id);
