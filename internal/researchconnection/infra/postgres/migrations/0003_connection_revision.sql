create table research.connection_revision (
    id                         uuid primary key,
    workspace_id               uuid not null,
    connection_id              uuid not null references research.connection(id),
    revision                   integer not null check (revision > 0),
    from_record_id             uuid not null,
    to_record_id               uuid not null,
    kind                       text not null check (kind in ('associated_with','may_belong_to','mentions','concerns_same_event','located_at')),
    state                      text not null check (state in ('proposed','accepted','rejected','deferred')),
    rationale                  text not null check (octet_length(rationale) between 1 and 4000),
    supporting_observation_ids jsonb not null default '[]'::jsonb,
    opposing_observation_ids   jsonb not null default '[]'::jsonb,
    changed_by                 uuid not null,
    changed_at                 timestamptz not null,
    unique (connection_id, revision)
);

create index connection_revision_for_connection on research.connection_revision(workspace_id, connection_id, revision);
