alter table brief.snapshot_event add column event_revision_id uuid;
alter table brief.snapshot_event add column event_revision integer;

create index snapshot_event_revision on brief.snapshot_event(event_revision_id);
