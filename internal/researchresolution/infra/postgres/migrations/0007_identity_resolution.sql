create table research.record_resolution (
    id                                uuid primary key,
    workspace_id                      uuid not null,
    alias_record_id                   uuid not null references research.record(id),
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
    check (alias_record_id <> canonical_record_id),
    check (reviewed_at is null or reviewed_at >= proposed_at),
    check (reversed_at is null or reviewed_at is not null and reversed_at >= reviewed_at)
);

create unique index record_resolution_active_alias on research.record_resolution(workspace_id, alias_record_id) where state = 'accepted';
create index record_resolution_for_workspace on research.record_resolution(workspace_id, id desc);
