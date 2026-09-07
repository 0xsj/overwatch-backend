-- name: InsertOrg :exec
insert into org.org (id, name, version, created_at, updated_at, source_event_id)
values ($1, $2, $3, $4, $5, $6);

-- name: OrgByID :one
select id, name, version, created_at, updated_at, source_event_id from org.org where id = $1;

-- name: UpdateOrg :execrows
update org.org set name = $2, version = $3, updated_at = $4
where id = $1 and version = $5;

-- name: InsertMember :exec
insert into org.member (
    id, org_id, account_id, role, status, version, created_at, updated_at, archived_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: MemberByID :one
select id, org_id, account_id, role, status, version, created_at, updated_at, archived_at
from org.member where id = $1;

-- name: LiveMemberFor :one
select id, org_id, account_id, role, status, version, created_at, updated_at, archived_at
from org.member
where org_id = $1 and account_id = $2 and status <> 'archived';

-- name: MembersOf :many
-- LIVE members only. It did not filter until 2026-09-07, so the members
-- endpoint listed people who had been removed — every caller wants the live set
-- and every doc said so, but the query did not.
--
-- A former-members read, when something needs one, gets its OWN query — the same
-- shape workspace uses for ForOrg and AllForOrg. It does not get a boolean.
select id, org_id, account_id, role, status, version, created_at, updated_at, archived_at
from org.member
where org_id = $1 and status <> 'archived'
order by created_at;

-- name: OrgsForAccount :many
select o.id, o.name, o.version, o.created_at, o.updated_at, o.source_event_id
from org.org o
join org.member m on m.org_id = o.id
where m.account_id = $1 and m.status <> 'archived'
order by o.created_at;

-- name: UpdateMember :execrows
update org.member
set role = $2, status = $3, version = $4, updated_at = $5, archived_at = $6
where id = $1 and version = $7;

-- name: CountLiveOwners :one
select count(*) from org.member
where org_id = $1 and role = 'owner' and status <> 'archived';

-- name: OrgBySourceEvent :one
select id, name, version, created_at, updated_at, source_event_id
from org.org where source_event_id = $1;

-- name: InsertGrant :exec
insert into org.grant (
    id, org_id, account_id, workspace_id, level, version, created_at, updated_at
) values ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GrantFor :one
select id, org_id, account_id, workspace_id, level, version, created_at, updated_at
from org.grant where org_id = $1 and account_id = $2 and workspace_id = $3;

-- name: GrantsForAccount :many
select id, org_id, account_id, workspace_id, level, version, created_at, updated_at
from org.grant where account_id = $1 and org_id = $2;

-- name: GrantsOnWorkspace :many
select id, org_id, account_id, workspace_id, level, version, created_at, updated_at
from org.grant where workspace_id = $1 order by created_at;

-- name: UpdateGrant :execrows
update org.grant set level = $2, version = $3, updated_at = $4
where id = $1 and version = $5;

-- name: DeleteGrant :execrows
delete from org.grant where org_id = $1 and account_id = $2 and workspace_id = $3;


-- name: InsertInvite :exec
insert into org.invite (
    id, org_id, email, role, invited_by, workspace_id, level, hash,
    created_at, expires_at, accepted_at, accepted_by, revoked_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13);

-- name: InviteByHash :one
select id, org_id, email, role, invited_by, workspace_id, level, hash,
       created_at, expires_at, accepted_at, accepted_by, revoked_at
from org.invite where hash = $1;

-- name: LiveInviteFor :one
select id, org_id, email, role, invited_by, workspace_id, level, hash,
       created_at, expires_at, accepted_at, accepted_by, revoked_at
from org.invite
where org_id = $1 and email = $2 and accepted_at is null and revoked_at is null;

-- name: InvitesForOrg :many
select id, org_id, email, role, invited_by, workspace_id, level, hash,
       created_at, expires_at, accepted_at, accepted_by, revoked_at
from org.invite where org_id = $1 order by created_at desc;

-- name: SaveInvite :execrows
update org.invite
set accepted_at = $2, accepted_by = $3, revoked_at = $4
where id = $1;

-- name: LockLiveOwners :many
-- FOR UPDATE, and counted in Go rather than by count(*) — decisions/0026.
-- Postgres refuses FOR UPDATE with an aggregate, and the lock is the point: a
-- concurrent demotion of a DIFFERENT owner blocks here until this transaction
-- commits, then re-reads and sees one owner rather than two.
--
-- An unlocked count is a check that is true when it runs and false when it
-- matters: two administrators demoting two owners both read 2, both writes are
-- individually valid, and the org is left with none.
-- ACCOUNT_ID and not id: the caller compares these against the account it is
-- about to demote or remove, and a member id would never match one — a
-- comparison that is always false is a guard that always passes.
select account_id from org.member
where org_id = $1 and role = 'owner' and status <> 'archived'
for update;

-- name: RevokeGrantsFor :execrows
-- Every grant a departing member held — decisions/0026. Removal is a departure
-- and deletes them; a DEMOTION does not, so restoring a role restores access.
delete from org.grant where org_id = $1 and account_id = $2;
