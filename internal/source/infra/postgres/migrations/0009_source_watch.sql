create table source.watch (
    workspace_id uuid not null,
    source_id uuid not null,
    enabled boolean not null,
    interval_seconds integer not null check (interval_seconds between 900 and 604800),
    next_run_at timestamptz,
    last_run_at timestamptz,
    last_status text not null default 'never' check (last_status in ('never','changed','unchanged','failed')),
    last_capture_id uuid,
    last_error text not null default '' check (octet_length(last_error) <= 2000),
    updated_by uuid not null,
    updated_at timestamptz not null,
    primary key (workspace_id,source_id),
    foreign key (workspace_id,source_id) references source.source(workspace_id,id),
    check ((enabled and next_run_at is not null) or (not enabled and next_run_at is null)),
    check ((last_status = 'changed' and last_capture_id is not null) or (last_status <> 'changed'))
);
create index source_watch_due on source.watch(enabled,next_run_at);
