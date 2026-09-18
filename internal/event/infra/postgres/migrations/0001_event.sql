create table timeline.event (
    id             uuid primary key,
    workspace_id   uuid not null,
    title          text not null check (octet_length(title) between 1 and 400),
    description    text not null default '' check (octet_length(description) <= 4000),
    reported_time  text not null default '' check (octet_length(reported_time) <= 200),
    time_precision text not null check (time_precision in ('unknown','exact','approximate','range')),
    sort_date      text not null default '' check (sort_date = '' or sort_date ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'),
    location       text not null default '' check (octet_length(location) <= 400),
    author         uuid not null,
    updated_by     uuid not null,
    created_at     timestamptz not null,
    updated_at     timestamptz not null,
    check ((time_precision = 'unknown' and reported_time = '' and sort_date = '')
       or (time_precision <> 'unknown' and reported_time <> '')),
    check (updated_at >= created_at)
);

create table timeline.event_observation (
    workspace_id   uuid not null,
    event_id       uuid not null references timeline.event(id) on delete cascade,
    observation_id uuid not null,
    primary key (event_id, observation_id)
);

create index event_for_workspace on timeline.event(workspace_id,id desc);
create index event_observation_for_event on timeline.event_observation(workspace_id,event_id);
