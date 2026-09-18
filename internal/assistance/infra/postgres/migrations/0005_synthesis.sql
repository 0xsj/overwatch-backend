create table assistance.synthesis (
    id              uuid primary key,
    workspace_id    uuid not null,
    observation_ids jsonb not null,
    provider        text not null check (octet_length(provider) between 1 and 200),
    method          text not null check (octet_length(method) between 1 and 200),
    output          text not null check (octet_length(output) <= 24000),
    candidates      jsonb not null,
    created_by      uuid not null,
    created_at      timestamptz not null
);

create index synthesis_for_workspace on assistance.synthesis(workspace_id, created_at desc, id desc);
