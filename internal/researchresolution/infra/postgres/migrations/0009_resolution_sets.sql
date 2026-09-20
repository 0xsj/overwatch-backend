create table research.record_resolution_set (
    id                                uuid primary key,
    workspace_id                      uuid not null,
    canonical_record_id               uuid not null references research.record(id),
    state                             text not null check (state in ('proposed','accepted','rejected','reversed')),
    rationale                         text not null check (octet_length(rationale) between 1 and 4000),
    proposed_by                       uuid not null,
    proposed_at                       timestamptz not null,
    reviewed_by                       uuid,
    reviewed_at                       timestamptz,
    reversed_by                       uuid,
    reversed_at                       timestamptz,
    canonical_observation_ids_before jsonb not null default '[]'::jsonb,
    added_observation_ids             jsonb not null default '[]'::jsonb,
    check (reviewed_at is null or reviewed_at >= proposed_at),
    check (reversed_at is null or reviewed_at is not null and reversed_at >= reviewed_at)
);

create table research.record_resolution_set_member (
    resolution_set_id uuid not null references research.record_resolution_set(id) on delete cascade,
    workspace_id      uuid not null,
    record_id         uuid not null references research.record(id),
    role              text not null check (role in ('alias','canonical')),
    active            boolean not null default true,
    primary key (resolution_set_id, record_id)
);

create unique index record_resolution_set_active_record
    on research.record_resolution_set_member(workspace_id, record_id)
    where active;
create index record_resolution_set_for_workspace
    on research.record_resolution_set(workspace_id, id desc);
