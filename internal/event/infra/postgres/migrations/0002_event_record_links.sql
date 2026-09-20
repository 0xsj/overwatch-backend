alter table timeline.event add column location_record_id uuid;

create table timeline.event_participant (
    workspace_id uuid not null,
    event_id uuid not null references timeline.event(id) on delete cascade,
    record_id uuid not null,
    primary key (event_id, record_id)
);

create index event_participant_for_event on timeline.event_participant(workspace_id,event_id);
create index event_participant_for_record on timeline.event_participant(workspace_id,record_id);
