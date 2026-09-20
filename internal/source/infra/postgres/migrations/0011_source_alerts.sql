create table source.alert (
    id uuid primary key,
    workspace_id uuid not null,
    source_id uuid not null,
    capture_id uuid,
    kind text not null check (kind in ('capture_changed','watch_failed')),
    title text not null check (octet_length(title) between 1 and 400),
    detail text not null check (octet_length(detail) <= 2000),
    created_by uuid not null,
    created_at timestamptz not null,
    unique (workspace_id,id),
    foreign key (workspace_id,source_id) references source.source(workspace_id,id),
    foreign key (capture_id) references source.capture(id),
    check ((kind = 'capture_changed' and capture_id is not null) or (kind = 'watch_failed' and capture_id is null))
);

create index source_alert_workspace on source.alert(workspace_id,id desc);

create table source.alert_seen (
    workspace_id uuid not null,
    alert_id uuid not null,
    account_id uuid not null,
    seen_at timestamptz not null,
    primary key (alert_id,account_id),
    foreign key (workspace_id,alert_id) references source.alert(workspace_id,id)
);

create index source_alert_seen_account on source.alert_seen(account_id,workspace_id,seen_at);
