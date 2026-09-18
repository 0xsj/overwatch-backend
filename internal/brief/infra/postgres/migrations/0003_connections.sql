create table brief.working_connection (
    workspace_id  uuid not null,
    brief_id      uuid not null references brief.working(id) on delete cascade,
    connection_id uuid not null references research.connection(id),
    primary key (brief_id, connection_id)
);

create table brief.snapshot_connection (
    workspace_id               uuid not null,
    snapshot_id                uuid not null references brief.snapshot(id) on delete cascade,
    connection_id              uuid not null,
    from_record_id             uuid not null,
    from_record_kind           text not null,
    from_record_name           text not null,
    to_record_id               uuid not null,
    to_record_kind             text not null,
    to_record_name             text not null,
    kind                       text not null,
    state                      text not null,
    rationale                  text not null,
    supporting_observation_ids jsonb not null default '[]'::jsonb,
    opposing_observation_ids   jsonb not null default '[]'::jsonb,
    primary key (snapshot_id, connection_id)
);

create index working_connection_for_workspace on brief.working_connection(workspace_id, brief_id);
create index snapshot_connection_for_snapshot on brief.snapshot_connection(workspace_id, snapshot_id);
