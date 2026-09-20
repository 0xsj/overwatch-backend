create table assistance.provider_policy (
    workspace_id    uuid primary key,
    allow_external  boolean not null default false,
    updated_by      uuid not null,
    updated_at      timestamptz not null
);
