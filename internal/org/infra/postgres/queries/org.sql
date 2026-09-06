-- name: InsertOrg :exec
insert into org.org (id, name, version, created_at, updated_at)
values ($1, $2, $3, $4, $5);

-- name: OrgByID :one
select id, name, version, created_at, updated_at from org.org where id = $1;

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
select id, org_id, account_id, role, status, version, created_at, updated_at, archived_at
from org.member
where org_id = $1
order by created_at;

-- name: OrgsForAccount :many
select o.id, o.name, o.version, o.created_at, o.updated_at
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
