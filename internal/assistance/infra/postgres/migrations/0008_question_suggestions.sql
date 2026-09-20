create table assistance.question_suggestions (
    id               uuid primary key,
    workspace_id     uuid not null,
    gaps             jsonb not null,
    provider         text not null check (octet_length(provider) between 1 and 200),
    method           text not null check (octet_length(method) between 1 and 200),
    template_version text not null check (octet_length(template_version) between 1 and 200),
    status           text not null check (status in ('completed', 'empty')),
    output           text not null check (octet_length(output) <= 24000),
    suggestions      jsonb not null,
    created_by       uuid not null,
    created_at       timestamptz not null
);

create index question_suggestions_for_workspace on assistance.question_suggestions(workspace_id, created_at desc, id desc);
