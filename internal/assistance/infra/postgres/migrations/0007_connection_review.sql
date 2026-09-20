create table assistance.connection_review (
    id                         uuid primary key,
    workspace_id               uuid not null,
    connection_id              uuid not null,
    from_record_id             uuid not null,
    to_record_id               uuid not null,
    connection_kind            text not null,
    connection_state           text not null,
    connection_rationale       text not null check (octet_length(connection_rationale) between 1 and 4000),
    supporting_observation_ids jsonb not null,
    opposing_observation_ids   jsonb not null,
    provider                   text not null check (octet_length(provider) between 1 and 200),
    method                     text not null check (octet_length(method) between 1 and 200),
    template_version           text not null check (octet_length(template_version) between 1 and 200),
    status                     text not null check (status in ('completed', 'empty')),
    output                     text not null check (octet_length(output) <= 24000),
    findings                   jsonb not null,
    created_by                 uuid not null,
    created_at                 timestamptz not null
);

create index connection_review_for_connection on assistance.connection_review(workspace_id, connection_id, created_at desc, id desc);
