-- Comments belong to the frozen handoff collaboration layer, not to the
-- immutable authored snapshot or append-only review decision history.
create table brief.snapshot_comment (
    id           uuid primary key,
    workspace_id uuid        not null,
    snapshot_id  uuid        not null references brief.snapshot(id) on delete cascade,
    author_id    uuid        not null,
    body         text        not null,
    created_at   timestamptz not null,
    constraint snapshot_comment_body_length check (octet_length(body) between 1 and 4000)
);

create index snapshot_comment_for_snapshot
    on brief.snapshot_comment (workspace_id, snapshot_id, created_at desc);
