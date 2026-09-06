-- name: InsertCredential :exec
insert into identity.credential (
    id, account_id, kind, hash, name, expires_at, revoked_at, version, created_at, updated_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: CredentialByID :one
select id, account_id, kind, hash, name, expires_at, revoked_at, version, created_at, updated_at
from identity.credential
where id = $1;

-- name: CredentialByHash :one
select id, account_id, kind, hash, name, expires_at, revoked_at, version, created_at, updated_at
from identity.credential
where hash = $1;

-- name: LivePasswordForAccount :one
select id, account_id, kind, hash, name, expires_at, revoked_at, version, created_at, updated_at
from identity.credential
where account_id = $1 and kind = 'password' and revoked_at is null;

-- name: CredentialsForAccount :many
select id, account_id, kind, hash, name, expires_at, revoked_at, version, created_at, updated_at
from identity.credential
where account_id = $1
order by created_at desc;

-- name: UpdateCredential :execrows
update identity.credential
set hash = $2, name = $3, expires_at = $4, revoked_at = $5, version = $6, updated_at = $7
where id = $1 and version = $8;
