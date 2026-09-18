create table artifact_cleanup.review (
    id           uuid primary key,
    workspace_id uuid not null,
    created_by   uuid not null,
    updated_by   uuid not null,
    status       text not null,
    created_at   timestamptz not null,
    updated_at   timestamptz not null,
    completed_at timestamptz,
    sweep_id     uuid references artifact_cleanup.sweep_run(id),

    constraint review_status_known check (status in ('open', 'completed')),
    constraint review_completed_shape check ((status = 'completed') = (completed_at is not null))
);

create table artifact_cleanup.review_item (
    review_id    uuid not null references artifact_cleanup.review(id) on delete cascade,
    workspace_id uuid not null,
    ref          text not null check (ref ~ '^sha256:[a-f0-9]{64}$'),
    kind         text not null check (kind in ('source_capture', 'source_extraction')),
    source_id    uuid not null,
    capture_id   uuid,
    extraction_id uuid,
    bytes        bigint not null check (bytes >= 0),
    purged_at    timestamptz not null,
    created_at   timestamptz not null,

    primary key (review_id, ref)
);

create index review_workspace_status on artifact_cleanup.review(workspace_id, status, updated_at desc);
create unique index review_one_open_per_workspace on artifact_cleanup.review(workspace_id) where status='open';
create index review_item_workspace_ref on artifact_cleanup.review_item(workspace_id, ref);

alter table artifact_cleanup.sweep_run
    add column review_id uuid references artifact_cleanup.review(id);

create index sweep_run_review on artifact_cleanup.sweep_run(review_id);
