-- name: InsertAccount :exec
insert into identity.account (id, email, name, status, version, created_at, updated_at)
values ($1, $2, $3, $4, $5, $6, $7);

-- name: AccountByID :one
select id, email, name, status, version, created_at, updated_at
from identity.account
where id = $1;

-- name: AccountByEmail :one
select id, email, name, status, version, created_at, updated_at
from identity.account
where email = $1 and status <> 'archived';

-- name: UpdateAccount :execrows
update identity.account
set name = $2, status = $3, version = $4, updated_at = $5
where id = $1 and version = $6;
