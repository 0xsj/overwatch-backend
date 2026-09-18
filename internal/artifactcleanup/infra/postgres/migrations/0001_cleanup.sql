create schema if not exists artifact_cleanup;

create table artifact_cleanup.sweep_run (
    id                 uuid primary key,
    workspace_id       uuid not null,
    requested_by       uuid not null,
    status             text not null,
    "limit"            integer not null,
    candidate_count    integer not null default 0,
    candidate_bytes    bigint not null default 0,
    deleted_count      integer not null default 0,
    deleted_bytes      bigint not null default 0,
    already_gone_count integer not null default 0,
    skipped_count      integer not null default 0,
    error              text not null default '',
    started_at         timestamptz not null,
    finished_at        timestamptz,

    constraint sweep_status_known check (status in ('running', 'completed', 'failed', 'interrupted')),
    constraint sweep_limit_positive check ("limit" between 1 and 100),
    constraint sweep_counts_nonnegative check (
        candidate_count >= 0 and candidate_bytes >= 0 and
        deleted_count >= 0 and deleted_bytes >= 0 and
        already_gone_count >= 0 and skipped_count >= 0
    ),
    constraint sweep_finished_shape check ((status = 'running') = (finished_at is null)),
    constraint sweep_error_shape check ((status = 'running' or status = 'completed') = (error = ''))
);

create index sweep_run_workspace on artifact_cleanup.sweep_run(workspace_id, started_at desc);
