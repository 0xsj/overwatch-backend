-- Raw recipient-link tokens never live in the database. The API hashes the
-- opaque value before handing it to this store, so a database read cannot
-- reconstruct a usable handoff URL.
create table brief.snapshot_handoff_share (
    id           uuid primary key,
    workspace_id uuid        not null,
    snapshot_id  uuid        not null references brief.snapshot(id) on delete cascade,
    created_by   uuid        not null,
    token_digest text        not null unique,
    created_at   timestamptz not null,
    revoked_at   timestamptz,
    revoked_by   uuid,

    constraint snapshot_handoff_share_token_digest_length check (length(token_digest) = 64)
);

create index snapshot_handoff_share_for_snapshot
    on brief.snapshot_handoff_share (workspace_id, snapshot_id, created_at desc);
