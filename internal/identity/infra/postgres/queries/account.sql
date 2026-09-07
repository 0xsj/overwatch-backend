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
set email = $2, name = $3, status = $4, version = $5, updated_at = $6
where id = $1 and version = $7;

-- name: InsertToken :exec
insert into identity.token (
    id, account_id, kind, hash, created_at, expires_at, consumed_at, proposed_email
) values ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: TokenByHash :one
select id, account_id, kind, hash, created_at, expires_at, consumed_at, proposed_email
from identity.token where hash = $1;

-- name: ConsumeToken :execrows
update identity.token set consumed_at = $2
where id = $1 and consumed_at is null;

-- name: ConsumeLiveTokens :execrows
update identity.token set consumed_at = $3
where account_id = $1 and kind = $2 and consumed_at is null;

-- name: LiveSessionsFor :many
select id, account_id, hash, user_agent, address, issued_at, expires_at, revoked_at
from identity.session
where account_id = $1 and revoked_at is null and expires_at > $2
order by issued_at desc;

-- name: RevokeSessionsExcept :many
update identity.session
set revoked_at = $3
where account_id = $1 and id <> $2 and revoked_at is null
returning id;
