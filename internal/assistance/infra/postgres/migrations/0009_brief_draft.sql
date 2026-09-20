create table assistance.brief_draft (
    id               uuid primary key,
    workspace_id     uuid not null,
    brief_id         uuid not null,
    input            jsonb not null,
    provider         text not null check (octet_length(provider) between 1 and 200),
    method           text not null check (octet_length(method) between 1 and 200),
    template_version text not null check (octet_length(template_version) between 1 and 200),
    status           text not null check (status in ('completed', 'empty')),
    output           text not null check (octet_length(output) <= 24000),
    changes          jsonb not null,
    created_by       uuid not null,
    created_at       timestamptz not null
);

create index brief_draft_for_workspace on assistance.brief_draft(workspace_id, created_at desc, id desc);
