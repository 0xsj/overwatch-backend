create table timeline.event_account (
    id                    uuid primary key,
    workspace_id          uuid not null,
    event_id              uuid not null references timeline.event(id) on delete cascade,
    title                 text not null check (octet_length(title) between 1 and 400),
    description           text not null default '' check (octet_length(description) <= 4000),
    reported_time         text not null default '' check (octet_length(reported_time) <= 200),
    time_precision        text not null check (time_precision in ('unknown','exact','approximate','range')),
    sort_date             text not null default '' check (sort_date = '' or sort_date ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'),
    location              text not null default '' check (octet_length(location) <= 400),
    author                uuid not null,
    created_at            timestamptz not null,
    updated_at            timestamptz not null,
    check ((time_precision = 'unknown' and reported_time = '' and sort_date = '')
       or (time_precision <> 'unknown' and reported_time <> '')),
    check (updated_at >= created_at)
);

create table timeline.event_account_observation (
    workspace_id   uuid not null,
    account_id     uuid not null references timeline.event_account(id) on delete cascade,
    observation_id uuid not null,
    primary key (account_id, observation_id)
);

create table timeline.event_account_participant (
    workspace_id uuid not null,
    account_id   uuid not null references timeline.event_account(id) on delete cascade,
    record_id    uuid not null,
    primary key (account_id, record_id)
);

create table timeline.event_account_location (
    workspace_id uuid not null,
    account_id   uuid primary key references timeline.event_account(id) on delete cascade,
    record_id    uuid not null
);

create table timeline.event_reconciliation (
    id                  uuid primary key,
    workspace_id        uuid not null,
    event_id            uuid not null references timeline.event(id) on delete cascade,
    decision            text not null check (decision in ('unresolved','retain_event','prefer_account')),
    selected_account_id uuid references timeline.event_account(id),
    rationale           text not null check (octet_length(rationale) between 1 and 4000),
    reviewed_by         uuid not null,
    reviewed_at         timestamptz not null,
    unique (workspace_id, event_id),
    check ((decision = 'prefer_account' and selected_account_id is not null)
       or (decision <> 'prefer_account' and selected_account_id is null))
);

create index event_account_for_event on timeline.event_account(workspace_id,event_id,created_at,id);
create index event_account_observation_for_account on timeline.event_account_observation(workspace_id,account_id);
create index event_account_participant_for_account on timeline.event_account_participant(workspace_id,account_id);
