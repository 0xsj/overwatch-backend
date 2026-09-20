create table timeline.event_cluster (
    id          uuid primary key,
    workspace_id uuid not null,
    title       text not null check (octet_length(title) between 1 and 200),
    description text not null default '' check (octet_length(description) <= 4000),
    state       text not null check (state in ('proposed','accepted','rejected')),
    review_note text not null default '' check (octet_length(review_note) <= 4000),
    author      uuid not null,
    updated_by  uuid not null,
    reviewed_by uuid,
    reviewed_at timestamptz,
    created_at  timestamptz not null,
    updated_at  timestamptz not null,
    check ((state = 'proposed' and reviewed_by is null and reviewed_at is null)
       or (state <> 'proposed' and reviewed_by is not null and reviewed_at is not null and octet_length(review_note) > 0)),
    check (updated_at >= created_at)
);

create table timeline.event_cluster_event (
    workspace_id uuid not null,
    cluster_id   uuid not null references timeline.event_cluster(id) on delete cascade,
    event_id     uuid not null references timeline.event(id) on delete cascade,
    ordinal      integer not null check (ordinal >= 0 and ordinal < 12),
    primary key (cluster_id, event_id),
    unique (cluster_id, ordinal)
);

create index event_cluster_for_workspace on timeline.event_cluster(workspace_id,id desc);
create index event_cluster_event_for_event on timeline.event_cluster_event(workspace_id,event_id,cluster_id);
