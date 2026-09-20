create table timeline.event_revision (
    id                    uuid primary key,
    workspace_id          uuid not null,
    event_id              uuid not null references timeline.event(id) on delete cascade,
    revision              integer not null check (revision > 0),
    title                 text not null check (octet_length(title) between 1 and 400),
    description           text not null default '' check (octet_length(description) <= 4000),
    reported_time         text not null default '' check (octet_length(reported_time) <= 200),
    time_precision        text not null check (time_precision in ('unknown','exact','approximate','range')),
    sort_date             text not null default '' check (sort_date = '' or sort_date ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'),
    location              text not null default '' check (octet_length(location) <= 400),
    observation_ids       jsonb not null default '[]'::jsonb,
    participant_record_ids jsonb not null default '[]'::jsonb,
    participant_records   jsonb not null default '[]'::jsonb,
    location_record_id    uuid,
    location_record       jsonb,
    changed_by            uuid not null,
    changed_at            timestamptz not null,
    unique (event_id, revision),
    check ((time_precision = 'unknown' and reported_time = '' and sort_date = '')
       or (time_precision <> 'unknown' and reported_time <> ''))
);

create index event_revision_for_event on timeline.event_revision(workspace_id,event_id,revision);
