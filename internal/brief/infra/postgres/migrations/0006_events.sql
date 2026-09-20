create table brief.working_event (
    workspace_id uuid not null,
    brief_id     uuid not null references brief.working(id) on delete cascade,
    event_id     uuid not null references timeline.event(id),
    primary key (brief_id, event_id)
);

create table brief.snapshot_event (
    workspace_id        uuid not null,
    snapshot_id         uuid not null references brief.snapshot(id) on delete cascade,
    event_id            uuid not null,
    title               text not null check (octet_length(title) between 1 and 400),
    description         text not null default '' check (octet_length(description) <= 4000),
    reported_time       text not null default '' check (octet_length(reported_time) <= 200),
    time_precision      text not null,
    sort_date           text not null default '',
    location            text not null default '' check (octet_length(location) <= 400),
    observation_ids     jsonb not null default '[]'::jsonb,
    participant_records jsonb not null default '[]'::jsonb,
    location_record     jsonb,
    primary key (snapshot_id, event_id)
);

create index working_event_for_workspace on brief.working_event(workspace_id, brief_id);
create index snapshot_event_for_snapshot on brief.snapshot_event(workspace_id, snapshot_id);
