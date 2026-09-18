create table artifact_cleanup.sweep_item (
    sweep_id       uuid not null references artifact_cleanup.sweep_run(id),
    workspace_id   uuid not null,
    ref            text not null check (ref ~ '^sha256:[a-f0-9]{64}$'),
    kind           text not null check (kind in ('source_capture', 'source_extraction')),
    source_id      uuid not null,
    capture_id     uuid,
    extraction_id  uuid,
    bytes          bigint not null check (bytes >= 0),
    outcome        text not null check (outcome in ('deleted', 'already_gone', 'skipped', 'failed')),
    reason         text not null default '' check (octet_length(reason) <= 4000),
    recorded_at    timestamptz not null,

    primary key (sweep_id, ref)
);

create index sweep_item_workspace_ref on artifact_cleanup.sweep_item(workspace_id, ref, recorded_at desc);
