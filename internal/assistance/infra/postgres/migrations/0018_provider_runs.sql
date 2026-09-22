create table assistance.provider_run (
    id               uuid primary key,
    workspace_id     uuid not null,
    kind             text not null check (octet_length(kind) between 1 and 80),
    result_id        uuid not null,
    provider         text not null check (octet_length(provider) between 1 and 200),
    method           text not null check (octet_length(method) between 1 and 200),
    template_version text not null check (octet_length(template_version) between 1 and 200),
    status           text not null check (octet_length(status) between 1 and 80),
    input_bytes      bigint not null check (input_bytes >= 0),
    output_bytes     bigint not null check (output_bytes >= 0),
    duration_ms      bigint not null check (duration_ms >= 0),
    timed_out        boolean not null default false,
    error            text not null default '' check (octet_length(error) <= 2000),
    created_by       uuid not null,
    created_at       timestamptz not null,
    completed_at     timestamptz not null,
    check (completed_at >= created_at)
);

create index provider_run_for_workspace on assistance.provider_run(workspace_id, created_at desc, id desc);
