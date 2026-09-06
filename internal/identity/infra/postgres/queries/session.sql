-- name: InsertSession :exec
insert into identity.session (
    id, account_id, hash, user_agent, address, issued_at, expires_at, revoked_at
) values ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: SessionByHash :one
select id, account_id, hash, user_agent, address, issued_at, expires_at, revoked_at
from identity.session
where hash = $1;

-- name: SessionsForAccount :many
select id, account_id, hash, user_agent, address, issued_at, expires_at, revoked_at
from identity.session
where account_id = $1
order by issued_at desc;

-- name: RevokeSession :execrows
update identity.session
set revoked_at = $2
where id = $1 and revoked_at is null;

-- name: RevokeSessionsForAccount :execrows
update identity.session
set revoked_at = $2
where account_id = $1 and revoked_at is null;
