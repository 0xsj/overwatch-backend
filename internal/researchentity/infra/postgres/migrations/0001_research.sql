create schema if not exists research;

create table research.record (
    id           uuid primary key,
    workspace_id uuid not null,
    kind         text not null check (kind in ('person','account','organisation','place')),
    name         text not null check (octet_length(name) between 1 and 400),
    description  text not null default '' check (octet_length(description) <= 4000),
    author       uuid not null,
    updated_by   uuid not null,
    created_at   timestamptz not null,
    updated_at   timestamptz not null,
    check (updated_at >= created_at)
);

create table research.record_observation (
    workspace_id   uuid not null,
    record_id      uuid not null references research.record(id) on delete cascade,
    observation_id uuid not null,
    primary key (record_id, observation_id)
);

create index record_for_workspace on research.record(workspace_id, id desc);
create index record_observation_for_record on research.record_observation(workspace_id, record_id);
